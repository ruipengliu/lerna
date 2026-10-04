package development

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

// 在原Claim已经提交后记录此用例必须关闭的未发送Decision责任；其余作业
// 沿已有清单。完整Store与QueryBindingStore透传，不能丢失公开查询绑定能力。
type noChildResponsibilityStore struct {
	*configuredReportInventoryStore
	runtime.QueryBindingStore
}

func (s *noChildResponsibilityStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, max int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	works, status, err := s.configuredReportInventoryStore.Claim(ctx, scope, holder, kinds, max, lease)
	if status == runtime.Committed && err == nil {
		s.inventory.mu.Lock()
		defer s.inventory.mu.Unlock()
		for _, work := range works {
			if work.Job.Kind != task.JobDispatchDecision {
				continue
			}
			if _, known := s.inventory.jobs[work.Job.JobID]; !known && len(s.inventory.jobs) >= 1024 {
				s.inventory.overflow = true
				continue
			}
			s.inventory.jobs[work.Job.JobID] = work.Job
		}
	}
	return works, status, err
}

func observeNoChildResponsibilities(t *testing.T, store runtime.Store, inventory *configuredReportJobInventory) runtime.Store {
	t.Helper()
	queries, ok := store.(runtime.QueryBindingStore)
	if !ok {
		t.Fatal("original no-child store lost query binding capability")
	}
	return &noChildResponsibilityStore{&configuredReportInventoryStore{Store: store, inventory: inventory}, queries}
}

// 原可复用Session已经存在；预算关闭先于新目标的真实Task创建Job。
func TestConfiguredRemoteSessionBudgetCloseBeforeLateChildCannotReopenWork(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			fixtureStart := time.Now()
			fixtureCtx, fixtureCancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer fixtureCancel()
			phaseStart := time.Now()
			a, b, p := configuredAgentPairWithParentDriverContext(t, driver, fixtureCtx)
			t.Logf("original_no_child_fixture_phase=pair elapsed=%s total=%s", time.Since(phaseStart), time.Since(fixtureStart))
			phaseStart = time.Now()
			permission := approveRemoteDelegationGrant(fixtureCtx, t, a, b.app.Scope.OwnerID)
			t.Logf("original_no_child_fixture_phase=grant elapsed=%s total=%s", time.Since(phaseStart), time.Since(fixtureStart))
			phaseStart = time.Now()
			p = configureRemoteReuseProfile(fixtureCtx, t, a, b, p, []api.ObjectRef{permission})
			t.Logf("original_no_child_fixture_phase=reuse_profile elapsed=%s total=%s", time.Since(phaseStart), time.Since(fixtureStart))
			phaseStart = time.Now()
			handle, _ := configureOriginalRemoteSession(fixtureCtx, t, a, b, p)
			t.Logf("original_no_child_fixture_phase=original_session elapsed=%s total=%s", time.Since(phaseStart), time.Since(fixtureStart))
			if err := fixtureCtx.Err(); err != nil {
				t.Fatal("original no-child fixture preparation exceeded its explicit three-minute boundary", err)
			}
			fixtureCancel()
			// 夹具准备与实际场景观察分别有界；业务Task/命令/创建期限从未续期。
			observerStart := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			t.Logf("original_total_2m_case_boundary_changed=true fixture_budget_seconds=180 observer_budget_seconds=120 fixture_elapsed=%s", time.Since(fixtureStart))
			// Session配置会真实重开两个App；只包装最终实例的实际Claim路径。
			parentJobs := &configuredReportJobInventory{jobs: make(map[string]api.Job)}
			childJobs := &configuredReportJobInventory{jobs: make(map[string]api.Job)}
			a.app.Store = observeNoChildResponsibilities(t, a.app.Store, parentJobs)
			b.app.Store = observeNoChildResponsibilities(t, b.app.Store, childJobs)
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
			var originalPacket collaboration.RemoteCreateInput
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
					originalPacket = packet
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
			var parentClosure *task.ClosureView
			for ctx.Err() == nil {
				configuredAgentStep(ctx, t, b, task.JobDispatchDecision, task.JobAllocation, task.JobBilling, collaboration.JobRemoteProof, proofJob)
				configuredAgentStep(ctx, t, a, task.JobDispatchDecision, task.JobDelegation, task.JobAllocation, task.JobBilling, collaboration.JobRemoteProof, proofJob)
				state, stateErr := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
				if stateErr != nil {
					t.Fatal("original no-child closure", stateErr)
				}
				current, readErr := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.DelegationID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if state.Fact.GoalWorkClosed && state.Fact.EffectsClosed && state.Fact.TransfersClosed && state.Fact.UsageFinal && state.Fact.ClosureRef != nil && current.ClosureRef != nil {
					allocation, err := a.app.Task.AllocationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.AllocationRef.ObjectID)
					if err != nil {
						t.Fatal("original parent allocation", err)
					}
					parentActual, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
					if err != nil {
						t.Fatal("original cancelled parent", err)
					}
					grant = configuredAgentGrant(ctx, t, a, permission)
					if allocation.State != "settled" || parentActual.AccountingOpen || !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "0"}}) || !api.Equal(grant.Spent, []api.Amount{{Unit: "USD", Value: "0"}}) {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					for _, unit := range parentActual.Budget {
						if unit.Reserved != "0" || unit.Spent != "0" {
							t.Fatal("no-child parent budget did not retain exact zero cost")
						}
					}
					if parentClosure == nil {
						// 只封存一次真正父 Task Closure，不让轮询产生新出版责任。
						closed, closureErr := a.app.Task.Closure(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, a.app.Scope.Ref(parent.TaskID, parentActual.Revision))
						if closureErr != nil || !closed.GoalWorkClosed || !closed.EffectsClosed || closed.AccountingOpen {
							t.Fatal("original parent Task Closure incomplete", closureErr)
						}
						parentClosure = &closed
					}
					if state.Task != nil || state.TaskClosure != nil || state.Incoming == nil || state.Incoming.TaskRef != nil || state.Incoming.Gate != "closed" || state.Incoming.ClosurePending || state.Incoming.ClosureRef == nil || state.AllocationClosure == nil || state.AllocationClosureRef == nil || state.DelegationClosure == nil || state.DelegationClosureRef == nil || state.RejectedCreation == nil || allocation.ClosureRef == nil {
						t.Fatal("genuine no-child Closure must retain permanent rejection without a fabricated Task Closure")
					}
					packetDigest, err := api.Digest(originalPacket)
					if err != nil || state.PacketDigest != packetDigest || state.RejectedCreation.PacketDigest != packetDigest || state.RejectedCreation.CommandRef != b.app.Scope.Ref(originalCID, 1) || state.RejectedCreation.AllocationRef != d.AllocationRef || state.RejectedCreation.ParentTaskRef != d.ParentTaskRef || !api.Equal(state.RejectedCreation.Receipt, rejected.Receipt) || state.RejectedCreation.ChildTaskID != originalPacket.ChildTaskID {
						t.Fatal("permanent refusal lost original command, receipt or immutable packet", err)
					}
					if *state.AllocationClosureRef != *state.Incoming.ClosureRef || *allocation.ClosureRef != *state.Incoming.ClosureRef || *state.DelegationClosureRef != *current.ClosureRef || state.DelegationClosure.AllocationClosureRef != *state.AllocationClosureRef || !state.AllocationClosure.SpendingClosed || !api.Equal(state.AllocationClosure.FinalUsage, []api.Amount{{Unit: "USD", Value: "0"}}) || state.Fact.Usage == nil || !state.Fact.Usage.UsageFinal || !state.Fact.Usage.SpendingClosed || !api.Equal(state.Fact.Usage.Cumulative, state.AllocationClosure.FinalUsage) {
						t.Fatal("original three-layer zero-cost Closure does not agree")
					}
					proofs := []api.ContentRef{state.RejectedCreation.ProofRef, state.AllocationClosure.ProofRef}
					if proofs[0] == proofs[1] || !api.Equal(state.DelegationClosure.ProofRefs, proofs) || !api.Equal(state.Fact.Usage.ProofRefs, proofs[1:]) {
						t.Fatal("original refusal and allocation proof identities differ")
					}
					requiredProofs := append(append([]api.ContentRef{}, proofs...), parentClosure.ProofRef)
					if !configuredReportProofsPublished(ctx, t, a, b, requiredProofs) {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					parentDone, parentStates, parentErr := configuredReportJobsDone(ctx, a, parentJobs)
					childDone, childStates, childErr := configuredReportJobsDone(ctx, b, childJobs)
					if parentErr != nil || childErr != nil {
						t.Fatal("original no-child business Jobs", errors.Join(parentErr, childErr))
					}
					if !parentDone || !childDone {
						time.Sleep(20 * time.Millisecond)
						continue
					}
					if !configuredReportHasJob(parentStates, task.JobDelegation, "delegation/"+d.DelegationID) || !configuredReportHasCorrection(childStates, d.AllocationRef.ObjectID) {
						t.Fatal("original delegation/correction Job identity not observed")
					}
					finalHandle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
					if err != nil || finalHandle.ChildSessionRef == nil || *finalHandle.ChildSessionRef != *handle.ChildSessionRef || finalHandle.ActiveDelegationRef == nil || *finalHandle.ActiveDelegationRef != *output.DelegationRef {
						t.Fatal("closing changed original Session/delegation mapping", err)
					}
					// 使用保存的原配置、原凭据与原令牌重开两个真实 App.Run；构造本身不得有 RPC。
					a.reopenOriginal(t)
					b.reopenOriginal(t)
					reopened, err := b.app.Store.LookupCommand(ctx, b.app.Scope, originalCID)
					if err != nil || !api.Equal(reopened, rejected) {
						t.Fatal("reopen changed the original rejected command", err)
					}
					reopenedTasks, err := b.app.Task.List(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, task.TaskListInput{Limit: 100})
					if err != nil || !reopenedTasks.Exhausted || reopenedTasks.Partial || len(reopenedTasks.Gaps) != 0 || reopenedTasks.NextCursor != "" || !api.Equal(reopenedTasks.Items, baseline.Items) {
						t.Fatal("reopen introduced a late child", err)
					}
					reopenedHandle, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
					if err != nil || !api.Equal(reopenedHandle, finalHandle) {
						t.Fatal("reopen changed the original Session mapping", err)
					}
					reopenedState, err := a.app.RemoteAgent.State(ctx, a.app.Scope, current)
					if err != nil || reopenedState.Task != nil || reopenedState.TaskClosure != nil || !api.Equal(reopenedState.RejectedCreation, state.RejectedCreation) || !api.Equal(reopenedState.AllocationClosure, state.AllocationClosure) || !api.Equal(reopenedState.DelegationClosure, state.DelegationClosure) {
						t.Fatal("reopen changed durable no-child Closure facts", err)
					}
					if !configuredReportProofsPublished(ctx, t, a, b, requiredProofs) {
						t.Fatal("reopen lost the original permitted proof bytes")
					}
					grant = configuredAgentGrant(ctx, t, a, permission)
					if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "0"}}) || !api.Equal(grant.Spent, []api.Amount{{Unit: "USD", Value: "0"}}) {
						t.Fatal("reopen returned consumed once permission or changed settled costs")
					}
					reopenedAllocation, err := a.app.Task.AllocationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, d.AllocationRef.ObjectID)
					if err != nil || reopenedAllocation.State != "settled" || !api.Equal(reopenedAllocation, allocation) {
						t.Fatal("reopen changed the original settled allocation", err)
					}
					reopenedParent, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
					if err != nil || reopenedParent.AccountingOpen || !api.Equal(reopenedParent.Budget, parentActual.Budget) {
						t.Fatal("reopen changed original parent accounting", err)
					}
					parentDone, parentStates, parentErr = configuredReportJobsDone(ctx, a, parentJobs)
					childDone, childStates, childErr = configuredReportJobsDone(ctx, b, childJobs)
					if parentErr != nil || childErr != nil || !parentDone || !childDone {
						t.Fatal("reopen left original no-child Jobs pending", errors.Join(parentErr, childErr))
					}
					t.Logf("original Session=%s send=%s refused_child_command=%s allocation=%s closure=%s no_child=true original_cfg_token_reopen=true required_proof_count=%d parent_jobs=%s child_jobs=%s", handle.ChildSessionRef.ObjectID, send.CommandID, originalCID, d.AllocationRef.ObjectID, current.ClosureRef.ObjectID, len(requiredProofs), api.Raw(parentStates), api.Raw(childStates))
					t.Logf("original_no_child_observer_elapsed=%s original_case_total_elapsed=%s", time.Since(observerStart), time.Since(fixtureStart))
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("original late child responsibility did not reach its own Closure", ctx.Err())
		})
	}
}
