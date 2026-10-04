package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"testing"
	"time"
)

// 明确错误的原child_goal_revision不能因可复用Session得到当前Goal的隐式授权。
func TestConfiguredRemoteSessionWrongChildGoalRevisionSettlesOriginalTransfer(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			_, d, child := sendRemoteSessionNewGoal(ctx, t, a, b, p, handle, parent, goal, permission, 0)
			handle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil {
				t.Fatal(err)
			}
			amendment, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "text/plain", []byte("This amendment carries the wrong original child Goal version."), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			wrongVersion := child.GoalRevision + 1
			send := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.send", TargetID: handle.ChildID, ExpectedRevision: &handle.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ChildSendInput{ChildID: handle.ChildID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "steer", ChildGoalRevision: wrongVersion, ContentRef: &amendment})}
			receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(send))
			if err != nil || receipt.Stage != "applied" {
				t.Fatalf("original versioned transfer not queued: %v %+v", err, receipt)
			}
			rows, err := a.app.Store.List(ctx, a.app.Scope, "task.transfers", handle.ChildID, "", 129)
			if err != nil || len(rows) != 1 {
				t.Fatal("original bounded transfer missing", err)
			}
			var transfer task.Transfer
			if err = rows[0].Decode(&transfer); err != nil || transfer.SourceSubmissionRef != a.app.Scope.Ref(send.CommandID, 1) || transfer.Input.ChildGoalRevision != wrongVersion {
				t.Fatal("original transfer version/source changed", err)
			}
			for ctx.Err() == nil {
				configuredAgentStep(ctx, t, a, task.JobChildTransfer, collaboration.JobRemoteInputSend)
				configuredAgentStep(ctx, t, b, collaboration.JobRemoteInputReceive, task.JobSteer)
				decision, e := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, transfer.CommandRef.ObjectID)
				if e != nil && !api.IsCode(e, "not_found") {
					t.Fatal("original parent command query", e)
				}
				if e == nil && decision.Stage == "rejected" {
					var current task.Transfer
					if _, e = a.app.Store.Read(ctx, a.app.Scope, "task.transfers", transfer.TransferID, 0, &current); e != nil {
						t.Fatal(e)
					}
					if current.State != "rejected" {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					if decision.Error == nil || decision.Error.Code != "revision_conflict" || decision.CommandID != transfer.CommandRef.ObjectID || current.RejectedReceipt == nil || !api.Equal(*current.RejectedReceipt, decision) {
						t.Fatalf("original Goal-version rejection missing: %+v", decision)
					}
					actual, e := b.app.Task.Read(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, child.TaskID)
					if e != nil || actual.GoalRevision != child.GoalRevision || actual.GoalRef != child.GoalRef {
						t.Fatal("wrong version changed original Goal", e)
					}
					facts, e := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, child.TaskID)
					if e != nil {
						t.Fatal(e)
					}
					for _, source := range facts.SourceRefs {
						if source.ContentRef == amendment {
							t.Fatal("rejected version admitted amendment source")
						}
					}
					mapped, e := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
					if e != nil || mapped.ChildSessionRef == nil || *mapped.ChildSessionRef != *handle.ChildSessionRef || mapped.ActiveDelegationRef == nil || *mapped.ActiveDelegationRef != a.app.Scope.Ref(d.DelegationID, 1) {
						t.Fatal("version refusal replaced original Session/Delegation", e)
					}
					original, e := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.DelegationID)
					if e != nil || original.AllocationRef != d.AllocationRef || !api.Equal(original.ChildTaskRef, d.ChildTaskRef) {
						t.Fatal("version refusal replaced original allocation/Task", e)
					}
					if !configuredAgentGrant(ctx, t, a, permission).OnceConsumed {
						t.Fatal("version refusal returned consumed once permission")
					}
					t.Logf("session=%s original_transfer=%s source=%s wrong_child_goal_revision=%d preserved_child_goal_revision=%d parent_rejection=%s:%s", handle.ChildSessionRef.ObjectID, transfer.CommandRef.ObjectID, send.CommandID, wrongVersion, actual.GoalRevision, decision.Error.Code, decision.Error.Reason)
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("original wrong-version responsibility did not settle", ctx.Err())
		})
	}
}
