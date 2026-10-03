package task_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type decisionProofKey struct{}

// 此边界只提供本次原父范围证明，不模拟物理模型或权限签名。
// 本测试使用真实Task库和原Job；HTTPS/独立owner证明由协作装配验收。
type decisionPreparationBoundary struct {
	h            *harness
	required     bool
	preparations int
	original     api.DecisionDispatchIntent
	beforeReturn func(context.Context, api.Task) error
}

func (*decisionPreparationBoundary) Authorize(context.Context, runtime.Tx, runtime.Auth, string, []api.ContentRef, []api.ObjectRef) error {
	return nil
}
func (*decisionPreparationBoundary) Evidence(context.Context, runtime.Tx, api.Task, []api.ObjectRef, []api.ComponentRef) error {
	return nil
}
func (g *decisionPreparationBoundary) CheckTaskCurrentTx(ctx context.Context, _ runtime.Tx, actual api.Task, running bool) error {
	if g.required && running && ctx.Value(decisionProofKey{}) != actual.TaskID {
		return api.E("dependency_unavailable", "remote_parent_scope_required")
	}
	return nil
}
func (g *decisionPreparationBoundary) PrepareTaskDecision(ctx context.Context, scope runtime.Scope, auth runtime.Auth, actual api.Task, intent api.DecisionDispatchIntent) (context.Context, error) {
	g.preparations++
	if scope != g.h.scope || auth.TenantID != g.h.auth.TenantID || auth.SubjectID != g.h.auth.SubjectID || auth.CredentialGeneration != g.h.auth.CredentialGeneration || !slices.Equal(auth.Roles, g.h.auth.Roles) || !api.Equal(intent, g.original) || actual.TaskID != g.original.TaskRef.ObjectID {
		return ctx, api.E("forbidden", "original_decision_scope_changed")
	}
	if g.beforeReturn != nil {
		if err := g.beforeReturn(ctx, actual); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, decisionProofKey{}, actual.TaskID), nil
}

type decisionDispatchBoundary struct{ dispatched []api.DecisionDispatchIntent }

func (b *decisionDispatchBoundary) Dispatch(_ context.Context, _ runtime.Scope, intent api.DecisionDispatchIntent, _ task.Snapshot) error {
	b.dispatched = append(b.dispatched, intent)
	return nil
}
func (*decisionDispatchBoundary) ReadProposal(context.Context, runtime.Scope, api.DecisionDispatchIntent) (task.Proposal, error) {
	return task.Proposal{}, api.E("dependency_unavailable", "original_receiver_not_ready")
}
func (*decisionDispatchBoundary) Usage(context.Context, runtime.Scope, api.ObjectRef) (api.UsageSnapshot, error) {
	return api.UsageSnapshot{}, api.E("unsupported", "no_physical_model_in_this_boundary")
}

func callOriginalDecision(t *testing.T, h *harness) error {
	t.Helper()
	ctx := context.Background()
	works, status, err := h.store.Claim(ctx, h.scope, api.NewID("boot"), []string{task.JobDispatchDecision}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original decision claim: %+v %v %v", works, status, err)
	}
	handler, ok := h.dispatch.Registry.Job(task.JobDispatchDecision)
	if !ok {
		t.Fatal("original decision handler unavailable")
	}
	return handler(ctx, h.store, h.scope, works[0])
}

func TestDecisionJobPreparesOriginalCurrentScopeBeforeDispatch(t *testing.T) {
	gate := &decisionPreparationBoundary{}
	brain := &decisionDispatchBoundary{}
	h := newHarness(t, task.Ports{Gate: gate, Brain: brain})
	gate.h = h
	original := h.submit(t)
	intent, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(original, "0"))
	if err != nil {
		t.Fatal(err)
	}
	gate.original, gate.required = intent, true
	if err = callOriginalDecision(t, h); err != nil {
		t.Fatal(err)
	}
	if gate.preparations != 1 || len(brain.dispatched) != 1 || !api.Equal(brain.dispatched[0], intent) {
		t.Fatalf("fresh original scope did not dispatch the original intent: preparations=%d dispatched=%+v", gate.preparations, brain.dispatched)
	}
	t.Logf("original tenant=%s owner=%s task=%s decision=%s command=%s goal_revision=%d control_revision=%d", h.scope.TenantID, h.scope.OwnerID, original.TaskID, intent.DecisionID, intent.CommandID, original.GoalRevision, original.ControlRevision)
	budget, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil || len(budget.Reservations) != 1 {
		t.Fatalf("preparation changed original budget responsibilities: %+v %v", budget, err)
	}
}

func TestDecisionPreparationCancellationCannotDispatchOrReviveOriginalTask(t *testing.T) {
	gate := &decisionPreparationBoundary{}
	brain := &decisionDispatchBoundary{}
	h := newHarness(t, task.Ports{Gate: gate, Brain: brain})
	gate.h = h
	original := h.submit(t)
	intent, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(original, "0"))
	if err != nil {
		t.Fatal(err)
	}
	gate.original, gate.required = intent, true
	gate.beforeReturn = func(ctx context.Context, actual api.Task) error {
		// 独立公开命令在准备返回前真实提交，证明准备没有持原业务Tx。
		receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", actual.TaskID, &actual.Revision, task.ControlInput{TaskID: actual.TaskID, Reason: "cancel during current parent preparation"})))
		if err != nil {
			return err
		}
		if receipt.Stage != "applied" {
			t.Fatalf("actual cancellation: %+v", receipt)
		}
		return nil
	}
	if err = callOriginalDecision(t, h); err != nil {
		t.Fatal(err)
	}
	current, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil || current.Status != "cancelled" || current.GoalRef != original.GoalRef || current.GoalRevision != original.GoalRevision || gate.preparations != 1 || len(brain.dispatched) != 0 {
		t.Fatalf("late parent proof bypassed original cancellation: task=%+v preparations=%d dispatches=%d err=%v", current, gate.preparations, len(brain.dispatched), err)
	}
}

func TestDecisionJobSkipsNewPreparationForTerminalOrOldControl(t *testing.T) {
	for _, change := range []string{"terminal", "old_control"} {
		t.Run(change, func(t *testing.T) {
			gate := &decisionPreparationBoundary{}
			brain := &decisionDispatchBoundary{}
			h := newHarness(t, task.Ports{Gate: gate, Brain: brain})
			gate.h = h
			original := h.submit(t)
			intent, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(original, "0"))
			if err != nil {
				t.Fatal(err)
			}
			gate.original, gate.required = intent, true
			methods := []string{"task.cancel"}
			if change == "old_control" {
				methods = []string{"task.pause", "task.resume"}
			}
			for _, method := range methods {
				actual, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, original.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command(method, actual.TaskID, &actual.Revision, task.ControlInput{TaskID: actual.TaskID, Reason: "close original decision admission"})))
				if err != nil || receipt.Stage != "applied" {
					t.Fatalf("actual %s: %+v %v", method, receipt, err)
				}
			}
			if err = callOriginalDecision(t, h); err != nil {
				t.Fatal(err)
			}
			if gate.preparations != 0 || len(brain.dispatched) != 0 {
				t.Fatalf("closed original decision acquired new parent proof or dispatched: preparations=%d dispatches=%d", gate.preparations, len(brain.dispatched))
			}
		})
	}
}

func TestDecisionJobWithoutOptionalPreparationKeepsOriginalLegacyDispatch(t *testing.T) {
	brain := &decisionDispatchBoundary{}
	h := newHarness(t, task.Ports{Brain: brain})
	original := h.submit(t)
	intent, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), h.prepared(original, "0"))
	if err != nil {
		t.Fatal(err)
	}
	if err = callOriginalDecision(t, h); err != nil {
		t.Fatal(err)
	}
	if len(brain.dispatched) != 1 || !api.Equal(brain.dispatched[0], intent) {
		t.Fatalf("legacy profile changed the original dispatch: %+v", brain.dispatched)
	}
}
