package catalogcheck

import (
	"context"
	"errors"
	"lerna/contextassembly"
	"testing"
)

func TestMemorySelectsActualAPIRecord(t *testing.T) {
	for _, tc := range []struct {
		mode, record string
		ledger       int64
	}{{"personalized-item", "item", 997}, {"personalized-alternative", "alternative", 995}, {"personalized-inapplicable", "item", 997}} {
		t.Run(tc.mode, func(t *testing.T) {
			r, e := RunActionCase(context.Background(), tc.mode, nil)
			if e != nil {
				t.Fatal(e)
			}
			if r.State != "COMPLETED" || r.SelectedRecord != tc.record || r.Ledger != tc.ledger || r.Operations != 1 || r.OtherState != "submitted" || r.MemoryReadAllocated != 1 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 {
				t.Fatalf("personalized action %+v", r)
			}
		})
	}
}

func TestPersonalizedActionReopensOriginalMemoryBinding(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-reopen-alternative", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.MemoryBindingRestored || r.State != "COMPLETED" || r.SelectedRecord != "alternative" || r.Ledger != 995 || r.OtherState != "submitted" || r.MemoryReadAllocated != 1 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 {
		t.Fatalf("reopened original binding lost: %+v", r)
	}
}

func TestReopenedActionMemoryRejectsRevokedOriginalRead(t *testing.T) {
	_, err := RunActionCase(context.Background(), "personalized-reopen-revoked", nil)
	if !errors.Is(err, contextassembly.Denied) {
		t.Fatalf("reopened revoked binding: %v", err)
	}
}

func TestPersonalizedTaskBindsEachDecisionSnapshot(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-multistep", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "COMPLETED" || r.Decisions != 2 || r.Operations != 2 || r.Ledger != 997 || r.ContextSnapshots != 2 || r.MemoryReadAllocated != 1 || r.GovernedArtifacts != 7 || r.RevokedArtifacts != 7 {
		t.Fatalf("multi-decision context: %+v", r)
	}
}
