package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 边界只记录领域对原来源的调用，不提供模型正文、收费账单或物理出口。
// 实际 Sent 标记、原取消命令、Claim、Reservation 和金额均在真实库执行。
type unsentAccountingBrain struct {
	dispatches, cancellations, usageCalls int
}

func (b *unsentAccountingBrain) Dispatch(context.Context, runtime.Scope, api.DecisionDispatchIntent, task.Snapshot) error {
	b.dispatches++
	return api.E("effect_unknown", "original_dispatch_reply_unknown")
}
func (*unsentAccountingBrain) ReadProposal(context.Context, runtime.Scope, api.DecisionDispatchIntent) (task.Proposal, error) {
	return task.Proposal{}, api.E("dependency_unavailable", "original_proposal_unknown")
}
func (b *unsentAccountingBrain) Usage(context.Context, runtime.Scope, api.ObjectRef) (api.UsageSnapshot, error) {
	b.usageCalls++
	return api.UsageSnapshot{}, api.E("accounting_unknown", "original_usage_unknown")
}
func (b *unsentAccountingBrain) CancelDecision(context.Context, runtime.Scope, api.DecisionDispatchIntent) error {
	b.cancellations++
	return nil
}

func originalUnsentAccountingJob(t *testing.T, ctx context.Context, h *harness, kind string) runtime.Work {
	t.Helper()
	works, status, err := h.store.Claim(ctx, h.scope, api.NewID("boot"), []string{kind}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original %s responsibility claim: count=%d status=%s error=%v", kind, len(works), status, err)
	}
	handler, found := h.dispatch.Registry.Job(kind)
	if !found {
		t.Fatal("original registered handler missing", kind)
	}
	if err = handler(ctx, h.store, h.scope, works[0]); err != nil {
		t.Fatal("original registered responsibility", kind, err)
	}
	return works[0]
}

func TestClosedUnsentDecisionSettlesOriginalReservationWithoutModelLedger(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			for _, bound := range []string{"0", "3"} {
				t.Run("bound_"+bound, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					brain := &unsentAccountingBrain{}
					h := pendingDecisionHarness(t, ctx, driver, task.Ports{Brain: brain})
					original := pendingSubmit(t, ctx, h)
					prepared := h.prepared(original, bound)
					intent, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared)
					if err != nil {
						t.Fatal(err)
					}
					current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
					if err != nil {
						t.Fatal(err)
					}
					command := h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "cancel original definitely undispatched decision"})
					receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
					if err != nil || receipt.Stage != "applied" {
						t.Fatal("original public cancellation", receipt.Stage, err)
					}
					decisionJob := originalUnsentAccountingJob(t, ctx, h, task.JobDispatchDecision)
					if decisionJob.Job.SourceRef.ObjectID != intent.DecisionID {
						t.Fatal("cancellation processed another decision")
					}
					originalUnsentAccountingJob(t, ctx, h, task.JobBilling)
					budget, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, original.TaskID)
					if err != nil || budget.AccountingOpen || len(budget.Reservations) != 1 || budget.Reservations[0].State != "settled" {
						t.Fatalf("original definitely unsent cancellation left accounting open: %+v error=%v", budget, err)
					}
					reservation := budget.Reservations[0]
					if reservation.SourceRef != h.scope.Ref(intent.DecisionID, 1) || reservation.SourceKind != "brain_decision" || reservation.AppliedUsageRevision != 0 || reservation.UsageDigest != "" || len(reservation.Units) != 1 || reservation.Units[0].OriginalReserved != bound || reservation.Units[0].AppliedCumulative != "0" || reservation.Units[0].RemainingReserved != "0" || budget.Budget[0].Spent != "0" || budget.Budget[0].Reserved != "0" {
						t.Fatalf("unsent cancellation changed original source or fabricated usage: %+v", budget)
					}
					if brain.dispatches != 0 || brain.cancellations != 0 || brain.usageCalls != 0 {
						t.Fatalf("never-dispatched closure contacted a missing model ledger: dispatch=%d cancel=%d usage=%d", brain.dispatches, brain.cancellations, brain.usageCalls)
					}
				})
			}
		})
	}
}

func TestSentUnknownDecisionCancellationRetainsOriginalReservation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			brain := &unsentAccountingBrain{}
			h := pendingDecisionHarness(t, ctx, driver, task.Ports{Brain: brain})
			original := pendingSubmit(t, ctx, h)
			prepared := h.prepared(original, "3")
			intent, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared)
			if err != nil {
				t.Fatal(err)
			}
			originalUnsentAccountingJob(t, ctx, h, task.JobDispatchDecision)
			if brain.dispatches != 1 {
				t.Fatal("original unknown dispatch did not actually enter its port")
			}
			current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "cancel while original dispatch receipt is unknown"})))
			if err != nil || receipt.Stage != "applied" {
				t.Fatal("original public cancellation", receipt.Stage, err)
			}
			// 等原Waiting责任已固定的短due；不改变任何Task/Command/Use期限。
			time.Sleep(1100 * time.Millisecond)
			originalUnsentAccountingJob(t, ctx, h, task.JobDispatchDecision)
			originalUnsentAccountingJob(t, ctx, h, task.JobBilling)
			budget, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, original.TaskID)
			if err != nil || !budget.AccountingOpen || len(budget.Reservations) != 1 || budget.Reservations[0].State != "open" || budget.Reservations[0].SourceRef != h.scope.Ref(intent.DecisionID, 1) || budget.Budget[0].Reserved != "3" || budget.Budget[0].Spent != "0" || brain.dispatches != 1 || brain.cancellations != 1 || brain.usageCalls != 1 {
				t.Fatalf("sent/unknown cancellation released or repeated original work: budget=%+v dispatch=%d cancel=%d usage=%d error=%v", budget, brain.dispatches, brain.cancellations, brain.usageCalls, err)
			}
		})
	}
}
