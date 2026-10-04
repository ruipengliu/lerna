package interaction

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const collections = "interaction.collections"

func pageExpiry(ctx context.Context, now time.Time) (time.Time, error) {
	expiry := now.Add(10 * time.Minute)
	if fixed, ok := runtime.QueryBindingExpiry(ctx); ok {
		expiry = fixed
	}
	if !now.Before(expiry) {
		return time.Time{}, api.E("cursor_expired", "query_binding_expired")
	}
	return expiry, nil
}

type collectionMarker struct {
	Revision uint64 `json:"revision"`
}

func collectionKey(kind, parent string) string { return kind + ":" + parent }
func bumpCollection(ctx context.Context, tx runtime.Tx, kind, parent string) error {
	key := collectionKey(kind, parent)
	var marker collectionMarker
	_, err := tx.Get(ctx, collections, key, &marker)
	if api.IsCode(err, "not_found") {
		return tx.Create(ctx, collections, key, parent, collectionMarker{Revision: 1})
	}
	if err != nil {
		return err
	}
	if marker.Revision >= api.MaxSafeInteger {
		return api.E("overloaded", "collection_revision_exhausted")
	}
	old := marker.Revision
	marker.Revision++
	return tx.Put(ctx, collections, key, old, marker)
}
func markerRevision(ctx context.Context, tx runtime.Tx, kind, parent string) (uint64, error) {
	var marker collectionMarker
	_, err := tx.Get(ctx, collections, collectionKey(kind, parent), &marker)
	if api.IsCode(err, "not_found") {
		err = tx.Create(ctx, collections, collectionKey(kind, parent), parent, collectionMarker{Revision: 1})
		return 1, err
	}
	return marker.Revision, err
}

type pageCursor struct {
	TenantID             string `json:"tenant_id"`
	OwnerID              string `json:"owner_id"`
	DatabaseID           string `json:"database_id"`
	SubjectID            string `json:"subject_id"`
	CredentialGeneration uint64 `json:"credential_generation"`
	RolesDigest          string `json:"roles_digest"`
	Kind                 string `json:"kind"`
	Parent               string `json:"parent"`
	Revision             uint64 `json:"revision"`
	Last                 string `json:"last"`
	ExpiresAt            string `json:"expires_at"`
}

func (s *Service) encodeCursor(cursor pageCursor) string {
	body := api.Raw(cursor)
	mac := hmac.New(sha256.New, s.config.CursorKey)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) parseCursor(raw string, scope runtime.Scope, a runtime.Auth, kind, parent string, revision uint64, now time.Time) (string, time.Time, error) {
	if raw == "" {
		return "", time.Time{}, nil
	}
	if len(raw) > 4096 {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	body, signature, ok := strings.Cut(raw, ".")
	if !ok {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	decoded, e := base64.RawURLEncoding.DecodeString(body)
	if e != nil {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	signed, e := base64.RawURLEncoding.DecodeString(signature)
	if e != nil {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	mac := hmac.New(sha256.New, s.config.CursorKey)
	_, _ = mac.Write(decoded)
	if !hmac.Equal(mac.Sum(nil), signed) {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	var c pageCursor
	if e = api.Decode(decoded, &c); e != nil {
		return "", time.Time{}, api.E("cursor_expired", "cursor_invalid")
	}
	roles, _ := api.Digest(a.Roles)
	expiry, e := api.ParseTime(c.ExpiresAt)
	if e != nil || !now.Before(expiry) || c.TenantID != scope.TenantID || c.OwnerID != scope.OwnerID || c.DatabaseID != scope.DatabaseID || c.SubjectID != a.SubjectID || c.CredentialGeneration != a.CredentialGeneration || c.RolesDigest != roles || c.Kind != kind || c.Parent != parent {
		return "", time.Time{}, api.E("cursor_expired", "cursor_scope_changed")
	}
	if c.Revision != revision {
		return "", time.Time{}, api.E("snapshot_required", "collection_changed")
	}
	return c.Last, expiry, nil
}
func ownedPage[T any](ctx context.Context, s *Service, store runtime.Store, scope runtime.Scope, a runtime.Auth, in api.ListInput, namespace, kind, parent string, gate func(runtime.Tx) error, decode func(runtime.Record) (T, error)) (api.Page[T], error) {
	page := api.Page[T]{Items: []T{}, Gaps: []string{}}
	if in.Limit < 1 || in.Limit > 100 {
		return page, invalid("invalid_page_limit")
	}
	if a.TenantID != scope.TenantID || !api.ValidID(a.SubjectID) || a.CredentialGeneration == 0 {
		return page, api.E("forbidden", "invalid_identity")
	}
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		if gate != nil {
			if e := gate(tx); e != nil {
				return e
			}
		}
		markerKind, _, _ := strings.Cut(kind, "@")
		revision, e := markerRevision(ctx, tx, markerKind, parent)
		if e != nil {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		expiry, e := pageExpiry(ctx, now)
		if e != nil {
			return e
		}
		last, originalExpiry, e := s.parseCursor(in.Cursor, scope, a, kind, parent, revision, now)
		if e != nil {
			return e
		}
		if !originalExpiry.IsZero() && originalExpiry.Before(expiry) {
			expiry = originalExpiry
		}
		rows, e := tx.List(ctx, namespace, parent, last, int(in.Limit)+1)
		if e != nil {
			return e
		}
		page.CollectionRevision = revision
		page.Exhausted = len(rows) <= int(in.Limit)
		if !page.Exhausted {
			rows = rows[:in.Limit]
		}
		for _, row := range rows {
			item, e := decode(row)
			if e != nil {
				return e
			}
			page.Items = append(page.Items, item)
		}
		if !page.Exhausted {
			roles, _ := api.Digest(a.Roles)
			page.NextCursor = s.encodeCursor(pageCursor{TenantID: scope.TenantID, OwnerID: scope.OwnerID, DatabaseID: scope.DatabaseID, SubjectID: a.SubjectID, CredentialGeneration: a.CredentialGeneration, RolesDigest: roles, Kind: kind, Parent: parent, Revision: revision, Last: rows[len(rows)-1].ID, ExpiresAt: api.Time(expiry)})
		}
		return s.checkDisclosureTx(ctx, tx, a)
	})
	if status == runtime.CommitUnknown {
		return api.Page[T]{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return api.Page[T]{}, err
	}
	return page, err
}
func (s *Service) ListSessions(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, in api.ListInput) (api.Page[api.Session], error) {
	return ownedPage(ctx, s, store, scope, a, in, sessions, "sessions", a.SubjectID, nil, func(row runtime.Record) (api.Session, error) {
		var r sessionRecord
		if e := row.Decode(&r); e != nil {
			return api.Session{}, e
		}
		return r.Session, access(a, scope, r.SubjectID)
	})
}
func (s *Service) ListBranches(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in api.ListInput) (api.Page[api.Branch], error) {
	return ownedPage(ctx, s, store, scope, a, in, branches, "branches", id, func(tx runtime.Tx) error { _, e := getSession(ctx, tx, a, id); return e }, func(row runtime.Record) (api.Branch, error) {
		var r branchRecord
		e := row.Decode(&r)
		return r.Branch, e
	})
}
func (s *Service) ListMessages(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in api.ListInput) (api.Page[api.Message], error) {
	return ownedPage(ctx, s, store, scope, a, in, messages, "messages", id, func(tx runtime.Tx) error {
		r, e := getSession(ctx, tx, a, id)
		if e == nil && r.Session.State == "deleted" {
			e = api.E("gone", "session_body_collected")
		}
		return e
	}, func(row runtime.Record) (api.Message, error) { var r api.Message; e := row.Decode(&r); return r, e })
}
func (s *Service) ListSchedules(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, in api.ListInput) (api.Page[Schedule], error) {
	return ownedPage(ctx, s, store, scope, a, in, schedules, "schedules", a.SubjectID, nil, func(row runtime.Record) (Schedule, error) {
		var r scheduleRecord
		if e := row.Decode(&r); e != nil {
			return Schedule{}, e
		}
		return r.Schedule, access(a, scope, r.Auth.SubjectID)
	})
}
func (s *Service) ListOccurrences(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in api.ListInput) (api.Page[Occurrence], error) {
	return ownedPage(ctx, s, store, scope, a, in, occurrences, "occurrences", id, func(tx runtime.Tx) error { _, e := getSchedule(ctx, tx, a, id); return e }, func(row runtime.Record) (Occurrence, error) {
		var r occurrenceRecord
		e := row.Decode(&r)
		return r.Occurrence, e
	})
}

type SkipListInput struct {
	api.ListInput
	RuleRevision *uint64 `json:"rule_revision,omitempty"`
}

func (s *Service) ListSkips(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in SkipListInput) (api.Page[SkipRange], error) {
	kind := "skips"
	if in.RuleRevision != nil {
		if *in.RuleRevision == 0 {
			return api.Page[SkipRange]{}, invalid("invalid_rule_revision")
		}
		kind += "@" + strconv.FormatUint(*in.RuleRevision, 10)
	}
	page, err := ownedPage(ctx, s, store, scope, a, in.ListInput, skips, kind, id, func(tx runtime.Tx) error { _, e := getSchedule(ctx, tx, a, id); return e }, func(row runtime.Record) (SkipRange, error) { var r SkipRange; e := row.Decode(&r); return r, e })
	if err != nil {
		return page, err
	}
	if in.RuleRevision != nil {
		filtered := []SkipRange{}
		for _, item := range page.Items {
			if item.RuleRevision == *in.RuleRevision {
				filtered = append(filtered, item)
			}
		}
		page.Items = filtered
	}
	return page, nil
}
func (s *Service) ListSubmissions(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in api.ListInput) (api.Page[SubmissionView], error) {
	return ownedPage(ctx, s, store, scope, a, in, submissions, "submissions", id, func(tx runtime.Tx) error { _, e := getSession(ctx, tx, a, id); return e }, func(row runtime.Record) (SubmissionView, error) {
		var r submissionRecord
		if e := row.Decode(&r); e != nil {
			return SubmissionView{}, e
		}
		return r.SubmissionView, access(a, scope, r.Auth.SubjectID)
	})
}

type HistoryView struct {
	Submission SubmissionView `json:"submission"`
	Messages   []api.Message  `json:"messages"`
	Complete   bool           `json:"complete"`
}

// History 只沿原 Submission 的不可变父链取得固定截止的历史；每次使用重核内容资格。
func (s *Service) History(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, ref api.ObjectRef) (HistoryView, error) {
	out := HistoryView{Messages: []api.Message{}}
	if e := exactScope(scope, ref); e != nil {
		return out, e
	}
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		var current submissionRecord
		if _, e := tx.Get(ctx, submissions, ref.ObjectID, &current); e != nil {
			return e
		}
		if e := access(a, scope, current.Auth.SubjectID); e != nil {
			return e
		}
		var r submissionRecord
		if e := tx.GetVersion(ctx, submissions, ref.ObjectID, ref.Revision, &r); e != nil {
			return e
		}
		out.Submission = r.SubmissionView
		var source api.Message
		if _, e := tx.Get(ctx, messages, r.Submission.MessageID, &source); e != nil {
			return e
		}
		id := source.ParentMessageID
		seen := map[string]bool{}
		for id != "" {
			if len(out.Messages) >= 100 {
				return api.E("overloaded", "history_budget_exceeded")
			}
			if seen[id] {
				return api.E("invalid_state", "history_cycle")
			}
			seen[id] = true
			var message api.Message
			if _, e := tx.Get(ctx, messages, id, &message); e != nil {
				return e
			}
			if message.SessionRef.ObjectID != r.Submission.SessionRef.ObjectID || message.Seq > r.Submission.HistoryCutoff {
				return invalid("history_scope_mismatch")
			}
			if e := s.content(ctx, tx, a, message.ContentRef, "task.goal"); e != nil {
				return e
			}
			out.Messages = append(out.Messages, message)
			id = message.ParentMessageID
		}
		for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
			out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
		}
		out.Complete = true
		return s.checkDisclosureTx(ctx, tx, a)
	})
	if status == runtime.CommitUnknown {
		return HistoryView{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return HistoryView{}, err
	}
	return out, err
}
