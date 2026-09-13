package catalog

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	store   Store
	auth    Authorization
	config  Config
	binding QueryContext
	slots   chan struct{}
	ranking Ranking
}

func New(st Store, a Authorization, c Config, b QueryContext) (*Service, error) {
	return WithRanking(st, a, KeywordRanking{}, c, b)
}
func WithRanking(st Store, a Authorization, ranking Ranking, c Config, b QueryContext) (*Service, error) {
	if st == nil || a == nil || ranking == nil || c.MaxScan < 1 || c.MaxScan > 4096 || c.MaxPage < 1 || c.MaxPage > 64 || c.MaxCandidates < 1 || c.MaxCandidates > 32 || c.Timeout < time.Millisecond || c.Timeout > time.Second || b.Namespace == "" || b.Subject == "" || b.Token == "" || b.Purpose == "" || b.Location != "local" {
		return nil, fail(authorization.Invalid)
	}
	return &Service{st, a, c, b, make(chan struct{}, 4), ranking}, nil
}
func fail(c authorization.Code) error { return &authorization.Error{Code: c} }
func Key(r Ref) string                { b, _ := json.Marshal(r); return string(b) }
func queryKey(q Query) string {
	q.Cursor = ""
	b, _ := json.Marshal(q)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

type cursor struct {
	Revision                               uint64
	View, Query, Namespace, Subject, After string
}

func (s *Service) crypt(ctx context.Context) (cipher.AEAD, error) {
	key, e := s.store.CursorKey(ctx)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, fail(authorization.Unavailable)
	}
	return cipher.NewGCM(block)
}
func (s *Service) readCursor(ctx context.Context, raw string) (cursor, error) {
	var c cursor
	if len(raw) > 4096 {
		return c, fail(authorization.Invalid)
	}
	a, e := s.crypt(ctx)
	if e != nil {
		return c, e
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) < a.NonceSize() {
		return c, fail(authorization.Invalid)
	}
	b, e = a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("catalog-cursor-v1"))
	if e != nil || json.Unmarshal(b, &c) != nil {
		return c, fail(authorization.Invalid)
	}
	return c, nil
}
func (s *Service) writeCursor(ctx context.Context, c cursor) (string, error) {
	a, e := s.crypt(ctx)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, b, []byte("catalog-cursor-v1"))), nil
}
func (s *Service) view(ctx context.Context, action string, rows []Summary) (Access, error) {
	policies := make([]Policy, len(rows))
	for i, r := range rows {
		policies[i] = r.Policy()
	}
	b := s.binding
	b.Action = action
	a, e := s.auth.View(ctx, b, policies)
	if e != nil {
		return a, e
	}
	if a.Namespace != b.Namespace || a.Subject != b.Subject || a.Revision == "" || len(a.Allowed) != len(rows) {
		return a, fail(authorization.Denied)
	}
	return a, nil
}
func (s *Service) List(ctx context.Context, q Query) (Page, error)   { return s.query(ctx, q, false) }
func (s *Service) Search(ctx context.Context, q Query) (Page, error) { return s.query(ctx, q, true) }
func (s *Service) query(ctx context.Context, q Query, search bool) (Page, error) {
	out := Page{Items: []Match{}, Limitations: []string{}, Coverage: "COMPLETE"}
	max := s.config.MaxPage
	action := "catalog.list"
	if search {
		max = s.config.MaxCandidates
		action = "catalog.search"
	}
	if q.Limit < 1 || q.Limit > max || q.Budget < 1 || q.Budget > s.config.MaxScan || len(q.Text) > 2048 || !utf8.ValidString(q.Text) || len(q.Category) > 128 || len(q.ResourceType) > 128 || len(q.Required) > 8 || q.Purpose != s.binding.Purpose || q.Location != s.binding.Location || (!search && q.Text != "") || (search && q.Cursor != "") {
		return out, fail(authorization.Invalid)
	}
	for _, v := range q.Required {
		if len(v) > 128 || !utf8.ValidString(v) {
			return out, fail(authorization.Invalid)
		}
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return out, fail(authorization.Unavailable)
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if _, e := s.view(ctx, action, nil); e != nil {
		return Page{}, e
	}
	var cur cursor
	var e error
	if q.Cursor != "" {
		cur, e = s.readCursor(ctx, q.Cursor)
		if e != nil {
			return out, e
		}
		if cur.Query != queryKey(q) || cur.Namespace != s.binding.Namespace || cur.Subject != s.binding.Subject {
			return out, fail(authorization.Conflict)
		}
	}
	snapshot, e := s.store.Snapshot(ctx, cur.After, 4096)
	if e != nil {
		return out, e
	}
	if snapshot.More {
		return out, fail(authorization.Unavailable)
	}
	out.Revision = snapshot.Revision
	access, e := s.view(ctx, action, snapshot.Rows)
	if e != nil {
		return out, e
	}
	if q.Cursor != "" && (cur.Revision != snapshot.Revision || cur.View != access.Revision) {
		return out, fail(authorization.Conflict)
	}
	visible := []Summary{}
	for i, row := range snapshot.Rows {
		if access.Allowed[i] && row.Purpose == q.Purpose && row.Location == q.Location {
			visible = append(visible, row)
		}
	}
	// Only authorized documents reach candidate generation or index scoring.
	refs := make([]Ref, min(q.Budget, len(visible)))
	for i, r := range visible[:len(refs)] {
		refs[i] = r.Ref
	}
	index := IndexResult{Terms: map[string]string{}, Status: "NOT_USED"}
	if search {
		index, e = s.store.Index(ctx, refs, snapshot.Revision)
		if e != nil {
			index = IndexResult{Terms: map[string]string{}, Status: "UNAVAILABLE"}
		}
		out.IndexRevision = index.Revision
		if index.Status != "CURRENT" {
			out.Limitations = append(out.Limitations, "INDEX_"+index.Status)
		}
	}
	type scored struct {
		match Match
		score int
	}
	ranked := []scored{}
	resultBytes := 0
	byteLimited := false
	after := cur.After
	more := snapshot.More
	for i, row := range visible {
		if e = ctx.Err(); e != nil {
			out.Coverage = "PARTIAL"
			out.Limitations = append(out.Limitations, "TIME_BUDGET")
			more = true
			break
		}
		if out.Scanned == q.Budget {
			more = true
			break
		}
		previousAfter := after
		after = Key(row.Ref)
		out.Scanned++
		if !row.Available {
			out.Coverage = "PARTIAL"
			out.Limitations = appendUnique(out.Limitations, "SOURCE_UNAVAILABLE")
			continue
		}
		if q.Category != "" && q.Category != row.Category || q.ResourceType != "" && q.ResourceType != row.ResourceType {
			continue
		}
		text := Text(row)
		if search && index.Status == "CURRENT" {
			v, ok := index.Terms[Key(row.Ref)]
			if !ok {
				out.Limitations = appendUnique(out.Limitations, "INDEX_PARTIAL")
			} else if v != text {
				out.Limitations = appendUnique(out.Limitations, "INDEX_CORRUPT")
			} else {
				text = v
			}
		}
		required := true
		for _, v := range q.Required {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(v)) {
				required = false
			}
		}
		if !required {
			continue
		}
		score := 1
		if search {
			score, e = s.ranking.Score(ctx, q, row, text)
			if e != nil {
				return Page{}, e
			}
			if score == 0 {
				continue
			}
		}
		match := Match{Ref: row.Ref, Title: row.Title, Category: row.Category, ResourceType: row.ResourceType, Reason: "authorized text match", Purpose: row.Purpose, Location: row.Location, Resource: row.Resource, Preconditions: row.Preconditions, Effects: row.Effects, Unsupported: row.Unsupported, Guarantees: row.Guarantees}
		if !search {
			encoded, _ := json.Marshal(match)
			if resultBytes+len(encoded) > 65536 {
				after = previousAfter
				more = true
				byteLimited = true
				break
			}
			resultBytes += len(encoded)
		}
		ranked = append(ranked, scored{match, score})
		if !search && len(ranked) == q.Limit {
			more = snapshot.More || i+1 < len(visible)
			break
		}
	}
	if search {
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].score == ranked[j].score {
				return Key(ranked[i].match.Ref) < Key(ranked[j].match.Ref)
			}
			return ranked[i].score > ranked[j].score
		})
	}
	resultBytes = 0
	for _, r := range ranked[:min(q.Limit, len(ranked))] {
		encoded, _ := json.Marshal(r.match)
		if resultBytes+len(encoded) > 65536 {
			byteLimited = true
			break
		}
		resultBytes += len(encoded)
		out.Items = append(out.Items, r.match)
	}
	if byteLimited {
		out.Coverage = "PARTIAL"
		out.Limitations = appendUnique(out.Limitations, "RESULT_BYTES")
	}
	if more {
		out.Coverage = "PARTIAL"
		reason := "SCAN_BUDGET"
		if !search && len(out.Items) == q.Limit {
			reason = "PAGE_LIMIT"
		}
		out.Limitations = appendUnique(out.Limitations, reason)
		if !search {
			out.Cursor, e = s.writeCursor(ctx, cursor{snapshot.Revision, access.Revision, queryKey(q), s.binding.Namespace, s.binding.Subject, after})
			if e != nil {
				return Page{}, e
			}
		}
	}
	// Revocation during lookup cannot release the earlier view or cached schemas.
	final, e := s.view(ctx, action, snapshot.Rows)
	if e != nil {
		return Page{}, e
	}
	if final.Revision != access.Revision || !sameAllowed(final.Allowed, access.Allowed) {
		return Page{}, fail(authorization.Conflict)
	}
	return out, nil
}
func appendUnique(s []string, v string) []string {
	for _, a := range s {
		if a == v {
			return s
		}
	}
	return append(s, v)
}
func sameAllowed(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func Text(s Summary) string {
	return strings.Join(append([]string{s.Title, s.Category, s.ResourceType, s.Preconditions, s.Effects}, s.Aliases...), " ")
}
func Score(q, text string) int {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return 1
	}
	text = strings.ToLower(text)
	n := 0
	for _, w := range words {
		if strings.Contains(text, w) {
			n++
		}
	}
	return n
}
func (s *Service) Describe(ctx context.Context, ref Ref) (Entry, error) {
	for _, v := range []string{ref.Namespace, ref.Name, ref.Version, ref.Implementation, ref.ImplementationVersion} {
		if len(v) == 0 || len(v) > 256 {
			return Entry{}, fail(authorization.Invalid)
		}
	}
	if len(ref.Digest) != 64 {
		return Entry{}, fail(authorization.Invalid)
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return Entry{}, fail(authorization.Unavailable)
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	if _, e := s.view(ctx, "catalog.describe", nil); e != nil {
		return Entry{}, e
	}
	summary, revision, e := s.store.LookupSummary(ctx, ref)
	if e != nil {
		if authorization.Is(e, authorization.NotFound) {
			return Entry{}, fail(authorization.Denied)
		}
		return Entry{}, e
	}
	a, e := s.view(ctx, "catalog.describe", []Summary{summary})
	if e != nil {
		return Entry{}, e
	}
	if !a.Allowed[0] {
		return Entry{}, fail(authorization.Denied)
	}
	if !summary.Available {
		return Entry{}, fail(authorization.Unavailable)
	}
	entry, loadedRevision, e := s.store.Describe(ctx, ref)
	if e != nil {
		return Entry{}, e
	}
	if loadedRevision != revision {
		return Entry{}, fail(authorization.Conflict)
	}
	final, e := s.view(ctx, "catalog.describe", []Summary{entry.Summary()})
	if e != nil {
		return Entry{}, e
	}
	if final.Revision != a.Revision || !final.Allowed[0] {
		return Entry{}, fail(authorization.Conflict)
	}
	return entry, nil
}
