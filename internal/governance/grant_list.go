package governance

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const grantListSlots = 100
const grantListMaxRecords = 999

type grantListEntry struct {
	Grant    api.Grant  `json:"grant"`
	Usage    GrantUsage `json:"usage"`
	InWindow bool       `json:"in_window"`
}
type grantListView struct {
	Entries  []grantListEntry
	Digest   string
	Revision uint64
	Boundary time.Time
}
type grantListQuery struct {
	Scope              runtime.Scope `json:"scope"`
	QueryID            string        `json:"query_id"`
	QueryDigest        string        `json:"query_digest"`
	PrincipalDigest    string        `json:"principal_digest"`
	VisibilityDigest   string        `json:"visibility_digest"`
	ViewDigest         string        `json:"view_digest"`
	CollectionRevision uint64        `json:"collection_revision"`
	Limit              uint64        `json:"limit"`
	QueryExpiresAt     string        `json:"query_expires_at"`
	CursorExpiresAt    string        `json:"cursor_expires_at"`
	Nonce              string        `json:"nonce"`
	Key                string        `json:"key"`
}
type grantListCursor struct {
	Slot      string `json:"slot"`
	Nonce     string `json:"nonce"`
	After     string `json:"after"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) registerGrantList(r *runtime.Registry) error {
	c := api.Contract[api.ListInput, api.Page[GrantRecord]]("grant.list", Namespace, "query", false, false)
	c.InputSchema["properties"].(map[string]any)["limit"] = api.Schema{"type": "integer", "minimum": 1, "maximum": 100}
	return r.Register(runtime.Method{Contract: c, Query: func(ctx context.Context, st runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in api.ListInput
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return s.listGrants(ctx, st, scope, auth, q, in)
	}})
}

func grantListIdentity(scope runtime.Scope, auth runtime.Auth) (string, error) {
	roles := append([]string{}, auth.Roles...)
	sort.Strings(roles)
	return api.Digest(struct {
		Scope   runtime.Scope `json:"scope"`
		Subject api.ObjectRef `json:"subject_ref"`
		Roles   []string      `json:"roles"`
	}{scope, auth.Ref(scope.OwnerID), roles})
}

// Lists here are a metadata snapshot, never grant.check or use qualification.
// No collection head or Grant/Usage row lock is introduced into Use's lock order.
func readGrantListView(ctx context.Context, tx runtime.Tx, now time.Time) (grantListView, error) {
	v := grantListView{Entries: []grantListEntry{}, Revision: 1}
	grants, err := tx.List(ctx, ns("grants"), "", "", grantListMaxRecords+1)
	if err != nil {
		return v, err
	}
	usages, err := tx.List(ctx, ns("grant_usage"), "", "", grantListMaxRecords+1)
	if err != nil {
		return v, err
	}
	if len(grants) > grantListMaxRecords || len(usages) > grantListMaxRecords {
		return v, api.E("overloaded", "grant_list_collection_limit")
	}
	if len(grants) != len(usages) {
		return v, api.E("snapshot_required", "grant_list_collection_changed")
	}
	metadataBytes := 2
	for i, row := range grants {
		metadataBytes += len(row.Data) + len(usages[i].Data)
		if metadataBytes > api.MaxJSONBytes {
			return v, api.E("overloaded", "grant_list_metadata_limit")
		}
		var g api.Grant
		var u GrantUsage
		if err = row.Decode(&g); err != nil {
			return v, err
		}
		if err = usages[i].Decode(&u); err != nil {
			return v, err
		}
		if row.ID != g.GrantID || usages[i].ID != g.GrantID || u.GrantID != g.GrantID || row.Revision != g.Revision || usages[i].Revision != u.Revision || g.OwnerID != tx.Scope().OwnerID || g.SubjectRef.TenantID != tx.Scope().TenantID || row.Revision == 0 || usages[i].Revision == 0 {
			return v, api.E("snapshot_required", "grant_list_collection_changed")
		}
		for _, revision := range []uint64{row.Revision, usages[i].Revision} {
			if revision > api.MaxSafeInteger-v.Revision {
				return v, api.E("overloaded", "grant_list_revision_exhausted")
			}
			v.Revision += revision
		}
		starts, e := api.ParseTime(g.NotBefore)
		if e != nil {
			return v, e
		}
		ends, e := api.ParseTime(g.ExpiresAt)
		if e != nil {
			return v, e
		}
		for _, boundary := range []time.Time{starts, ends} {
			if now.Before(boundary) && (v.Boundary.IsZero() || boundary.Before(v.Boundary)) {
				v.Boundary = boundary
			}
		}
		v.Entries = append(v.Entries, grantListEntry{g, u, g.State == "active" && !now.Before(starts) && now.Before(ends)})
	}
	if len(api.Raw(v.Entries)) > api.MaxJSONBytes {
		return v, api.E("overloaded", "grant_list_metadata_limit")
	}
	v.Digest, err = api.Digest(v.Entries)
	return v, err
}

func grantListSlot(i int) string { return fmt.Sprintf("slot_%02d", i) }

// Only expired original QueryBindings release slots; a grant time boundary does
// not let a repeated first query mint a new snapshot inside its original TTL.
func acquireGrantListQuery(ctx context.Context, tx runtime.Tx, expected grantListQuery, now time.Time) (string, grantListQuery, error) {
	rows, err := tx.List(ctx, ns("grant_list_queries"), "", "", grantListSlots+1)
	if err != nil {
		return "", expected, err
	}
	if len(rows) > grantListSlots {
		return "", expected, api.E("overloaded", "grant_list_query_slots_exhausted")
	}
	used := map[string]grantListQuery{}
	for _, row := range rows {
		var saved grantListQuery
		if err = row.Decode(&saved); err != nil {
			return "", expected, err
		}
		used[row.ID] = saved
		if saved.QueryID == expected.QueryID && saved.QueryExpiresAt == expected.QueryExpiresAt {
			_, err = tx.Get(ctx, ns("grant_list_queries"), row.ID, &saved)
			return row.ID, saved, err
		}
	}
	for i := 0; i < grantListSlots; i++ {
		id := grantListSlot(i)
		old, exists := used[id]
		var revision uint64
		if exists {
			expiry, e := api.ParseTime(old.QueryExpiresAt)
			if e != nil {
				return "", expected, e
			}
			if now.Before(expiry) {
				continue
			}
			revision, err = tx.Get(ctx, ns("grant_list_queries"), id, &old)
			if err != nil {
				return "", expected, err
			}
			expiry, err = api.ParseTime(old.QueryExpiresAt)
			if err != nil {
				return "", expected, err
			}
			if now.Before(expiry) {
				continue
			}
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return "", expected, err
		}
		expected.Key, expected.Nonce = base64.RawURLEncoding.EncodeToString(key), api.NewID("cursor")
		if exists {
			err = tx.Put(ctx, ns("grant_list_queries"), id, revision, expected)
		} else {
			err = tx.Create(ctx, ns("grant_list_queries"), id, "", expected)
		}
		return id, expected, err
	}
	return "", expected, api.E("overloaded", "grant_list_query_slots_exhausted")
}

func encodeGrantListCursor(saved grantListQuery, cursor grantListCursor) string {
	key, _ := base64.RawURLEncoding.DecodeString(saved.Key)
	body := api.Raw(cursor)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func parseGrantListCursor(ctx context.Context, tx runtime.Tx, raw string) (grantListCursor, grantListQuery, error) {
	var c grantListCursor
	var saved grantListQuery
	invalid := api.E("cursor_expired", "grant_list_cursor_invalid")
	body, signature, ok := strings.Cut(raw, ".")
	if !ok || len(raw) > 4096 {
		return c, saved, invalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || api.Decode(decoded, &c) != nil || !api.ValidID(c.After) || !api.ValidID(c.Nonce) {
		return c, saved, invalid
	}
	validSlot := false
	for i := 0; i < grantListSlots; i++ {
		validSlot = validSlot || c.Slot == grantListSlot(i)
	}
	if !validSlot {
		return c, saved, invalid
	}
	if _, err = tx.Get(ctx, ns("grant_list_queries"), c.Slot, &saved); err != nil {
		if api.IsCode(err, "not_found") {
			return c, saved, invalid
		}
		return c, saved, err
	}
	key, err := base64.RawURLEncoding.DecodeString(saved.Key)
	if err != nil || len(key) != 32 || c.Nonce != saved.Nonce {
		return c, saved, invalid
	}
	signed, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return c, saved, invalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(decoded)
	if !hmac.Equal(mac.Sum(nil), signed) {
		return c, saved, invalid
	}
	return c, saved, nil
}

func (s *Service) listGrants(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in api.ListInput) (api.Page[GrantRecord], error) {
	page := api.Page[GrantRecord]{Items: []GrantRecord{}, Gaps: []string{}}
	if q.TargetID != scope.OwnerID || auth.TenantID != scope.TenantID || in.Limit < 1 || in.Limit > 100 {
		return page, api.E("invalid_request", "grant_list_scope_or_limit_invalid")
	}
	if s.Ports.GrantMetadataGate == nil {
		return page, api.E("unsupported", "grant_metadata_gate_unconfigured")
	}
	fixedExpiry, bound := runtime.QueryBindingExpiry(ctx)
	if !bound {
		return page, api.E("unsupported", "query_identity_store_unavailable")
	}
	principal, err := grantListIdentity(scope, auth)
	if err != nil {
		return page, err
	}
	queryDigest, err := api.Digest(q)
	if err != nil {
		return page, err
	}
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		visibility, e := s.Ports.GrantMetadataGate.VisibilityTx(ctx, tx, auth)
		if e != nil {
			return e
		}
		if visibility == "" {
			return api.E("forbidden", "grant_metadata_visibility_unavailable")
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		if !now.Before(fixedExpiry) {
			return api.E("cursor_expired", "query_binding_expired")
		}
		view, e := readGrantListView(ctx, tx, now)
		if e != nil {
			return e
		}
		expiry := fixedExpiry
		if !view.Boundary.IsZero() && view.Boundary.Before(expiry) {
			expiry = view.Boundary
		}
		expected := grantListQuery{Scope: scope, QueryID: q.QueryID, QueryDigest: queryDigest, PrincipalDigest: principal, VisibilityDigest: visibility, ViewDigest: view.Digest, CollectionRevision: view.Revision, Limit: in.Limit, QueryExpiresAt: api.Time(fixedExpiry), CursorExpiresAt: api.Time(expiry)}
		var slot, after string
		var saved grantListQuery
		if in.Cursor == "" {
			slot, saved, e = acquireGrantListQuery(ctx, tx, expected, now)
			if e == nil && (saved.QueryDigest != queryDigest || saved.QueryExpiresAt != expected.QueryExpiresAt) {
				return api.E("idempotency_conflict", "grant_list_original_query_changed")
			}
		} else {
			var cursor grantListCursor
			cursor, saved, e = parseGrantListCursor(ctx, tx, in.Cursor)
			slot, after = cursor.Slot, cursor.After
			if e == nil {
				originalExpiry, parseErr := api.ParseTime(cursor.ExpiresAt)
				if parseErr != nil || !now.Before(originalExpiry) {
					return api.E("cursor_expired", "grant_list_cursor_expired")
				}
				if originalExpiry.Before(expiry) {
					expiry = originalExpiry
				}
			}
		}
		if e != nil {
			return e
		}
		key, decodeErr := base64.RawURLEncoding.DecodeString(saved.Key)
		if decodeErr != nil || len(key) != 32 || !api.ValidID(saved.Nonce) {
			return api.E("dependency_unavailable", "grant_list_original_snapshot_corrupted")
		}
		if saved.Scope != scope || saved.PrincipalDigest != principal || saved.Limit != in.Limit || saved.VisibilityDigest != visibility {
			return api.E("cursor_expired", "grant_list_cursor_visibility_changed")
		}
		if saved.ViewDigest != view.Digest || saved.CollectionRevision != view.Revision {
			return api.E("snapshot_required", "grant_list_collection_changed")
		}
		originalExpiry, e := api.ParseTime(saved.CursorExpiresAt)
		if e != nil || !now.Before(originalExpiry) {
			return api.E("cursor_expired", "grant_list_cursor_expired")
		}
		if originalExpiry.Before(expiry) {
			expiry = originalExpiry
		}
		foundAfter := after == ""
		page.CollectionRevision, page.Exhausted = view.Revision, true
		for _, entry := range view.Entries {
			if !auth.HasRole("grant_authority") && !api.Equal(entry.Grant.SubjectRef, auth.Ref(scope.OwnerID)) {
				continue
			}
			foundAfter = foundAfter || entry.Grant.GrantID == after
			if entry.Grant.GrantID <= after {
				continue
			}
			if uint64(len(page.Items)) == in.Limit {
				page.Exhausted = false
				break
			}
			page.Items = append(page.Items, GrantRecord{Grant: entry.Grant, OnceConsumed: entry.Usage.OnceConsumed, Spent: entry.Usage.Spent, Reserved: entry.Usage.Reserved})
		}
		if !foundAfter {
			return api.E("cursor_expired", "grant_list_cursor_invalid")
		}
		if !page.Exhausted {
			page.NextCursor = encodeGrantListCursor(saved, grantListCursor{slot, saved.Nonce, page.Items[len(page.Items)-1].Grant.GrantID, api.Time(expiry)})
		}
		now, e = tx.Now(ctx)
		if e != nil {
			return e
		}
		if !now.Before(expiry) {
			return api.E("cursor_expired", "grant_list_cursor_expired")
		}
		current, e := readGrantListView(ctx, tx, now)
		if e != nil {
			return e
		}
		if current.Digest != view.Digest {
			return api.E("snapshot_required", "grant_list_collection_changed")
		}
		if len(api.Raw(page)) > api.MaxJSONBytes {
			return api.E("overloaded", "grant_list_metadata_limit")
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return page, runtime.ErrCommitUnknown
	}
	return page, err
}
