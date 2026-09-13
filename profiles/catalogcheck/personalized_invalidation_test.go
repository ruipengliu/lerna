package catalogcheck

import (
	"context"
	"lerna/brain"
	"testing"
)

func TestCurrentMemoryControlsActualActionStart(t *testing.T) {
	for _, phase := range []string{"admitted", "invoked"} {
		for _, change := range []string{"related", "unrelated", "revoke"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				r, e := RunActionCase(context.Background(), "personalized-"+change+"-"+phase, nil)
				if e != nil {
					t.Fatal(e)
				}
				if r.Decisions != 1 || r.Operations != 1 || r.MemoryReadAllocated != 1 || r.OtherState != "submitted" {
					t.Fatalf("changed original identity/other target: %+v", r)
				}
				if change == "unrelated" {
					if r.State != "COMPLETED" || r.Ledger != 997 || r.StartedInvocations != 1 {
						t.Fatalf("unrelated correction blocked original action: %+v", r)
					}
					return
				}
				if r.State != "WAITING" || r.Ledger != 1000 || r.FinalState != "submitted" || r.StartedInvocations != 0 {
					t.Fatalf("invalidated action caused effects: %+v", r)
				}
				if phase == "admitted" && (r.ReadyOperations != 1 || r.DispatchedOperations != 0 || r.Invocations != 0) {
					t.Fatalf("unexpected dispatch: %+v", r)
				}
				if phase == "invoked" && (r.Invocations != 1 || r.DispatchedOperations != 1) {
					t.Fatalf("did not exercise durable invocation boundary: %+v", r)
				}
			})
		}
	}
}

func TestInvalidatedModelDecisionSettlesWithoutDisclosingOldContext(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-related-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "WAITING" || r.Decisions != 1 || r.MetadataDecisions != 1 || !r.SettlementReplayVerified || r.Operations != 0 || r.Ledger != 1000 || r.FinalState != "submitted" || r.OtherState != "submitted" || r.MemoryReadAllocated != 1 || r.UsedRequests != 1 || r.UsedTokens != 140 || r.UnknownRequests != 0 || r.ReservedTokens != 0 {
		t.Fatalf("lost settlement or stale effects: %+v", r)
	}
}

func TestRevokedContextPreservesUnknownModelReservation(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-revoke-model", &unknownUsageModel{})
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "WAITING" || r.Decisions != 1 || r.MetadataDecisions != 1 || !r.SettlementReplayVerified || r.Operations != 0 || r.Ledger != 1000 || r.MemoryReadAllocated != 1 || r.UsedRequests != 0 || r.UsedTokens != 0 || r.UnknownRequests != 1 || r.ReservedTokens != 9216 {
		t.Fatalf("unknown request was refunded or repeated: %+v", r)
	}
}

func TestUnrelatedModelContextChangeRetainsOriginalDecision(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-unrelated-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "COMPLETED" || r.Decisions != 1 || r.MetadataDecisions != 0 || r.Operations != 1 || r.Ledger != 997 || r.MemoryReadAllocated != 1 || r.UsedRequests != 1 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 {
		t.Fatalf("unrelated change affected original decision: %+v", r)
	}
}

func TestContextRevocationDuringEvidenceSaveStillSettlesKnownUsage(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-revoke-save", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "WAITING" || r.Decisions != 1 || r.MetadataDecisions != 1 || !r.SettlementReplayVerified || r.Operations != 0 || r.Ledger != 1000 || r.UsedRequests != 1 || r.UsedTokens != 140 || r.UnknownRequests != 0 || r.ReservedTokens != 0 {
		t.Fatalf("save race lost settlement: %+v", r)
	}
}

func TestSourceCorrectionDuringEvidenceSaveSettlesWithoutOldBody(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-related-save", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "WAITING" || r.MetadataDecisions != 1 || !r.SettlementReplayVerified || r.Operations != 0 || r.Ledger != 1000 || r.UsedRequests != 1 || r.UsedTokens != 140 || r.UnknownRequests != 0 {
		t.Fatalf("corrected source lost settlement: %+v", r)
	}
}

func TestCorrectedMemoryReassemblesIntoNewAuthorizedDecision(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-reassemble-once", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "COMPLETED" || r.Decisions != 2 || r.MetadataDecisions != 1 || r.Corrections != 1 || r.ContextSnapshots != 2 || r.Operations != 1 || r.Ledger != 995 || r.FinalState != "authorized" || r.OtherState != "submitted" || r.MemoryReadAllocated != 2 || r.UsedRequests != 2 || r.GovernedArtifacts != 4 || r.RevokedArtifacts != 4 {
		t.Fatalf("new revision did not govern actual effect: %+v", r)
	}
}

func TestUnstableMemoryStopsAfterTwoPersistedReassemblies(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-reassemble-unstable", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "WAITING" || r.Decisions != 3 || r.MetadataDecisions != 3 || r.Corrections != 2 || r.ContextSnapshots != 3 || r.Operations != 0 || r.Ledger != 1000 || r.MemoryReadAllocated != 3 || r.UsedRequests != 3 || !r.SettlementReplayVerified {
		t.Fatalf("reassembly was unbounded or replay reset its budget: %+v", r)
	}
}
func TestReassemblyCannotReplaceRevokedOrUnknownReads(t *testing.T) {
	for _, mode := range []string{"revoked", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			var m brain.Model
			if mode == "unknown" {
				m = &unknownUsageModel{}
			}
			r, err := RunActionCase(context.Background(), "personalized-reassemble-"+mode, m)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != "WAITING" || r.Decisions != 1 || r.MetadataDecisions != 1 || r.Corrections != 0 || r.Operations != 0 || r.MemoryReadAllocated != 1 || r.Ledger != 1000 || !r.SettlementReplayVerified {
				t.Fatalf("replaced disallowed context: %+v", r)
			}
			if mode == "unknown" && (r.UnknownRequests != 1 || r.UsedRequests != 0 || r.ReservedTokens != 9216) {
				t.Fatalf("refunded unknown request: %+v", r)
			}
		})
	}
}

func TestDeletedMemoryBlocksFirstActionStart(t *testing.T) {
	for _, phase := range []string{"admitted", "invoked"} {
		t.Run(phase, func(t *testing.T) {
			r, err := RunActionCase(context.Background(), "personalized-delete-"+phase, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != "WAITING" || r.Ledger != 1000 || r.FinalState != "submitted" || r.StartedInvocations != 0 || r.Decisions != 1 || r.Operations != 1 || r.MemoryReadAllocated != 1 {
				t.Fatalf("deleted source started an effect or replaced identity: %+v", r)
			}
			if phase == "admitted" && (r.ReadyOperations != 1 || r.DispatchedOperations != 0 || r.Invocations != 0) {
				t.Fatalf("unexpected dispatch: %+v", r)
			}
			if phase == "invoked" && (r.Invocations != 1 || r.DispatchedOperations != 1) {
				t.Fatalf("did not reach durable invocation: %+v", r)
			}
		})
	}
}

func TestActionHostAutomaticallyCleansDeletedContext(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-delete-admitted", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.RetiredContexts != 1 || r.State != "WAITING" || r.Ledger != 1000 || r.StartedInvocations != 0 {
		t.Fatalf("automatic cleanup or action boundary failed: %+v", r)
	}
}

func TestActionHostAutomaticallyCleansDeletedArtifacts(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-delete-admitted", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Deletion == nil || r.Deletion.Authority != "committed" || len(r.Deletion.Derived) != 5 || r.Deletion.Derived[0].Name != "artifacts" || r.Deletion.Derived[0].State != "applied" || r.Deletion.Derived[1].Name != "contexts" || r.Deletion.Derived[1].State != "applied" || r.Deletion.Derived[2].Name != "archives" || r.Deletion.Derived[2].State != "not_covered" || r.Deletion.Derived[3].Name != "legacy-checkpoints" || r.Deletion.Derived[3].State != "applied" || r.Deletion.Derived[4].Name != "controlled-backups" || r.Deletion.Derived[4].State != "not_covered" {
		t.Fatalf("derived cleanup not confirmed: %+v", r.Deletion)
	}
	if r.Deletion.Replicas[0].State != "not_covered" || r.State != "WAITING" || r.Ledger != 1000 || r.StartedInvocations != 0 || r.Operations != 1 {
		t.Fatalf("scope or original action changed: %+v", r)
	}
}

func TestActionHostAutomaticallyErasesDeletedMemoryComparison(t *testing.T) {
	r, err := RunActionCase(context.Background(), "personalized-delete-admitted", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.MemoryComparisonErased || r.Deletion == nil || len(r.Deletion.Local) != 1 || r.Deletion.Local[0].Name != "admission-comparisons" || r.Deletion.Local[0].State != "applied" {
		t.Fatalf("original comparison cleanup not confirmed: %+v %+v", r.Deletion, r.MemoryComparisonErased)
	}
	if r.State != "WAITING" || r.Ledger != 1000 || r.StartedInvocations != 0 || r.Operations != 1 || r.ReadyOperations != 1 {
		t.Fatalf("cleanup changed original action: %+v", r)
	}
}
