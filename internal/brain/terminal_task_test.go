package brain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestTerminalTaskClosesOriginalPublishingDecisionAndKnownReservation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			f := originalTaskProvider(t, driver, true)
			dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
			if r, err := dispatch.Command(ctx, f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
				t.Fatalf("original decision admission %+v %v", r, err)
			}
			before := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "publishing")
			if before.Decision.PhysicalRequestCount != 1 || !before.Decision.UsageFinal || !api.Equal(before.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
				t.Fatalf("actual known original HTTP use missing %+v", before)
			}
			current, err := f.task.Read(ctx, f.store, f.scope, f.auth, f.taskID)
			if err != nil {
				t.Fatal(err)
			}
			cancel := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: f.taskID, Method: "task.cancel", ExpectedRevision: &current.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: f.taskID, Reason: "stop original task while known output awaits publication"})}
			if r, err := f.taskDispatcher.Command(ctx, f.auth, api.Raw(cancel)); err != nil || r.Stage != "applied" {
				t.Fatalf("public original Task cancel %+v %v", r, err)
			}
			f.content.paused = false
			// 原出版端口曾真实返回等待；只等原 Job 的一次有限 due，不刷新业务期限。
			time.Sleep(1050 * time.Millisecond)
			if err = runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
				t.Fatalf("terminal task stranded the original publishing responsibility: %v", err)
			}
			closed, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || closed.Decision.Status != "cancelled" || closed.Publication != "cancelled" || !closed.CancelRequested || closed.Decision.ProposalRef != nil || closed.CallID != before.CallID || closed.Decision.PhysicalRequestCount != 1 || !api.Equal(closed.Decision.Usage, before.Decision.Usage) || !closed.Decision.UsageFinal {
				t.Fatalf("negative closure lost original call/use or published an adoptable proposal %+v %v", closed, err)
			}
			receipt, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Reason != "decision_cancelled" {
				t.Fatalf("original accepted decision receipt remained stranded %+v %v", receipt, err)
			}
			usage, err := f.brain.Usage(ctx, f.store, f.scope, f.scope.Ref(f.command.TargetID, closed.Decision.Revision))
			if err != nil || !usage.SpendingClosed || !usage.UsageFinal || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
				t.Fatalf("known final fee was replaced or remained open %+v %v", usage, err)
			}
			reservation, err := f.task.ReconcileUsage(ctx, f.store, f.scope, f.auth, "brain_decision", usage)
			if err != nil || reservation.State != "settled" {
				t.Fatalf("original budget could not close %+v %v", reservation, err)
			}
			current, err = f.task.Read(ctx, f.store, f.scope, f.auth, f.taskID)
			if err != nil || current.Status != "cancelled" || current.AccountingOpen || current.Budget[0].Spent != "0.00024" || current.Budget[0].Reserved != "0" {
				t.Fatalf("Task cancellation rewrote original expense or left a known hold %+v %v", current, err)
			}
			jobs, status, err := f.store.Claim(ctx, f.scope, api.NewID("worker"), []string{brain.JobAdvance}, 10, time.Minute)
			if err != nil || status != runtime.Committed || len(jobs) != 0 || f.posts.Load() != 1 {
				t.Fatalf("closed work recurred or original HTTP was repeated jobs=%d calls=%d %s %v", len(jobs), f.posts.Load(), status, err)
			}
			replayed, err := dispatch.Command(ctx, f.auth, api.Raw(f.command))
			if err != nil || !api.Equal(replayed, receipt) || f.posts.Load() != 1 {
				t.Fatalf("original closed command replay changed %+v %v", replayed, err)
			}
			stop := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: f.command.TargetID, Method: "brain.cancel", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(brain.CancelInput{DecisionID: f.command.TargetID, TaskRef: before.TaskRef})}
			if r, err := dispatch.Command(ctx, f.auth, api.Raw(stop)); err != nil || r.Stage != "applied" {
				t.Fatalf("repeated negative control %+v %v", r, err)
			}
			if err = runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
				t.Fatalf("original rejected receipt could not survive another negative control: %v", err)
			}
			again, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || !api.Equal(again, receipt) || f.posts.Load() != 1 {
				t.Fatalf("negative control changed the original decision or sent again %+v %v", again, err)
			}
		})
	}
}

func cancelOriginalBrain(t *testing.T, f *taskProviderFixture) api.Receipt {
	t.Helper()
	var input brain.DecideInput
	if err := api.Decode(f.command.Payload, &input); err != nil {
		t.Fatal(err)
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: input.DecisionID, Method: "brain.cancel", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(brain.CancelInput{DecisionID: input.DecisionID, TaskRef: input.TaskRef})}
	dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
	receipt, err := dispatch.Command(context.Background(), f.auth, api.Raw(command))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original Brain negative control %+v %v", receipt, err)
	}
	return receipt
}

func TestUnsentBrainCancellationDecidesOriginalReceiptWithoutPhysicalRequest(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := originalTaskProvider(t, driver, false)
			ctx := context.Background()
			dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
			if r, err := dispatch.Command(ctx, f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
				t.Fatalf("original Brain acceptance %+v %v", r, err)
			}
			cancelOriginalBrain(t, f)
			if err := runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
				t.Fatal(err)
			}
			view, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || view.Decision.Status != "cancelled" || !view.Decision.UsageFinal || view.Decision.SendStarted || view.Decision.PhysicalRequestCount != 0 || f.posts.Load() != 0 {
				t.Fatalf("unsent negative control created work or left a hold %+v %v", view, err)
			}
			receipt, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Reason != "decision_cancelled" {
				t.Fatalf("unsent original accepted command never decided %+v %v", receipt, err)
			}
			usage, err := f.brain.Usage(ctx, f.store, f.scope, f.scope.Ref(f.command.TargetID, view.Decision.Revision))
			if err != nil || !usage.SpendingClosed || !usage.UsageFinal || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0"}}) {
				t.Fatalf("definitely unsent fee was not represented accurately %+v %v", usage, err)
			}
		})
	}
}

func TestPublishingGateDatabaseFailureKeepsOriginalAcceptedWorkRecoverable(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := originalTaskProvider(t, driver, true)
			ctx := context.Background()
			dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
			if r, err := dispatch.Command(ctx, f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
				t.Fatalf("original Brain acceptance %+v %v", r, err)
			}
			before := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "publishing")
			f.content.paused = false
			time.Sleep(1050 * time.Millisecond)
			works, status, err := f.store.Claim(ctx, f.scope, api.NewID("worker"), []string{brain.JobAdvance}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(works) != 1 {
				t.Fatalf("original publication claim %+v %s %v", works, status, err)
			}
			fault := errors.New("temporarily unavailable original Task database")
			f.gate.fault = fault
			handler, _ := f.registry.Job(brain.JobAdvance)
			if err = handler(ctx, f.store, f.scope, works[0]); !errors.Is(err, fault) {
				t.Fatalf("database failure was converted into permanent cancellation: %v", err)
			}
			still, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || !api.Equal(still, before) {
				t.Fatalf("rolled-back dependency failure changed original facts %+v %v", still, err)
			}
			r, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || r.Stage != "accepted" {
				t.Fatalf("database failure rejected the original accepted command %+v %v", r, err)
			}
			f.gate.fault = nil
			if err = handler(ctx, f.store, f.scope, works[0]); err != nil {
				t.Fatalf("same original claim could not recover: %v", err)
			}
			closed := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "completed")
			if f.posts.Load() != 1 || closed.CallID != before.CallID || !api.Equal(closed.Decision.Usage, before.Decision.Usage) || closed.CancelRequested {
				t.Fatalf("dependency recovery sent or charged twice %+v", closed)
			}
		})
	}
}

// 原 HTTP 回应已在供应商适配器持久保存。这里仅丢失返回或暂缓 final 标志，
// 不改变任何真实 token/金额；Lookup 仍查询同一个原 CallID。
type delayedOriginalReply struct {
	brain.Engine
	loseReply bool
	partial   bool
	unknown   bool
}

func (e *delayedOriginalReply) Request(ctx context.Context, id string, encoding brain.Encoding) (brain.Generated, error) {
	out, err := e.Engine.Request(ctx, id, encoding)
	if err != nil {
		return out, err
	}
	if e.loseReply {
		return brain.Generated{}, api.E("effect_unknown", "original_reply_lost")
	}
	if e.partial {
		out.UsageFinal = false
	}
	return out, nil
}
func (e *delayedOriginalReply) Lookup(ctx context.Context, id string, encoding brain.Encoding) (brain.Generated, error) {
	if e.unknown {
		return brain.Generated{}, api.E("effect_unknown", "original_reply_still_unavailable")
	}
	return e.Engine.Lookup(ctx, id, encoding)
}

func TestCancelledPhysicalDecisionRetainsUnknownAndReconcilesOriginalLateUse(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, mode := range []string{"unknown_reply", "late_final_use"} {
			t.Run(driver+"/"+mode, func(t *testing.T) {
				var boundary *delayedOriginalReply
				f := originalTaskProviderEngine(t, driver, true, func(engine brain.Engine) brain.Engine {
					boundary = &delayedOriginalReply{Engine: engine, loseReply: mode == "unknown_reply", partial: mode == "late_final_use", unknown: true}
					return boundary
				})
				ctx := context.Background()
				dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
				if r, err := dispatch.Command(ctx, f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
					t.Fatalf("original Brain acceptance %+v %v", r, err)
				}
				phase := "publishing"
				if mode == "unknown_reply" {
					phase = "provider_result_unknown"
				}
				before := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, phase)
				cancelOriginalBrain(t, f)
				time.Sleep(1050 * time.Millisecond)
				if err := runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
					t.Fatal(err)
				}
				pending, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
				if err != nil || !pending.CancelRequested || pending.Decision.UsageFinal || pending.Decision.ProposalRef != nil || pending.CallID != before.CallID || pending.Decision.PhysicalRequestCount != 1 || f.posts.Load() != 1 {
					t.Fatalf("negative control discarded unknown/final use or sent again %+v %v", pending, err)
				}
				current, err := f.task.Read(ctx, f.store, f.scope, f.auth, f.taskID)
				if err != nil || current.Budget[0].Reserved != "1" {
					t.Fatalf("negative control released unresolved original reservation %+v %v", current, err)
				}
				boundary.unknown = false
				f.content.paused = false
				time.Sleep(1050 * time.Millisecond)
				if err = runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
					t.Fatalf("known late reply could not close the same original responsibility: %v", err)
				}
				closed, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
				if err != nil || closed.Decision.Status != "cancelled" || closed.Decision.ProposalRef != nil || !closed.Decision.UsageFinal || closed.CallID != before.CallID || f.posts.Load() != 1 {
					t.Fatalf("original late-use recovery changed the call or published a proposal %+v %v", closed, err)
				}
				usage, err := f.brain.Usage(ctx, f.store, f.scope, f.scope.Ref(f.command.TargetID, closed.Decision.Revision))
				if err != nil || !usage.UsageFinal || !usage.SpendingClosed || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
					t.Fatalf("actual original late token cost did not survive cancellation %+v %v", usage, err)
				}
				if result, err := f.task.ReconcileUsage(ctx, f.store, f.scope, f.auth, "brain_decision", usage); err != nil || result.State != "settled" {
					t.Fatalf("late use could not settle exactly once %+v %v", result, err)
				}
				receipt, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
				if err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Reason != "decision_cancelled" {
					t.Fatalf("original decision receipt not closed %+v %v", receipt, err)
				}
			})
		}
	}
}

func TestPublishedOriginalDecisionReconcilesLateFinalUseWithoutChangingProposal(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := originalTaskProviderEngine(t, driver, false, func(engine brain.Engine) brain.Engine {
				return &delayedOriginalReply{Engine: engine, partial: true}
			})
			ctx := context.Background()
			dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
			if r, err := dispatch.Command(ctx, f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
				t.Fatalf("original admission %+v %v", r, err)
			}
			before := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "completed")
			if before.Decision.UsageFinal || before.Decision.ProposalRef == nil {
				t.Fatalf("test lost its original non-final known use %+v", before)
			}
			receipt, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || receipt.Stage != "applied" {
				t.Fatalf("original published receipt %+v %v", receipt, err)
			}
			time.Sleep(1050 * time.Millisecond)
			if err = runtime.Drain(ctx, f.store, f.scope, f.registry, 20); err != nil {
				t.Fatal(err)
			}
			closed, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || !closed.Decision.UsageFinal || closed.Decision.Status != "completed" || !api.Equal(closed.Decision.ProposalRef, before.Decision.ProposalRef) || closed.CallID != before.CallID || f.posts.Load() != 1 {
				t.Fatalf("original published decision stranded its late fee or changed its proposal %+v %v", closed, err)
			}
			fixed, err := dispatch.Lookup(ctx, f.auth, f.command.CommandID)
			if err != nil || !api.Equal(fixed, receipt) {
				t.Fatalf("late fee overwrote original published receipt %+v %v", fixed, err)
			}
		})
	}
}
