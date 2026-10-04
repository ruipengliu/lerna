package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"testing"
	"time"
)

// close 保持原已接纳创建责任；迟到原 Session 只能记账，不能重开 handle。
func TestConfiguredRemoteChildClosedBeforePrepareKeepsOriginalSession(t *testing.T) {
	verifyConfiguredRemoteChildClosedBeforePrepareKeepsOriginalSession(t, "sqlite")
}

func verifyConfiguredRemoteChildClosedBeforePrepareKeepsOriginalSession(t *testing.T, parentDriver string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, b, p := configuredAgentPairWithParentDriver(t, parentDriver)
	access, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("Exact original session creation only."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	binding := collaboration.RemoteSessionBinding{ProfileRef: p.ProfileRef, SessionConfigRef: p.Values.PolicyRef, AgentBindingRef: p.Values.AgentBindingRef, InstallLockRef: p.Values.InstallLockRef, AccessScopeRef: access}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Sessions = []collaboration.RemoteSessionBinding{binding}
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.persistPrivateConfiguration(t)
		e.startRun(t)
	}
	baseline, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
	if err != nil || !baseline.Exhausted || baseline.Partial || baseline.NextCursor != "" || len(baseline.Gaps) != 0 {
		t.Fatalf("original Session baseline is incomplete: %v", err)
	}
	baselineSessions := map[string]api.Session{}
	for _, session := range baseline.Items {
		baselineSessions[session.SessionID] = session
	}
	id := api.NewID("child")
	create := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.create", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildCreateInput{ChildID: id, SessionOwnerID: b.app.Scope.OwnerID, SessionConfigRef: binding.SessionConfigRef, AgentBindingRef: binding.AgentBindingRef, InstallLockRef: binding.InstallLockRef, AccessScopeRef: binding.AccessScopeRef, PrepareDeadline: api.Time(time.Now().Add(time.Minute))})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(create))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original create: %s %v %v", receipt.Stage, err, receipt.Error)
	}
	handle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	originalSessionCommand := handle.SessionCommandRef
	close := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.close", TargetID: id, ExpectedRevision: &handle.Revision, ExpiresAt: create.ExpiresAt, Payload: api.Raw(task.ChildCloseInput{ChildID: id, CancelActive: false, Reason: "Close only future sends; keep original responsibility."})}
	receipt, err = a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(close))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original close: %s %v %v", receipt.Stage, err, receipt.Error)
	}
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, a, task.JobChildPrepare)
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteSessionCreate)
		handle, err = a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
		if err != nil {
			t.Fatal(err)
		}
		if handle.State != "closed" || handle.SessionCommandRef != originalSessionCommand {
			t.Fatal("late callback changed closed original identity")
		}
		if handle.ChildSessionRef != nil {
			replay, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(create))
			if err != nil || replay.Stage != "applied" {
				t.Fatalf("original create did not settle: %s %v", replay.Stage, err)
			}
			sessions, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions.Items) != len(baseline.Items)+1 || !sessions.Exhausted || sessions.Partial || sessions.NextCursor != "" || len(sessions.Gaps) != 0 {
				t.Fatal("original session creation was not unique and complete")
			}
			matches := 0
			for _, session := range sessions.Items {
				if session.SessionID == handle.ChildSessionRef.ObjectID {
					matches++
					continue
				}
				original, ok := baselineSessions[session.SessionID]
				if !ok || !api.Equal(original, session) {
					t.Fatal("original creation changed another Session")
				}
			}
			if matches != 1 {
				t.Fatal("original Session reference is not unique")
			}
			before := handle
			a.reopenOriginal(t)
			b.reopenOriginal(t)
			handle, err = a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
			if err != nil || !api.Equal(handle, before) {
				t.Fatal("original cfg/token reopen changed closed handle")
			}
			t.Logf("original child=%s session_command=%s session=%s closed_revision=%d", id, originalSessionCommand.ObjectID, handle.ChildSessionRef.ObjectID, handle.Revision)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("original accepted preparation did not settle after close")
}
