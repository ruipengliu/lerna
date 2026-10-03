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

type advanceProofKey struct{}
type advancePreparationBoundary struct {
	h            *harness
	required     bool
	preparations int
	beforeReturn func(context.Context, api.Task) error
	prepared     task.PreparedDecision
}

func (*advancePreparationBoundary) Authorize(context.Context, runtime.Tx, runtime.Auth, string, []api.ContentRef, []api.ObjectRef) error {
	return nil
}
func (*advancePreparationBoundary) Evidence(context.Context, runtime.Tx, api.Task, []api.ObjectRef, []api.ComponentRef) error {
	return nil
}
func (g *advancePreparationBoundary) CheckTaskCurrentTx(ctx context.Context, _ runtime.Tx, actual api.Task, running bool) error {
	if g.required && running && ctx.Value(advanceProofKey{}) != actual.TaskID {
		return api.E("dependency_unavailable", "current_parent_proof_required")
	}
	return nil
}
func (g *advancePreparationBoundary) PrepareTaskAdvance(ctx context.Context, scope runtime.Scope, auth runtime.Auth, actual api.Task) (context.Context, error) {
	g.preparations++
	if scope != g.h.scope || auth.TenantID != g.h.auth.TenantID || auth.SubjectID != g.h.auth.SubjectID || auth.CredentialGeneration != g.h.auth.CredentialGeneration || !slices.Equal(auth.Roles, g.h.auth.Roles) {
		return ctx, api.E("forbidden", "original_submitter_changed")
	}
	if g.beforeReturn != nil {
		if err := g.beforeReturn(ctx, actual); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, advanceProofKey{}, actual.TaskID), nil
}

type advancePreparedContext struct{ gate *advancePreparationBoundary }

func (c advancePreparedContext) Prepare(_ context.Context, _ runtime.Scope, _ runtime.Auth, actual api.Task) (task.PreparedDecision, error) {
	c.gate.prepared = c.gate.h.prepared(actual, "0")
	return c.gate.prepared, nil
}

func callOriginalAdvance(t *testing.T, h *harness) error {
	t.Helper()
	ctx := context.Background()
	works, status, err := h.store.Claim(ctx, h.scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original claim %+v %v %v", works, status, err)
	}
	handler, ok := h.dispatch.Registry.Job(task.JobAdvance)
	if !ok {
		t.Fatal("original advance handler unavailable")
	}
	return handler(ctx, h.store, h.scope, works[0])
}

func TestAdvancePreparesCurrentProofBeforeOriginalDecisionAdmission(t *testing.T) {
	gate := &advancePreparationBoundary{}
	h := newHarness(t, task.Ports{Gate: gate, Context: advancePreparedContext{gate}})
	gate.h = h
	original := h.submit(t)
	gate.required = true
	if err := callOriginalAdvance(t, h); err != nil {
		t.Fatal(err)
	}
	var snap api.Snapshot
	status, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
		var err error
		snap, err = h.service.DecisionSnapshotTx(context.Background(), tx, h.trusted(), gate.prepared.DecisionID)
		return err
	})
	if err != nil || status != runtime.Committed || gate.preparations != 1 || snap.TaskRef.ObjectID != original.TaskID {
		t.Fatalf("fresh proof did not admit original decision: %d %+v %v", gate.preparations, snap.TaskRef, err)
	}
}

func TestAdvancePreparationCancellationCannotAdmitOriginalDecisionOrReadTerminalBody(t *testing.T) {
	gate := &advancePreparationBoundary{}
	h := newHarness(t, task.Ports{Gate: gate, Context: advancePreparedContext{gate}})
	gate.h = h
	original := h.submit(t)
	gate.required = true
	gate.beforeReturn = func(ctx context.Context, actual api.Task) error {
		// 在真实独立命令Tx中取消；若准备口还持原Tx/锁，这个边界不能完成。
		receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", actual.TaskID, &actual.Revision, task.ControlInput{TaskID: actual.TaskID, Reason: "cancel during fresh parent preparation"})))
		if err != nil {
			return err
		}
		if receipt.Stage != "applied" {
			t.Fatalf("original cancellation %+v", receipt)
		}
		return nil
	}
	if err := callOriginalAdvance(t, h); err != nil {
		t.Fatal(err)
	}
	facts, err := h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil || facts.Task.Status != "cancelled" || len(facts.FactRefs) != 0 || gate.preparations != 1 {
		t.Fatalf("late proof revived cancelled task: %d %+v %v", gate.preparations, facts, err)
	}
	status, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
		_, err := tx.Raise(context.Background(), task.JobAdvance, api.NewID("terminal"), h.scope.Ref(original.TaskID, facts.Task.Revision), time.Now())
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("terminal original continuation %v %v", status, err)
	}
	if err = callOriginalAdvance(t, h); err != nil {
		t.Fatal(err)
	}
	if gate.preparations != 1 {
		t.Fatal("terminal accounting/control continuation performed fresh positive preparation")
	}
}
