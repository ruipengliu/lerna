package task_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 通过已登记的Task方法、受信原Executor事实和原Job观察串行准入。
// Content是真实文件介质；Brain边界只表示原接纳等待，不配置物理模型出口。
type countedPendingContext struct {
	h       *harness
	content *contentBridge
	calls   atomic.Uint64
	last    task.PreparedDecision
}

func (p *countedPendingContext) Prepare(ctx context.Context, scope runtime.Scope, _ runtime.Auth, current api.Task) (task.PreparedDecision, error) {
	p.calls.Add(1)
	prepared := p.h.prepared(current, "1")
	ref, err := p.content.Publish(ctx, scope, api.NewID("upload"), "application/json", api.Raw(prepared.Snapshot))
	prepared.SnapshotRef = ref
	p.last = prepared
	return prepared, err
}

func pendingDecisionHarness(t *testing.T, ctx context.Context, driver string, ports task.Ports) *harness {
	t.Helper()
	var store runtime.Store
	var err error
	if driver == "sqlite" {
		store, err = sqlite.Open(filepath.Join(t.TempDir(), "pending-decision.sqlite"))
	} else {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("requires actual PostgreSQL")
		}
		store, err = postgres.Open(ctx, dsn)
	}
	if err != nil {
		t.Fatal(err)
	}
	if driver == "sqlite" {
		err = store.(*sqlite.Store).Migrate(ctx)
	} else {
		err = store.(*postgres.Store).Migrate(ctx)
	}
	if err != nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	h := harnessForStore(t, store, ports)
	t.Cleanup(func() {
		if err := h.store.Close(); err != nil {
			t.Error(err)
		}
	})
	return h
}

func oneOriginalAdvance(t *testing.T, ctx context.Context, h *harness) runtime.Work {
	t.Helper()
	works, status, err := h.store.Claim(ctx, h.scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original advance claim: count=%d status=%v err=%v", len(works), status, err)
	}
	handler, ok := h.dispatch.Registry.Job(task.JobAdvance)
	if !ok {
		t.Fatal("original advance handler missing")
	}
	if err = handler(ctx, h.store, h.scope, works[0]); err != nil {
		t.Fatalf("original advance: %v", err)
	}
	return works[0]
}

func pendingSubmit(t *testing.T, ctx context.Context, h *harness) api.Task {
	t.Helper()
	command := h.command("task.submit", api.NewID("task"), nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: h.content("Keep the original target while its current decision is pending."), PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
	if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
		t.Fatalf("public original submit: %+v %v", receipt, err)
	}
	current, err := h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func isTaskRejection(err error, code, reason string) bool {
	var rejection *api.Error
	return errors.As(err, &rejection) && rejection.Code == code && rejection.Reason == reason
}

func TestCurrentPendingDecisionWaitsThroughOriginalEffectWake(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			content := &contentBridge{}
			compiler := &countedPendingContext{content: content}
			brain := &decisionDispatchBoundary{}
			proof := &localProofFixture{}
			h := pendingDecisionHarness(t, ctx, driver, task.Ports{Context: compiler, Content: content, Brain: brain, ActionAuthorization: proof})
			compiler.h = h
			configureContent(t, h, content)
			proof.install(t, h.scope)
			goal := h.content("Preserve this original target while its current decision is pending.")
			command := h.command("task.submit", api.NewID("task"), nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: goal, PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
			receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
			if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
				t.Fatalf("public original submit: %+v %v", receipt, err)
			}
			current, err := h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil {
				t.Fatal(err)
			}
			initial := h.prepared(current, "0")
			if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), initial); err != nil {
				t.Fatal(err)
			}
			action := preparedAction(h, "0", "original_safe_observation")
			action.SafeRequirementCheck = true
			admission, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: initial.DecisionID, Kind: "act", ReasonRef: goal, Actions: []task.PreparedAction{action}}, nil)
			if err != nil || admission.Outcome != "adopted" || len(admission.AdmittedOperationIDs) != 1 {
				t.Fatalf("original upstream observation responsibility: %+v %v", admission, err)
			}
			firstWork := oneOriginalAdvance(t, ctx, h)
			first := compiler.last
			// 此前已消费的零费用Decision无需物理请求；只处理当前原Decision的派发Job。
			drainKind(t, h, task.JobDispatchDecision)
			if compiler.calls.Load() != 1 || len(brain.dispatched) != 1 || brain.dispatched[0].DecisionID != first.DecisionID {
				t.Fatalf("fixture did not reach one original pending dispatch: calls=%d dispatched=%+v", compiler.calls.Load(), brain.dispatched)
			}
			before, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, current.TaskID)
			if err != nil || len(before.Reservations) != 3 || before.Budget[0].Reserved != "1" {
				t.Fatalf("original reservations: %+v %v", before, err)
			}
			operation := api.Operation{OperationID: action.OperationID, OwnerID: h.scope.OwnerID, TaskRef: h.scope.Ref(current.TaskID, current.Revision), Revision: 1, ExecutionState: "closed", Effect: "not_applied", MayApplyLater: false, Attempts: api.CollectionSummary{CollectionRevision: 1, Complete: true}, EvidenceRefs: []api.ContentRef{h.content("preapproved exact original observation")}, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: false, NextAction: "none"}
			if err = h.service.MergeOperation(ctx, h.store, h.scope, h.trusted(), operation); err != nil {
				t.Fatal(err)
			}
			repeatedWork := oneOriginalAdvance(t, ctx, h)
			after, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, current.TaskID)
			t.Logf("tenant=%s owner=%s task=%s submit=%s original_decision=%s original_command=%s original_operation=%s advance=%s work_revision=%d->%d compiler_calls=%d reservations=%d->%d", h.scope.TenantID, h.scope.OwnerID, current.TaskID, command.CommandID, first.DecisionID, first.CommandID, action.OperationID, firstWork.Job.JobID, firstWork.Job.WorkRevision, repeatedWork.Job.WorkRevision, compiler.calls.Load(), len(before.Reservations), len(after.Reservations))
			if err != nil || repeatedWork.Job.JobID != firstWork.Job.JobID || repeatedWork.Job.WorkRevision <= firstWork.Job.WorkRevision || compiler.calls.Load() != 1 || len(after.Reservations) != 3 || after.Budget[0].Reserved != "1" {
				t.Fatalf("original effect wake overlapped a pending current decision: calls=%d budget=%+v err=%v", compiler.calls.Load(), after, err)
			}
			facts, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, current.TaskID)
			if err != nil || len(facts.Operations) != 1 || !facts.Operations[0].Fact.Closed || facts.Operations[0].Fact.SourceRevision != 1 || facts.Task.GoalRef != goal {
				t.Fatalf("serial wait blocked the original effect facts: %+v %v", facts, err)
			}
		})
	}
}

func TestCurrentPendingDecisionAdmissionKeepsOriginalUsageAndResumesAfterConsumption(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, usageFinal := range []bool{false, true} {
			name := "usage_unknown"
			if usageFinal {
				name = "usage_known"
			}
			t.Run(driver+"/"+name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				content := &contentBridge{}
				compiler := &countedPendingContext{content: content}
				brain := &decisionDispatchBoundary{}
				h := pendingDecisionHarness(t, ctx, driver, task.Ports{Context: compiler, Content: content, Brain: brain})
				compiler.h = h
				configureContent(t, h, content)
				original := pendingSubmit(t, ctx, h)
				prepared := h.prepared(original, "1")
				intent, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared)
				if err != nil {
					t.Fatal(err)
				}
				if err = callOriginalDecision(t, h); err != nil {
					t.Fatal(err)
				}
				// 上游账务事实是受信明确夹具；不声称发生了供应商扣款。
				usage := api.UsageSnapshot{SourceRef: h.scope.Ref(prepared.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "0.4"}}, SpendingClosed: true, UsageFinal: usageFinal, ProofRefs: []api.ContentRef{h.content("preapproved original cumulative cost fixture")}}
				usage.UsageDigest, _ = task.UsageDigest(usage)
				if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
					t.Fatal(err)
				}
				current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "1")); !isTaskRejection(err, "invalid_state", "decision_pending") {
					t.Fatalf("known or unknown original usage admitted a second pending decision: %v", err)
				}
				replayed, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared)
				if err != nil || !api.Equal(replayed, intent) {
					t.Fatalf("serial gate changed the original replay: %+v %v", replayed, err)
				}
				oneOriginalAdvance(t, ctx, h)
				budget, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, original.TaskID)
				reserved := "0.6"
				if usageFinal {
					reserved = "0"
				}
				if err != nil || compiler.calls.Load() != 0 || len(budget.Reservations) != 1 || budget.Budget[0].Spent != "0.4" || budget.Budget[0].Reserved != reserved {
					t.Fatalf("pending wait changed original known/unknown accounting: calls=%d budget=%+v err=%v", compiler.calls.Load(), budget, err)
				}
				out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "invalid_fixture_proposal", ReasonRef: original.GoalRef}, nil)
				if err != nil || out.Outcome != "rejected" {
					t.Fatalf("original proposal could not be consumed: %+v %v", out, err)
				}
				oneOriginalAdvance(t, ctx, h)
				if compiler.calls.Load() != 1 || compiler.last.DecisionID == prepared.DecisionID {
					t.Fatal("original consumption did not admit one next current decision")
				}
				usage.UsageRevision = 2
				usage.Cumulative[0].Value = "0.6"
				usage.UsageFinal = true
				usage.UsageDigest, _ = task.UsageDigest(usage)
				for i := 0; i < 2; i++ {
					if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
						t.Fatal(err)
					}
				}
				budget, err = h.service.BudgetRead(ctx, h.store, h.scope, h.auth, original.TaskID)
				if err != nil || len(budget.Reservations) != 2 || budget.Budget[0].Spent != "0.6" || budget.Budget[0].Reserved != "1" || !budget.AccountingOpen {
					t.Fatalf("late original final fees changed new pending responsibility: %+v %v", budget, err)
				}
				t.Logf("tenant=%s owner=%s task=%s original_decision=%s original_command=%s next_decision=%s usage_final_initial=%v final_spent=0.6 remaining_reserved=1", h.scope.TenantID, h.scope.OwnerID, original.TaskID, intent.DecisionID, intent.CommandID, compiler.last.DecisionID, usageFinal)
			})
		}
	}
}

func TestPendingOldDecisionCannotBlockGoalControlOrTerminalChange(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, change := range []string{"control", "goal", "cancel"} {
			t.Run(driver+"/"+change, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				h := pendingDecisionHarness(t, ctx, driver, task.Ports{})
				original := pendingSubmit(t, ctx, h)
				prepared := h.prepared(original, "1")
				if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
					t.Fatal(err)
				}
				methods := []string{"task.pause", "task.resume"}
				if change == "goal" {
					methods = []string{"task.revise"}
				} else if change == "cancel" {
					methods = []string{"task.cancel"}
				}
				for _, method := range methods {
					current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
					if err != nil {
						t.Fatal(err)
					}
					command := h.command(method, current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "change while original decision is pending"})
					if change == "goal" {
						command.Payload = api.Raw(task.ReviseInput{TaskID: current.TaskID, BaseGoalRevision: current.GoalRevision, GoalRef: h.content("exact new goal from original user command"), SourceRef: h.scope.Ref(command.CommandID, 1)})
					}
					receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
					if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
						t.Fatalf("original %s did not apply: %+v %v", method, receipt, err)
					}
				}
				current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "1"))
				if change == "cancel" {
					if !isTaskRejection(err, "invalid_state", "target_terminal") {
						t.Fatalf("pending admission revived the cancelled original task: %v", err)
					}
				} else if err != nil {
					t.Fatalf("old decision blocked the new %s revision: %v", change, err)
				}
				out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: original.GoalRef, Actions: []task.PreparedAction{}}, nil)
				if err != nil || out.Outcome != "stale" || len(out.AdmittedOperationIDs) != 0 {
					t.Fatalf("old proposal bypassed new %s revision: %+v %v", change, out, err)
				}
				usage := api.UsageSnapshot{SourceRef: h.scope.Ref(prepared.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "0.5"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{h.content("preapproved exact late original fee")}}
				usage.UsageDigest, _ = task.UsageDigest(usage)
				if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
					t.Fatalf("new %s blocked late original accounting: %v", change, err)
				}
				after, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
				if err != nil || after.GoalRef != current.GoalRef || after.GoalRevision != current.GoalRevision || after.ControlRevision != current.ControlRevision || after.Budget[0].Spent != "0.5" || change == "cancel" && (after.Status != "cancelled" || after.AccountingOpen || after.Budget[0].Reserved != "0") {
					t.Fatalf("late original facts changed the goal/control/terminal truth: %+v %v", after, err)
				}
				t.Logf("tenant=%s owner=%s task=%s original_decision=%s change=%s original_goal_revision=%d current_goal_revision=%d original_control_revision=%d current_control_revision=%d", h.scope.TenantID, h.scope.OwnerID, original.TaskID, prepared.DecisionID, change, original.GoalRevision, after.GoalRevision, original.ControlRevision, after.ControlRevision)
			})
		}
	}
}

func TestPendingDecisionWaitSkipsNewPositiveAdvancePreparation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			gate := &advancePreparationBoundary{}
			h := pendingDecisionHarness(t, ctx, driver, task.Ports{Gate: gate, Context: advancePreparedContext{gate}})
			gate.h = h
			original := pendingSubmit(t, ctx, h)
			if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(original, "1")); err != nil {
				t.Fatal(err)
			}
			gate.required = true
			oneOriginalAdvance(t, ctx, h)
			if gate.preparations != 0 || gate.prepared.DecisionID != "" {
				t.Fatalf("pending wait performed new parent/context preparation: preparations=%d decision=%s", gate.preparations, gate.prepared.DecisionID)
			}
		})
	}
}

// 原Context准备在Tx外，因此其间可以有独立受信准入先提交。
// 返回前再读当前Task编译准确Snapshot，验证最终短Tx也必须核未消费Decision。
type winnerDuringPendingContext struct {
	countedPendingContext
	winner task.PreparedDecision
	intent api.DecisionDispatchIntent
}

func (p *winnerDuringPendingContext) Prepare(ctx context.Context, scope runtime.Scope, auth runtime.Auth, current api.Task) (task.PreparedDecision, error) {
	if p.winner.DecisionID == "" {
		p.winner = p.h.prepared(current, "1")
		var err error
		p.intent, err = p.h.service.PrepareDecision(ctx, p.h.store, scope, p.h.trusted(), p.winner)
		if err != nil {
			return task.PreparedDecision{}, err
		}
	}
	actual, err := p.h.service.Read(ctx, p.h.store, scope, auth, current.TaskID)
	if err != nil {
		return task.PreparedDecision{}, err
	}
	return p.countedPendingContext.Prepare(ctx, scope, auth, actual)
}

func TestDecisionAdmittedDuringContextPreparationRequeuesOnlyOriginalAdvance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			content := &contentBridge{}
			compiler := &winnerDuringPendingContext{countedPendingContext: countedPendingContext{content: content}}
			h := pendingDecisionHarness(t, ctx, driver, task.Ports{Context: compiler, Content: content})
			compiler.h = h
			configureContent(t, h, content)
			original := pendingSubmit(t, ctx, h)
			work := oneOriginalAdvance(t, ctx, h)
			budget, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, original.TaskID)
			if err != nil || len(budget.Reservations) != 1 || budget.Reservations[0].SourceRef.ObjectID != compiler.winner.DecisionID || budget.Budget[0].Reserved != "1" || compiler.calls.Load() != 1 {
				t.Fatalf("late context return admitted another current decision: budget=%+v calls=%d err=%v", budget, compiler.calls.Load(), err)
			}
			limit := time.NewTimer(4 * time.Second)
			defer limit.Stop()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			var next runtime.Work
			for next.Job.JobID == "" {
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-limit.C:
					t.Fatal("confirmed pending-decision rollback kept its one-minute claim instead of requeuing the original job")
				case <-tick.C:
					works, status, err := h.store.Claim(ctx, h.scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
					if err != nil || status != runtime.Committed {
						t.Fatalf("original retry claim: %v %v", status, err)
					}
					if len(works) == 1 {
						next = works[0]
					}
				}
			}
			if next.Job.JobID != work.Job.JobID || next.Claim.LeaseEpoch <= work.Claim.LeaseEpoch {
				t.Fatalf("local rollback replaced the original advance responsibility: %+v", next)
			}
			handler, _ := h.dispatch.Registry.Job(task.JobAdvance)
			if err = handler(ctx, h.store, h.scope, next); err != nil {
				t.Fatal(err)
			}
			replayed, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), compiler.winner)
			if err != nil || !api.Equal(replayed, compiler.intent) || compiler.calls.Load() != 1 {
				t.Fatalf("original pending retry refreshed context or intent: calls=%d intent=%+v err=%v", compiler.calls.Load(), replayed, err)
			}
			t.Logf("tenant=%s owner=%s task=%s winner_decision=%s winner_command=%s rejected_preparation=%s advance=%s original_epoch=%d recovered_epoch=%d", h.scope.TenantID, h.scope.OwnerID, original.TaskID, compiler.winner.DecisionID, compiler.winner.CommandID, compiler.last.DecisionID, work.Job.JobID, work.Claim.LeaseEpoch, next.Claim.LeaseEpoch)
		})
	}
}
