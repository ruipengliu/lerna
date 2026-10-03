package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
)

func TestTrustedPreparedActionKeepsOriginalExecutorOwnedBinding(t *testing.T) {
	for _, mode := range []string{"local", "remote", "wrong_owner", "wrong_tenant"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			proof := &localProofFixture{}
			h := newHarness(t, task.Ports{ActionAuthorization: proof})
			proof.install(t, h.scope)
			original := h.submit(t)
			prepared := h.prepared(original, "0")
			if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
				t.Fatal(err)
			}
			action := preparedAction(h, "0", "trusted_original_binding")
			action.SafeRequirementCheck = true
			if mode != "local" {
				action.ExecutorID = api.NewID("executor")
				action.BindingRef.OwnerID = action.ExecutorID
			}
			if mode == "wrong_owner" {
				action.BindingRef.OwnerID = h.scope.OwnerID
			}
			if mode == "wrong_tenant" {
				action.BindingRef.TenantID = api.NewID("tenant")
			}
			out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: original.GoalRef, Actions: []task.PreparedAction{action}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "wrong_owner" || mode == "wrong_tenant" {
				if out.Outcome != "rejected" || len(out.AdmittedOperationIDs) != 0 {
					t.Fatalf("wrong executor binding admitted: %+v", out)
				}
				return
			}
			if out.Outcome != "adopted" || len(out.AdmittedOperationIDs) != 1 {
				t.Fatalf("trusted exact executor binding rejected: %+v", out)
			}
			intent, err := h.service.ReadOperationIntent(ctx, h.store, h.scope, h.trusted(), action.OperationID)
			if err != nil || !api.Equal(intent.BindingRef, action.BindingRef) || intent.ExecutorID != action.ExecutorID {
				t.Fatalf("binding owner was rewritten: %+v %v", intent, err)
			}
		})
	}
}
