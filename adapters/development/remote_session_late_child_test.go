package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"testing"
	"time"
)

// 原可复用Session已经存在；预算关闭先于新目标的真实Task创建Job。
func TestConfiguredRemoteSessionBudgetCloseBeforeLateChildCannotReopenWork(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			a, b, p := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			p = configureRemoteReuseProfile(ctx, t, a, b, p, []api.ObjectRef{permission})
			handle, _ := configureOriginalRemoteSession(ctx, t, a, b, p)
			baseline, err := b.app.Task.List(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, task.TaskListInput{Limit: 100})
			if err != nil || !baseline.Exhausted || baseline.Partial || len(baseline.Gaps) != 0 || baseline.NextCursor != "" {
				t.Fatal("original receiver Task baseline incomplete", err)
			}
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			send := remoteSessionGoalCommand(a, b, p, handle, parent, goal, permission, 0)
			receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(send))
			var output task.ChildOutput
			if err != nil || receipt.Stage != "applied" || api.Decode(receipt.Output, &output) != nil || output.DelegationRef == nil {
				t.Fatal("original reusable Session send did not persist", err)
			}
			configuredAgentStep(ctx, t, a, task.JobDelegation)
			d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, output.DelegationRef.ObjectID)
			if err != nil || !d.Sent || d.ChildTaskRef != nil {
				t.Fatal("original create must be sent but child Task must not exist yet", err)
			}
			entries, hasMore, err := a.app.remoteAgents.journals[0].Pending(ctx, 100)
			if err != nil || hasMore {
				t.Fatal("original SDK pending creation set incomplete", err)
			}
			originalCID := ""
			for _, entry := range entries {
				if entry.Command.Method == "collaboration.create" && entry.Command.TargetID == d.CreationKey {
					if originalCID != "" || entry.Receipt == nil || entry.Receipt.Stage != "accepted" {
						t.Fatal("original remote creation not uniquely accepted")
					}
					var packet collaboration.RemoteCreateInput
					if api.Decode(entry.Command.Payload, &packet) != nil || packet.SessionContext == nil || packet.SessionContext.SessionRef != *handle.ChildSessionRef || packet.OriginalCommandRef != a.app.Scope.Ref(send.CommandID, 1) {
						t.Fatal("late child lost original Session/send mapping")
					}
					originalCID = entry.Command.CommandID
				}
			}
			if originalCID == "" {
				t.Fatal("original accepted child command missing from SDK journal")
			}
			configuredAgentControl(ctx, t, a, parent.TaskID, "cancel")
			// 只推进原父控制及委派责任；原接收端Task创建保持未执行。
			for ctx.Err() == nil {
				configuredAgentStep(ctx, t, a, task.JobControl, task.JobDelegation, task.JobAllocation)
				incoming, readErr := b.app.Task.IncomingRead(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, d.AllocationRef)
				if readErr == nil && incoming.Gate == "closed" {
					if incoming.TaskRef != nil {
						t.Fatal("closing created a replacement child")
					}
					break
				}
				if readErr != nil && !api.IsCode(readErr, "not_found") {
					t.Fatal("original incoming gate query", readErr)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if ctx.Err() != nil {
				t.Fatal("original budget did not close before late create", ctx.Err())
			}
			configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate)
			rejected, err := b.app.Store.LookupCommand(ctx, b.app.Scope, originalCID)
			if err != nil || rejected.Receipt.Stage != "rejected" || rejected.Receipt.Error == nil || rejected.Receipt.Error.Reason != "allocation_closed" {
				t.Fatal("original late creation did not receive genuine original rejection", err)
			}
			after, err := b.app.Task.List(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, task.TaskListInput{Limit: 100})
			if err != nil || !after.Exhausted || after.Partial || len(after.Gaps) != 0 || after.NextCursor != "" || !api.Equal(after.Items, baseline.Items) {
				t.Fatal("late child created work after original gate closed", err)
			}
			actual, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
			if err != nil || actual.ChildSessionRef == nil || *actual.ChildSessionRef != *handle.ChildSessionRef || actual.ActiveDelegationRef == nil || *actual.ActiveDelegationRef != *output.DelegationRef {
				t.Fatal("late refusal changed original Session or delegation mapping", err)
			}
			grant := configuredAgentGrant(ctx, t, a, permission)
			if !grant.OnceConsumed {
				t.Fatal("late refusal returned an already consumed once permission")
			}
			// 三层事实分别由原owner提供；绝不从parent cancelled或一页空集合猜Closure。
			for ctx.Err() == nil {
				configuredAgentStep(ctx, t, b, task.JobAllocation, task.JobBilling, collaboration.JobRemoteProof)
				configuredAgentStep(ctx, t, a, task.JobDelegation, task.JobAllocation, task.JobBilling)
				state, stateErr := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
				if stateErr != nil {
					t.Fatal("original no-child closure", stateErr)
				}
				current, readErr := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.DelegationID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if state.Fact.GoalWorkClosed && state.Fact.EffectsClosed && state.Fact.TransfersClosed && state.Fact.UsageFinal && state.Fact.ClosureRef != nil && current.ClosureRef != nil {
					t.Logf("original Session=%s send=%s refused_child_command=%s allocation=%s closure=%s no_child=true", handle.ChildSessionRef.ObjectID, send.CommandID, originalCID, d.AllocationRef.ObjectID, current.ClosureRef.ObjectID)
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("original late child responsibility did not reach its own Closure", ctx.Err())
		})
	}
}
