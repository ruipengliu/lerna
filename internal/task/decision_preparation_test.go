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
	budget, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil || len(budget.Reservations) != 1 {
		t.Fatalf("preparation changed original budget responsibilities: %+v %v", budget, err)
	}
}
