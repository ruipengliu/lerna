package memory

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type QueryLimits struct {
	MaxCandidates       uint64 `json:"max_candidates"`
	MaxReadBytes        uint64 `json:"max_read_bytes"`
	MaxPermissionChecks uint64 `json:"max_permission_checks"`
	Deadline            string `json:"deadline"`
}

type permissionBudgetKey struct{}
type permissionBudget struct{ remaining uint64 }
type QueryInput struct {
	QueryRef api.ContentRef `json:"query_ref"`
	ScopeRef api.ContentRef `json:"scope_ref"`
	Purposes []string       `json:"purposes"`
	Limits   QueryLimits    `json:"limits"`
	Limit    uint64         `json:"limit,omitempty"`
	Cursor   string         `json:"cursor,omitempty"`
}

func LexicalProfile() api.ComponentRef {
	return api.ComponentRef{ComponentID: "ranking_00000000000000000000000000000001", Version: "han2-ascii1", Digest: api.Hash([]byte("han-bigrams/ascii-lower-words/unique-overlap/id-tie-break/v1"))}
}
func LiteralProfile() api.ComponentRef {
	return api.ComponentRef{ComponentID: "ranking_00000000000000000000000000000002", Version: "literal1", Digest: api.Hash([]byte("case-insensitive-literal-substring/id-tie-break/v1"))}
}

func lexicalTerms(text string) []string {
	terms := map[string]bool{}
	runes := []rune(strings.ToLower(text))
	var word strings.Builder
	flush := func() {
		if word.Len() != 0 {
			terms[word.String()] = true
			word.Reset()
		}
	}
	for i, r := range runes {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			word.WriteRune(r)
			continue
		}
		flush()
		if unicode.Is(unicode.Han, r) && i+1 < len(runes) && unicode.Is(unicode.Han, runes[i+1]) {
			terms[string(runes[i:i+2])] = true
		}
	}
	flush()
	out := make([]string, 0, len(terms))
	for term := range terms {
		out = append(out, term)
	}
	sort.Strings(out)
	return out
}

func (s *Service) QueryMemory(ctx context.Context, scope runtime.Scope, auth runtime.Auth, queryID string, in QueryInput) (api.Page[Match], error) {
	if err := checkAuth(scope, auth); err != nil {
		return api.Page[Match]{}, err
	}
	if len(in.Purposes) == 0 || len(in.Purposes) > 10 || in.Limits.MaxCandidates < 1 || in.Limits.MaxCandidates > 200 || in.Limits.MaxReadBytes < 1 || in.Limits.MaxReadBytes > MaxContentBytes || in.Limits.MaxPermissionChecks < 1 || in.Limits.MaxPermissionChecks > 100000 || in.Limit > 20 {
		return api.Page[Match]{}, api.E("invalid_request", "invalid_query_limits")
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Cursor != "" {
		return s.queryPage(ctx, scope, auth, in)
	}
	ctx = context.WithValue(ctx, permissionBudgetKey{}, &permissionBudget{remaining: in.Limits.MaxPermissionChecks})
	if !api.ValidID(queryID) {
		return api.Page[Match]{}, api.E("invalid_request", "invalid_query_identity")
	}
	digestIn := in
	digestIn.Limit = 0
	digestIn.Cursor = ""
	digest, err := api.Digest(digestIn)
	if err != nil {
		return api.Page[Match]{}, err
	}
	var existing QueryView
	_, err = s.Store.Read(ctx, scope, "memory.queries", queryID, 0, &existing)
	if err == nil {
		if existing.PrincipalID != auth.SubjectID || existing.Digest != digest {
			return api.Page[Match]{}, api.E("idempotency_conflict", "query_input_changed")
		}
		in.Cursor = cursorFor(existing.QueryID, existing.Digest, 0)
		return s.queryPage(ctx, scope, auth, in)
	}
	if !api.IsCode(err, "not_found") {
		return api.Page[Match]{}, err
	}
	var frozen []MemoryRecord
	var token string
	var head ChangeHead
	var expires time.Time
	partial := false
	gaps := []string{}
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		deadline, err := future(ctx, tx, in.Limits.Deadline)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if deadline.After(now.Add(5 * time.Minute)) {
			return api.E("invalid_request", "query_deadline_exceeded")
		}
		expires, err = querySnapshotExpiry(ctx, now)
		if err != nil {
			return err
		}
		for _, ref := range []api.ContentRef{in.QueryRef, in.ScopeRef} {
			if _, err = s.CheckContentTx(ctx, tx, auth, ref, "memory.query", s.Location, true); err != nil {
				return err
			}
		}
		head, err = loadHead(ctx, tx)
		if err != nil {
			return err
		}
		token, err = s.visibility(ctx, tx, auth)
		return err
	})
	if err != nil {
		return api.Page[Match]{}, err
	}
	deadline, _ := api.ParseTime(in.Limits.Deadline)
	bounded, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if in.QueryRef.ByteLength > in.Limits.MaxReadBytes {
		return api.Page[Match]{}, api.E("invalid_request", "query_text_budget_exceeded")
	}
	specBytes, err := s.Read(bounded, scope, auth, in.QueryRef, "memory.query")
	if err != nil {
		return api.Page[Match]{}, err
	}
	validator, err := api.NewValidator(api.SchemaFor[MemoryQuerySpec]())
	if err != nil {
		return api.Page[Match]{}, err
	}
	if err = validator.Validate(specBytes); err != nil {
		return api.Page[Match]{}, err
	}
	var spec MemoryQuerySpec
	if err = api.Decode(specBytes, &spec); err != nil {
		return api.Page[Match]{}, err
	}
	if !api.Equal(spec.RankingProfileRef, LexicalProfile()) && !api.Equal(spec.RankingProfileRef, LiteralProfile()) {
		return api.Page[Match]{}, api.E("unsupported", "ranking_profile_unavailable")
	}
	for _, typ := range spec.TypeFilter {
		if !validMemoryType(typ) {
			return api.Page[Match]{}, api.E("invalid_request", "invalid_memory_type")
		}
	}
	var validAt time.Time
	if spec.ValidAt != "" {
		validAt, err = api.ParseTime(spec.ValidAt)
		if err != nil {
			return api.Page[Match]{}, api.E("invalid_request", "invalid_valid_at")
		}
	}
	if spec.TextRef.ByteLength == 0 || spec.TextRef.ByteLength > 4096-uint64(len("literal:")) || uint64(len(specBytes)) > in.Limits.MaxReadBytes || spec.TextRef.ByteLength > in.Limits.MaxReadBytes-uint64(len(specBytes)) {
		return api.Page[Match]{}, api.E("invalid_request", "query_text_budget_exceeded")
	}
	text, err := s.Read(bounded, scope, auth, spec.TextRef, "memory.query")
	if err != nil {
		return api.Page[Match]{}, err
	}
	if len(text) > 4096 || uint64(len(text)+len(specBytes)) > in.Limits.MaxReadBytes {
		return api.Page[Match]{}, api.E("invalid_request", "query_text_budget_exceeded")
	}
	remaining := in.Limits.MaxReadBytes - uint64(len(text)+len(specBytes))
	terms := lexicalTerms(string(text))
	if len(terms) > 100 {
		return api.Page[Match]{}, api.E("invalid_request", "query_text_terms_exceeded")
	}
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		currentToken, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		if currentToken != token {
			return api.E("snapshot_required", "query_scope_changed")
		}
		head, err = loadHead(ctx, tx)
		if err != nil {
			return err
		}
		if _, err = s.CheckContentTx(ctx, tx, auth, spec.TextRef, "memory.query", s.Location, true); err != nil {
			return err
		}
		rows, err := tx.List(ctx, "memory.records", "", "", int(in.Limits.MaxCandidates)+1)
		if err != nil {
			return err
		}
		if len(rows) > int(in.Limits.MaxCandidates) {
			partial = true
			gaps = append(gaps, "candidate_limit")
			rows = rows[:in.Limits.MaxCandidates]
		}
		for _, row := range rows {
			var record MemoryRecord
			if err = row.Decode(&record); err != nil {
				return err
			}
			if record.State != "active" || len(spec.TypeFilter) > 0 && !contains(spec.TypeFilter, record.Values.Type) || spec.ScopeFilter != nil && !api.Equal(*spec.ScopeFilter, record.Values.ScopeRef) {
				continue
			}
			if !validAt.IsZero() {
				if record.Values.ValidFrom != "" {
					from, _ := api.ParseTime(record.Values.ValidFrom)
					if validAt.Before(from) {
						continue
					}
				}
				if record.Values.ValidTo != "" {
					to, _ := api.ParseTime(record.Values.ValidTo)
					if !validAt.Before(to) {
						continue
					}
				}
			}
			allowed := true
			for _, purpose := range append([]string{"memory.query"}, in.Purposes...) {
				if err = s.memoryAllowed(ctx, tx, auth, record, purpose, true); err != nil {
					allowed = false
					if isUnavailable(err) {
						partial = true
						gaps = append(gaps, "permission_authority_unavailable")
					}
					if api.IsCode(err, "overloaded") {
						partial = true
						gaps = append(gaps, "permission_budget")
						break
					}
					if !isUnavailable(err) && !api.IsCode(err, "forbidden") && !api.IsCode(err, "gone") {
						return err
					}
					break
				}
			}
			if api.IsCode(err, "overloaded") {
				break
			}
			if allowed {
				frozen = append(frozen, record)
			}
		}
		return nil
	})
	if err != nil {
		return api.Page[Match]{}, err
	}
	matches := []Match{}
	for _, record := range frozen {
		if record.Values.ContentRef.ByteLength > remaining {
			partial = true
			gaps = append(gaps, "byte_budget")
			break
		}
		body, err := s.Read(bounded, scope, auth, record.Values.ContentRef, "memory.query")
		if err != nil {
			if api.IsCode(err, "overloaded") {
				partial = true
				gaps = append(gaps, "permission_budget")
				break
			}
			if isUnavailable(err) || api.IsCode(err, "gone") {
				partial = true
				gaps = append(gaps, "content_unavailable")
				continue
			}
			return api.Page[Match]{}, err
		}
		remaining -= uint64(len(body))
		explanation := []string{}
		if api.Equal(spec.RankingProfileRef, LiteralProfile()) {
			if strings.Contains(strings.ToLower(string(body)), strings.ToLower(string(text))) {
				explanation = append(explanation, "literal:"+string(text))
			}
		} else {
			bodyTerms := lexicalTerms(string(body))
			for _, term := range terms {
				if contains(bodyTerms, term) {
					explanation = append(explanation, "term:"+term)
				}
			}
		}
		if len(explanation) > 0 {
			matches = append(matches, Match{MemoryRef: scope.Ref(record.MemoryID, record.Revision), ContentRef: record.Values.ContentRef, Score: uint64(len(explanation)), Explanation: explanation})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].MemoryRef.ObjectID < matches[j].MemoryRef.ObjectID
	})
	view := QueryView{QueryID: queryID, Revision: 1, PrincipalID: auth.SubjectID, Digest: digest, VisibilityToken: token, SourceRefs: []api.ContentRef{in.QueryRef, in.ScopeRef, spec.TextRef}, ExpiresAt: api.Time(expires), ChangeHead: head.ChangeHead, Matches: matches, Partial: partial, Gaps: unique(gaps), RemainingPermissionChecks: ctx.Value(permissionBudgetKey{}).(*permissionBudget).remaining}
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		current, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		if current != token {
			return api.E("snapshot_required", "query_scope_changed")
		}
		return tx.Create(ctx, "memory.queries", queryID, auth.SubjectID, view)
	})
	if err != nil {
		return api.Page[Match]{}, err
	}
	in.Cursor = cursorFor(queryID, digest, 0)
	return s.queryPage(ctx, scope, auth, in)
}

func unique(values []string) []string {
	set := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !set[v] {
			out = append(out, v)
			set[v] = true
		}
	}
	return out
}

func (s *Service) queryPage(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in QueryInput) (api.Page[Match], error) {
	parts := strings.Split(in.Cursor, ":")
	if len(parts) != 3 {
		return api.Page[Match]{}, api.E("invalid_request", "invalid_cursor")
	}
	id := parts[0]
	digestPart, position, err := parseCursor(in.Cursor, id)
	if err != nil {
		return api.Page[Match]{}, err
	}
	var out api.Page[Match]
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var view QueryView
		viewRevision, err := tx.Get(ctx, "memory.queries", id, &view)
		if err != nil {
			return err
		}
		digestIn := in
		digestIn.Limit = 0
		digestIn.Cursor = ""
		digest, err := api.Digest(digestIn)
		if err != nil {
			return err
		}
		if view.PrincipalID != auth.SubjectID || view.Digest != digest || digestPart != strings.TrimPrefix(view.Digest, "sha256:") || position > len(view.Matches) {
			return api.E("forbidden", "query_subject_or_parameters_changed")
		}
		if _, err = future(ctx, tx, view.ExpiresAt); err != nil {
			return api.E("cursor_expired", "query_expired")
		}
		if _, err = future(ctx, tx, in.Limits.Deadline); err != nil {
			return api.E("cursor_expired", "query_deadline_expired")
		}
		token, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		if token != view.VisibilityToken {
			return api.E("snapshot_required", "query_scope_changed")
		}
		if len(view.SourceRefs) != 3 || !api.Equal(view.SourceRefs[0], in.QueryRef) || !api.Equal(view.SourceRefs[1], in.ScopeRef) {
			return api.E("snapshot_required", "query_sources_unavailable")
		}
		out = api.Page[Match]{Items: []Match{}, CollectionRevision: view.ChangeHead, Partial: view.Partial, Gaps: append([]string{}, view.Gaps...)}
		budget := &permissionBudget{remaining: view.RemainingPermissionChecks}
		pageContext := context.WithValue(ctx, permissionBudgetKey{}, budget)
		sourcesAllowed := true
		for _, ref := range view.SourceRefs {
			if _, err = s.CheckContentTx(pageContext, tx, auth, ref, "memory.query", s.Location, true); err != nil {
				if api.IsCode(err, "overloaded") {
					out.Partial = true
					out.Gaps = unique(append(out.Gaps, "permission_budget"))
					sourcesAllowed = false
					break
				}
				if isUnavailable(err) {
					return err
				}
				return api.E("snapshot_required", "query_source_changed")
			}
		}
		end := position
		if sourcesAllowed {
			end += int(in.Limit)
		}
		if end > len(view.Matches) {
			end = len(view.Matches)
		}
		for _, match := range view.Matches[position:end] {
			var record MemoryRecord
			_, err = tx.Get(ctx, "memory.records", match.MemoryRef.ObjectID, &record)
			if err != nil {
				return err
			}
			if record.Revision != match.MemoryRef.Revision {
				return api.E("snapshot_required", "query_scope_changed")
			}
			for _, purpose := range append([]string{"memory.query"}, in.Purposes...) {
				if err = s.memoryAllowed(pageContext, tx, auth, record, purpose, true); err != nil {
					if api.IsCode(err, "overloaded") {
						out.Partial = true
						out.Gaps = unique(append(out.Gaps, "permission_budget"))
						break
					}
					if isUnavailable(err) {
						out.Partial = true
						out.Gaps = unique(append(out.Gaps, "permission_authority_unavailable"))
						break
					}
					return api.E("snapshot_required", "query_scope_changed")
				}
			}
			if err == nil {
				out.Items = append(out.Items, match)
			}
		}
		out.Exhausted = !sourcesAllowed || end == len(view.Matches)
		if !out.Exhausted {
			out.NextCursor = cursorFor(id, view.Digest, end)
		}
		view.RemainingPermissionChecks = budget.remaining
		view.Partial = out.Partial
		view.Gaps = out.Gaps
		view.Revision = viewRevision + 1
		return tx.Put(ctx, "memory.queries", id, viewRevision, view)
	})
	return out, err
}

type ListMemoryInput struct {
	Purpose string `json:"purpose"`
	Limit   uint64 `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

func querySnapshotExpiry(ctx context.Context, now time.Time) (time.Time, error) {
	expires := now.Add(5 * time.Minute)
	if bound, ok := runtime.QueryBindingExpiry(ctx); ok && bound.Before(expires) {
		expires = bound
	}
	if !expires.After(now) {
		return time.Time{}, api.E("cursor_expired", "query_binding_expired")
	}
	return expires, nil
}

func (s *Service) ListMemory(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ListMemoryInput) (api.Page[MemoryRecord], error) {
	return s.listMemory(ctx, scope, auth, api.NewID("list"), in)
}

type listView struct {
	ListID          string          `json:"list_id"`
	Revision        uint64          `json:"revision"`
	PrincipalID     string          `json:"principal_id"`
	Purpose         string          `json:"purpose"`
	VisibilityToken string          `json:"visibility_token"`
	ExpiresAt       string          `json:"expires_at"`
	Refs            []api.ObjectRef `json:"refs"`
	ChangeHead      uint64          `json:"change_head"`
	Partial         bool            `json:"partial"`
	Gaps            []string        `json:"gaps"`
}

func (s *Service) listMemory(ctx context.Context, scope runtime.Scope, auth runtime.Auth, id string, in ListMemoryInput) (api.Page[MemoryRecord], error) {
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit > 20 || in.Purpose == "" {
		return api.Page[MemoryRecord]{}, api.E("invalid_request", "invalid_list_limits")
	}
	var out api.Page[MemoryRecord]
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		token, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		position := 0
		var view listView
		if in.Cursor == "" {
			_, err = tx.Get(ctx, "memory.lists", id, &view)
			if api.IsCode(err, "not_found") {
				head, err := loadHead(ctx, tx)
				if err != nil {
					return err
				}
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				expires, err := querySnapshotExpiry(ctx, now)
				if err != nil {
					return err
				}
				view = listView{ListID: id, Revision: 1, PrincipalID: auth.SubjectID, Purpose: in.Purpose, VisibilityToken: token, ExpiresAt: api.Time(expires), Refs: []api.ObjectRef{}, ChangeHead: head.ChangeHead, Gaps: []string{}}
				rows, err := tx.List(ctx, "memory.records", "", "", 201)
				if err != nil {
					return err
				}
				if len(rows) > 200 {
					view.Partial = true
					view.Gaps = append(view.Gaps, "candidate_limit")
					rows = rows[:200]
				}
				for _, row := range rows {
					var record MemoryRecord
					if err = row.Decode(&record); err != nil {
						return err
					}
					if err = s.memoryAllowed(ctx, tx, auth, record, in.Purpose, true); err != nil {
						if isUnavailable(err) {
							view.Partial = true
							view.Gaps = unique(append(view.Gaps, "permission_authority_unavailable"))
						}
						if !isUnavailable(err) && !api.IsCode(err, "gone") && !api.IsCode(err, "forbidden") {
							return err
						}
						continue
					}
					view.Refs = append(view.Refs, scope.Ref(record.MemoryID, record.Revision))
				}
				if err = tx.Create(ctx, "memory.lists", id, auth.SubjectID, view); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		} else {
			parts := strings.Split(in.Cursor, ":")
			if len(parts) != 3 {
				return api.E("invalid_request", "invalid_cursor")
			}
			id = parts[0]
			digestPart, offset, err := parseCursor(in.Cursor, id)
			if err != nil {
				return err
			}
			position = offset
			_, err = tx.Get(ctx, "memory.lists", id, &view)
			if err != nil {
				return err
			}
			if digestPart != strings.TrimPrefix(view.VisibilityToken, "sha256:") {
				return api.E("forbidden", "list_cursor_changed")
			}
		}
		if view.PrincipalID != auth.SubjectID || view.Purpose != in.Purpose {
			return api.E("forbidden", "list_subject_or_parameters_changed")
		}
		if token != view.VisibilityToken {
			return api.E("snapshot_required", "query_scope_changed")
		}
		if _, err = future(ctx, tx, view.ExpiresAt); err != nil {
			return api.E("cursor_expired", "query_expired")
		}
		if position > len(view.Refs) {
			return api.E("invalid_request", "invalid_cursor")
		}
		out = api.Page[MemoryRecord]{Items: []MemoryRecord{}, CollectionRevision: view.ChangeHead, Partial: view.Partial, Gaps: append([]string{}, view.Gaps...)}
		end := position + int(in.Limit)
		if end > len(view.Refs) {
			end = len(view.Refs)
		}
		for _, ref := range view.Refs[position:end] {
			var record MemoryRecord
			_, err = tx.Get(ctx, "memory.records", ref.ObjectID, &record)
			if err != nil {
				return err
			}
			if record.Revision != ref.Revision {
				return api.E("snapshot_required", "query_scope_changed")
			}
			if err = s.memoryAllowed(ctx, tx, auth, record, in.Purpose, true); err != nil {
				if isUnavailable(err) {
					out.Partial = true
					out.Gaps = unique(append(out.Gaps, "permission_authority_unavailable"))
					continue
				}
				return api.E("snapshot_required", "query_scope_changed")
			}
			out.Items = append(out.Items, record)
		}
		out.Exhausted = end == len(view.Refs)
		if !out.Exhausted {
			out.NextCursor = cursorFor(id, view.VisibilityToken, end)
		}
		return nil
	})
	return out, err
}

func (s *Service) registerQueries(registry *runtime.Registry) {
	query(s, registry, "memory.query", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in QueryInput) (api.Page[Match], error) {
		return s.QueryMemory(ctx, scope, auth, q.QueryID, in)
	})
	query(s, registry, "memory.list", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ListMemoryInput) (api.Page[MemoryRecord], error) {
		return s.listMemory(ctx, scope, auth, q.QueryID, in)
	})
}
