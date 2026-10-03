package interaction_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

func TestSessionCursorRejectsCollectionChangeSubjectAndTampering(t *testing.T) {
	f := newApplication(t)
	for i := 0; i < 2; i++ {
		f.command(t, "session.create", f.scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: f.config})
	}
	roles, _ := api.Digest(f.auth.Roles)
	queryDigest, _ := api.Digest(api.ListInput{Limit: 1})
	bindings := f.store.(runtime.QueryBindingStore)
	binding, status, e := bindings.BindQuery(f.ctx, f.scope, runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, RolesDigest: roles, QueryDigest: queryDigest, TTL: 5 * time.Minute})
	if e != nil || status != runtime.Committed {
		t.Fatalf("bind query %s %v", status, e)
	}
	ctx := runtime.WithQueryBinding(f.ctx, binding)
	page, e := f.s.ListSessions(ctx, f.store, f.scope, f.auth, api.ListInput{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 1 || page.Exhausted || page.NextCursor == "" {
		t.Fatalf("incomplete first page %+v", page)
	}
	digest, _ := api.Digest(page)
	if _, status, e = bindings.SealQuery(f.ctx, f.scope, binding, digest); e != nil || status != runtime.Committed {
		t.Fatalf("seal query %s %v", status, e)
	}
	time.Sleep(10 * time.Millisecond)
	repeated, e := f.s.ListSessions(ctx, f.store, f.scope, f.auth, api.ListInput{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	repeatedDigest, _ := api.Digest(repeated)
	if repeatedDigest != digest || repeated.NextCursor != page.NextCursor {
		t.Fatal("unchanged page drifted on the same bound query")
	}
	if _, status, e = bindings.SealQuery(f.ctx, f.scope, binding, repeatedDigest); e != nil || status != runtime.Committed {
		t.Fatalf("repeated result changed %s %v", status, e)
	}
	wrong := f.auth
	wrong.SubjectID = api.NewID("subject")
	if _, e = f.s.ListSessions(f.ctx, f.store, f.scope, wrong, api.ListInput{Limit: 1, Cursor: page.NextCursor}); !api.IsCode(e, "cursor_expired") {
		t.Fatalf("cursor crossed subject %v", e)
	}
	if _, e = f.s.ListSessions(f.ctx, f.store, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: page.NextCursor + "x"}); !api.IsCode(e, "cursor_expired") {
		t.Fatalf("tampered cursor accepted %v", e)
	}
	one := uint64(1)
	f.command(t, "session.archive", f.session, &one, interaction.SessionControlInput{Reason: "并发修改"})
	if _, e = f.s.ListSessions(f.ctx, f.store, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: page.NextCursor}); !api.IsCode(e, "snapshot_required") {
		t.Fatalf("changed collection reused cursor %v", e)
	}
}

func TestSessionReadPagesEveryBranchAndKeepsOriginalCursorAfterReopen(t *testing.T) {
	f := newApplication(t)
	for i := 0; i < 100; i++ {
		f.command(t, "session.branch.create", f.session, nil, interaction.CreateBranchInput{BranchID: api.NewID("branch"), SourceBranchRef: f.scope.Ref(f.branch, 1), ExpectedSourceRevision: 1, ConfigRef: f.config})
	}
	roles, _ := api.Digest(f.auth.Roles)
	queryDigest, _ := api.Digest(interaction.ReadInput{})
	binding, status, e := f.store.(runtime.QueryBindingStore).BindQuery(f.ctx, f.scope, runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, RolesDigest: roles, QueryDigest: queryDigest, TTL: 5 * time.Minute})
	if e != nil || status != runtime.Committed {
		t.Fatalf("bind session read %s %v", status, e)
	}
	ctx := runtime.WithQueryBinding(f.ctx, binding)
	first, e := f.s.ReadSession(ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil || first.BranchesComplete || len(first.Branches) != 100 || first.BranchesCursor == "" || first.Sequence != 0 {
		t.Fatalf("branches were silently truncated %+v %v", first, e)
	}
	f.reopen(t)
	repeated, e := f.s.ReadSession(ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil || !api.Equal(first, repeated) {
		t.Fatalf("unchanged bound read drifted after reopening %+v %v", repeated, e)
	}
	next, e := f.s.ListBranches(ctx, f.store, f.scope, f.auth, f.session, api.ListInput{Limit: 100, Cursor: first.BranchesCursor})
	if e != nil || !next.Exhausted || len(next.Items) != 1 || next.CollectionRevision != first.BranchesCollectionRevision {
		t.Fatalf("remaining branch was lost %+v %v", next, e)
	}
	for _, branch := range first.Branches {
		if branch.BranchID == next.Items[0].BranchID {
			t.Fatal("continuation returned a branch twice")
		}
	}
	f.command(t, "session.branch.create", f.session, nil, interaction.CreateBranchInput{BranchID: api.NewID("branch"), SourceBranchRef: f.scope.Ref(f.branch, 1), ExpectedSourceRevision: 1, ConfigRef: f.config})
	if _, e = f.s.ListBranches(ctx, f.store, f.scope, f.auth, f.session, api.ListInput{Limit: 100, Cursor: first.BranchesCursor}); !api.IsCode(e, "snapshot_required") {
		t.Fatalf("old cursor crossed a branch collection change %v", e)
	}
}

func TestSessionCursorKeepsFirstDeadlineAcrossThreePagesAndReopen(t *testing.T) {
	f := newApplication(t)
	for i := 0; i < 2; i++ {
		f.command(t, "session.create", f.scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: f.config})
	}
	// 只固定受信时钟；每页的集合版本、关系和当前权限仍从真实数据库读取。
	clock := sessionClockStore{Store: f.store, QueryBindingStore: f.store.(runtime.QueryBindingStore), now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	first, e := f.s.ListSessions(f.ctx, clock, f.scope, f.auth, api.ListInput{Limit: 1})
	if e != nil || len(first.Items) != 1 || first.Exhausted || first.NextCursor == "" {
		t.Fatalf("first page %+v %v", first, e)
	}
	clock.now = time.Date(2026, 10, 3, 12, 9, 0, 0, time.UTC)
	second, e := f.s.ListSessions(f.ctx, clock, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: first.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Exhausted || second.NextCursor == "" || second.Items[0].SessionID == first.Items[0].SessionID {
		t.Fatalf("second page before original deadline %+v %v", second, e)
	}
	f.reopen(t)
	clock.Store, clock.QueryBindingStore = f.store, f.store.(runtime.QueryBindingStore)
	clock.now = time.Date(2026, 10, 3, 12, 11, 0, 0, time.UTC)
	if third, e := f.s.ListSessions(f.ctx, clock, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: second.NextCursor}); !api.IsCode(e, "cursor_expired") {
		t.Fatalf("continuation extended the original 12:10 deadline: %+v %v", third, e)
	}
}

func TestSessionCursorUsesEarlierOfOriginalAndCurrentQueryBindingDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		firstTTL, currentTTL time.Duration
	}{
		{name: "later_binding_preserves_original", firstTTL: time.Minute, currentTTL: 5 * time.Minute},
		{name: "earlier_binding_tightens_deadline", firstTTL: 5 * time.Minute, currentTTL: time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplication(t)
			for i := 0; i < 2; i++ {
				f.command(t, "session.create", f.scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: f.config})
			}
			roles, _ := api.Digest(f.auth.Roles)
			bind := func(ttl time.Duration, input api.ListInput) runtime.QueryBinding {
				t.Helper()
				digest, _ := api.Digest(input)
				binding, status, e := f.store.(runtime.QueryBindingStore).BindQuery(f.ctx, f.scope, runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, RolesDigest: roles, QueryDigest: digest, TTL: ttl})
				if e != nil || status != runtime.Committed {
					t.Fatalf("bind query %s %v", status, e)
				}
				return binding
			}
			original := bind(tc.firstTTL, api.ListInput{Limit: 1})
			originalExpiry, e := api.ParseTime(original.ExpiresAt)
			if e != nil {
				t.Fatal(e)
			}
			clock := sessionClockStore{Store: f.store, QueryBindingStore: f.store.(runtime.QueryBindingStore), now: originalExpiry.Add(-tc.firstTTL)}
			first, e := f.s.ListSessions(runtime.WithQueryBinding(f.ctx, original), clock, f.scope, f.auth, api.ListInput{Limit: 1})
			if e != nil || len(first.Items) != 1 || first.Exhausted || first.NextCursor == "" {
				t.Fatalf("first bound page %+v %v", first, e)
			}
			current := bind(tc.currentTTL, api.ListInput{Limit: 1, Cursor: first.NextCursor})
			currentExpiry, e := api.ParseTime(current.ExpiresAt)
			if e != nil {
				t.Fatal(e)
			}
			deadline := originalExpiry
			if tc.currentTTL < tc.firstTTL {
				deadline = currentExpiry
			}
			clock.now = deadline.Add(-10 * time.Second)
			second, e := f.s.ListSessions(runtime.WithQueryBinding(f.ctx, current), clock, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: first.NextCursor})
			if e != nil || len(second.Items) != 1 || second.Exhausted || second.NextCursor == "" || second.Items[0].SessionID == first.Items[0].SessionID {
				t.Fatalf("continuation before the earlier deadline %+v %v", second, e)
			}
			clock.now = deadline.Add(10 * time.Second)
			later := bind(5*time.Minute, api.ListInput{Limit: 1, Cursor: second.NextCursor})
			if third, e := f.s.ListSessions(runtime.WithQueryBinding(f.ctx, later), clock, f.scope, f.auth, api.ListInput{Limit: 1, Cursor: second.NextCursor}); !api.IsCode(e, "cursor_expired") {
				t.Fatalf("third page crossed the earlier deadline despite a later binding: %+v %v", third, e)
			}
		})
	}
}
