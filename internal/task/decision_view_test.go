package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestDecisionViewsKeepOriginalSnapshotAndReservationBound(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, task.Ports{})
	current := h.submit(t)
	prepared := h.prepared(current, "10")
	prepared.Snapshot.InputTokens = 120
	prepared.Snapshot.ReservedOutputTokens = 80
	prepared.Snapshot.SafetyMarginTokens = 5
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	usage := api.UsageSnapshot{SourceRef: h.scope.Ref(prepared.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "7"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{h.content("literal cumulative accounting fixture")}}
	usage.UsageDigest, _ = task.UsageDigest(usage)
	if _, err := h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
		t.Fatal(err)
	}
	read := func(scope runtime.Scope, auth runtime.Auth, id string) (api.Snapshot, []api.Amount, error) {
		var snapshot api.Snapshot
		var bound []api.Amount
		_, err := h.store.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			snapshot, err = h.service.DecisionSnapshotTx(ctx, tx, auth, id)
			if err == nil {
				bound, err = h.service.DecisionCostBoundTx(ctx, tx, auth, id)
			}
			return err
		})
		return snapshot, bound, err
	}
	snapshot, bound, err := read(h.scope, h.trusted(), prepared.DecisionID)
	if err != nil || !api.Equal(snapshot, prepared.Snapshot) || !api.Equal(bound, prepared.CostBound) {
		t.Fatalf("original snapshot/reservation was repriced after usage: %+v %+v %v", snapshot, bound, err)
	}
	if _, _, err = read(h.scope, h.auth, prepared.DecisionID); !api.IsCode(err, "forbidden") {
		t.Fatalf("ordinary caller accessed trusted assembly port: %v", err)
	}
	otherScope := h.scope
	otherScope.TenantID = api.NewID("tenant")
	otherAuth := h.trusted()
	otherAuth.TenantID = otherScope.TenantID
	if _, _, err = read(otherScope, otherAuth, prepared.DecisionID); !api.IsCode(err, "not_found") {
		t.Fatalf("frozen decision leaked across tenant: %v", err)
	}
	snapshot.InputTokens = 1
	bound[0].Value = "0"
	again, originalBound, err := read(h.scope, h.trusted(), prepared.DecisionID)
	if err != nil || again.InputTokens != 120 || originalBound[0].Value != "10" {
		t.Fatalf("caller mutated original admission: %+v %+v %v", again, originalBound, err)
	}
}
