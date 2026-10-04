package development

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 子方没有运行Task.advance。真实父命令/Grant Use/SDK journal与接纳Job
// 已持久化，后续每个拒绝窗口仅使用这次实际责任，不合成新创建或新额度。
func configuredRemoteUnadvancedChild(ctx context.Context, t *testing.T, driver string, receive bool) (*configuredAgentEndpoint, *configuredAgentEndpoint, api.ContentRef, api.Task, task.Delegation, api.ObjectRef, collaboration.RemoteCreateInput) {
	t.Helper()
	a, b, profile := configuredAgentPairWithParentDriver(t, driver)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	profile = configureRemoteFileScope(ctx, t, a, b, profile, permission)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original public delegation: %+v %v", receipt, err)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) {
		t.Fatal("original parent delegation Job missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	var packet collaboration.RemoteCreateInput
	for _, journal := range a.app.remoteAgents.journals {
		pending, more, err := journal.Pending(ctx, 128)
		if err != nil || more {
			t.Fatalf("bounded original SDK journal: more=%v err=%v", more, err)
		}
		for _, entry := range pending {
			if entry.Command.Method != "collaboration.create" || entry.Command.TargetID != id {
				continue
			}
			if packet.CreateCommandID != "" || api.Decode(entry.Command.Payload, &packet) != nil || packet.CreateCommandID != entry.Command.CommandID || entry.Receipt == nil || entry.Receipt.Stage != "accepted" || packet.OriginalCommandRef.ObjectID != original.CommandID || packet.AllocationRef != d.AllocationRef || packet.Input.Deadline != in.Deadline || !api.Equal(packet.Input, in) {
				t.Fatal("original journal lost exact create/allocation/parent command identity")
			}
		}
	}
	if packet.CreateCommandID == "" {
		t.Fatal("actual accepted original remote create journal entry missing")
	}
	if receive && !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original remote child acceptance Job missing")
	}
	t.Logf("original parent_driver=%s parent=%s delegation=%s allocation=%s create_command=%s child=%s grant=%s original_command=%s", driver, parent.TaskID, id, d.AllocationRef.ObjectID, packet.CreateCommandID, packet.ChildTaskID, permission.ObjectID, original.CommandID)
	return a, b, goal, parent, d, permission, packet
}

func TestConfiguredRemoteBudgetCloseBeforeLateOriginalCreateCannotReviveChild(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			a, b, _, parent, d, permission, packet := configuredRemoteUnadvancedChild(ctx, t, driver, false)
			allocation, err := a.app.Task.AllocationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.AllocationRef.ObjectID)
			if err != nil {
				t.Fatal(err)
			}
			// 真正原close命令先经HTTPS进入消费方；迟到的是已accepted的
			// 原create Job，不能更换创建键、额度或清理终态后再造新child。
			if err = a.app.RemoteAgent.CloseAllocation(ctx, a.app.Scope, allocation); err != nil {
				t.Fatal(err)
			}
			if !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
				t.Fatal("late original create responsibility missing")
			}
			peer := runtime.Auth{TenantID: b.app.Scope.TenantID, SubjectID: a.app.Scope.OwnerID, CredentialGeneration: 1, Roles: []string{"paired_agent"}}
			rejected, err := b.app.Dispatcher.Lookup(ctx, peer, packet.CreateCommandID)
			if err != nil || rejected.Stage != "rejected" || rejected.Error == nil || rejected.Error.Reason != "allocation_closed" {
				t.Fatalf("late original create revived or lost terminal receipt: %+v %v", rejected, err)
			}
			incoming, err := b.app.Task.IncomingRead(ctx, b.app.Store, b.app.Scope, peer, d.AllocationRef)
			if err != nil || incoming.Gate != "closed" || incoming.TaskRef != nil {
				t.Fatalf("original incoming gate revived: %+v %v", incoming, err)
			}
			if _, err = b.app.Task.Read(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, packet.ChildTaskID); !api.IsCode(err, "not_found") {
				t.Fatalf("late create installed a child Task: %v", err)
			}
			// 同原command字节回放只读原拒绝，不恢复一次许可或产生新Task。
			create := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: b.app.Scope.OwnerID, CommandID: packet.CreateCommandID, Method: "collaboration.create", TargetID: packet.CreationKey, ExpiresAt: packet.Input.Deadline, Payload: api.Raw(packet)}
			again, err := b.app.Dispatcher.Command(ctx, peer, api.Raw(create))
			if err != nil || !api.Equal(again, rejected) {
				t.Fatalf("original rejected create changed on replay: %+v %v", again, err)
			}
			grant := configuredAgentGrant(ctx, t, a, permission)
			actual, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
			if err != nil || actual.Status != "active" || actual.ResultRef != nil || !grant.OnceConsumed {
				t.Fatalf("early budget close synthesized parent success or refunded once: %+v %v", actual, err)
			}
			t.Logf("late_original_create_rejected=true incoming_closed_no_child=true original_once_consumed=true parent_result_absent=true receipt=%s", packet.CreateCommandID)
		})
	}
}

func TestConfiguredRemoteCurrentHolderRevocationOrSourceClosePreventsNewChildDecision(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, withdrawal := range []string{"holder_revoked", "source_closed"} {
			t.Run(driver+"_parent_sqlite_child/"+withdrawal, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				a, b, goal, _, d, permission, packet := configuredRemoteUnadvancedChild(ctx, t, driver, true)
				if withdrawal == "holder_revoked" {
					if err := b.app.Identity.Revoke(ctx, b.app.UserAuth); err != nil {
						t.Fatal(err)
					}
				} else {
					expected := uint64(1)
					closeCommand := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "content.close", TargetID: goal.ContentID, ExpectedRevision: &expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: goal, Reason: "original parent source withdrawn before child decision"})}
					receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(closeCommand))
					if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
						t.Fatalf("actual source closure: %+v %v", receipt, err)
					}
				}
				work, status, err := b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
				if err != nil || status != runtime.Committed || len(work) != 1 || work[0].Job.SourceRef.ObjectID != packet.ChildTaskID {
					t.Fatalf("original unadvanced child Job: %v %v", status, err)
				}
				handler, ok := b.app.Registry.Job(task.JobAdvance)
				if !ok {
					t.Fatal("original Task.advance handler missing")
				}
				err = handler(ctx, b.app.Store, b.app.Scope, work[0])
				if !api.IsCode(err, "forbidden") {
					t.Fatalf("current withdrawal did not reject the actual positive preparation: %v", err)
				}
				denial := err.Error()
				var found bool
				status, err = b.app.Store.Within(ctx, b.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
					_, current, err := b.app.Task.LatestTaskSnapshotTx(ctx, tx, b.app.ServiceAuth, packet.ChildTaskID)
					found = current
					return err
				})
				if err != nil || status != runtime.Committed || found {
					t.Fatalf("withdrawn positive entry installed a Decision Snapshot: found=%v %v %v", found, status, err)
				}
				facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, packet.ChildTaskID)
				if err != nil || len(facts.Operations) != 0 || len(facts.Delegations) != 0 {
					t.Fatalf("withdrawn original holder/source admitted new action: %v", err)
				}
				grant := configuredAgentGrant(ctx, t, a, permission)
				if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
					t.Fatalf("denied new start lost original pending liability: %+v", grant)
				}
				t.Logf("actual_current_denial=%s original_job=%s child=%s delegation=%s no_snapshot=true no_action=true original_once_reserve_preserved=true", denial, work[0].Job.JobID, packet.ChildTaskID, d.DelegationID)
			})
		}
	}
}
