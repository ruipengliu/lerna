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
