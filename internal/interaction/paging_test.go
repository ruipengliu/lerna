package interaction_test

import (
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
)

func TestSessionCursorRejectsCollectionChangeSubjectAndTampering(t *testing.T) {
	f := newApplication(t)
	for i := 0; i < 2; i++ {
		f.command(t, "session.create", f.scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: f.config})
	}
	page, e := f.s.ListSessions(f.ctx, f.store, f.scope, f.auth, api.ListInput{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 1 || page.Exhausted || page.NextCursor == "" {
		t.Fatalf("incomplete first page %+v", page)
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
