package contextcheck

import (
	"context"
	"lerna/profiles/catalogcheck"
)

// registerReassembly keeps these checks in the same CLI report as the original
// publication/start checks. All faults use the actual Memory and grant services.
func registerReassembly(add func(string, string, func(context.Context) error)) {
	for _, mode := range []string{"related-model", "revoke-save", "related-save", "unrelated-model", "revoke-model"} {
		add("action-settlement-"+mode, "actual_source_change_usage_settlement_and_replay", func(ctx context.Context) error {
			var r catalogcheck.ActionReport
			var err error
			if mode == "revoke-model" {
				r, err = catalogcheck.RunUnknownUsageActionCase(ctx, "personalized-"+mode)
			} else {
				r, err = catalogcheck.RunActionCase(ctx, "personalized-"+mode, nil)
			}
			if err != nil {
				return err
			}
			if r.Decisions != 1 || r.MemoryReadAllocated != 1 || r.OtherState != "submitted" {
				return require(false)
			}
			if mode == "unrelated-model" {
				return require(r.State == "COMPLETED" && r.MetadataDecisions == 0 && r.Operations == 1 && r.Ledger == 997 && r.UsedRequests == 1 && r.GovernedArtifacts == 4 && r.RevokedArtifacts == 4)
			}
			if r.State != "WAITING" || r.MetadataDecisions != 1 || !r.SettlementReplayVerified || r.Operations != 0 || r.Ledger != 1000 || r.FinalState != "submitted" {
				return require(false)
			}
			if mode == "revoke-model" {
				return require(r.UsedRequests == 0 && r.UsedTokens == 0 && r.UnknownRequests == 1 && r.ReservedTokens == 9216)
			}
			return require(r.UsedRequests == 1 && r.UsedTokens == 140 && r.UnknownRequests == 0 && r.ReservedTokens == 0)
		})
	}
	for _, mode := range []string{"once", "unstable", "revoked", "unknown"} {
		add("action-reassembly-"+mode, "actual_fresh_revision_read_bounded_decisions_and_business_effect", func(ctx context.Context) error {
			var r catalogcheck.ActionReport
			var err error
			if mode == "unknown" {
				r, err = catalogcheck.RunUnknownUsageActionCase(ctx, "personalized-reassemble-"+mode)
			} else {
				r, err = catalogcheck.RunActionCase(ctx, "personalized-reassemble-"+mode, nil)
			}
			if err != nil {
				return err
			}
			if r.OtherState != "submitted" {
				return require(false)
			}
			switch mode {
			case "once":
				return require(r.State == "COMPLETED" && r.Decisions == 2 && r.MetadataDecisions == 1 && r.Corrections == 1 && r.ContextSnapshots == 2 && r.Operations == 1 && r.Ledger == 995 && r.FinalState == "authorized" && r.MemoryReadAllocated == 2 && r.UsedRequests == 2 && r.GovernedArtifacts == 4 && r.RevokedArtifacts == 4)
			case "unstable":
				return require(r.State == "WAITING" && r.Decisions == 3 && r.MetadataDecisions == 3 && r.Corrections == 2 && r.ContextSnapshots == 3 && r.Operations == 0 && r.Ledger == 1000 && r.MemoryReadAllocated == 3 && r.UsedRequests == 3 && r.SettlementReplayVerified)
			default:
				if r.State != "WAITING" || r.Decisions != 1 || r.MetadataDecisions != 1 || r.Corrections != 0 || r.Operations != 0 || r.MemoryReadAllocated != 1 || r.Ledger != 1000 || !r.SettlementReplayVerified {
					return require(false)
				}
				if mode == "unknown" {
					return require(r.UnknownRequests == 1 && r.UsedRequests == 0 && r.ReservedTokens == 9216)
				}
				return require(r.UsedRequests == 1 && r.UnknownRequests == 0 && r.ReservedTokens == 0)
			}
		})
	}
}
