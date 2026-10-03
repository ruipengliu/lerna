package execution

import (
	"context"
	"sort"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// UsageProof 固定原账本版本和目标证据；send_started 不冒充物理出口计数。
type UsageProof struct {
	OperationRef     api.ObjectRef `json:"operation_ref"`
	IntentHash       string        `json:"intent_hash"`
	UsageRevision    uint64        `json:"usage_revision"`
	Cumulative       []api.Amount  `json:"cumulative"`
	SpendingClosed   bool          `json:"spending_closed"`
	UsageFinal       bool          `json:"usage_final"`
	SendStartedCount uint64        `json:"send_started_count"`
	PhysicalCountMin uint64        `json:"physical_count_min"`
	PhysicalCountMax uint64        `json:"physical_count_max"`
	Attempts         []AttemptView `json:"attempts"`
}

func (s *Service) usageGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p OperationIDInput) (api.UsageSnapshot, error) {
	var r operationRecord
	if p.OperationID != q.TargetID {
		return api.UsageSnapshot{}, api.E("invalid_request", "operation_target_mismatch")
	}
	proof := UsageProof{Attempts: []AttemptView{}}
	status, err := st.Within(ctx, sc, []string{Namespace}, func(tx rt.Tx) error {
		if _, err := tx.Get(ctx, Namespace+".operations", p.OperationID, &r); err != nil {
			return err
		}
		if err := disclose(a, r); err != nil {
			return err
		}
		for _, id := range r.AttemptIDs {
			var attempt Attempt
			if _, err := tx.Get(ctx, Namespace+".attempts", id, &attempt); err != nil {
				return err
			}
			proof.Attempts = append(proof.Attempts, publicAttempt(attempt))
			if attempt.StartedAt != "" {
				proof.SendStartedCount++
				if attempt.Effect != "not_started" {
					proof.PhysicalCountMax++
					if attempt.Effect == "applied" || attempt.ResultRef != nil {
						proof.PhysicalCountMin++
					}
				}
			}
		}
		return nil
	})
	if status == rt.CommitUnknown {
		return api.UsageSnapshot{}, rt.ErrCommitUnknown
	}
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	if s.cfg.Content == nil || r.Tombstone {
		return api.UsageSnapshot{}, api.E("accounting_unknown", "original_usage_basis_unavailable")
	}
	intent := r.Intent
	if intent == nil {
		raw, err := s.cfg.Content.ReadBytes(ctx, sc, r.Principal, r.Invoke.IntentRef, "execution_intent", s.cfg.Location)
		if err != nil {
			return api.UsageSnapshot{}, err
		}
		canonical, err := api.Canonical(raw)
		if err != nil {
			return api.UsageSnapshot{}, err
		}
		if api.Hash(canonical) != r.Invoke.IntentHash {
			return api.UsageSnapshot{}, api.E("idempotency_conflict", "intent_mismatch")
		}
		var fixed ExecutionIntent
		if err = api.Decode(raw, &fixed); err != nil {
			return api.UsageSnapshot{}, err
		}
		if err = validateIntent(r.Invoke, fixed); err != nil {
			return api.UsageSnapshot{}, err
		}
		intent = &fixed
	}
	units := map[string]string{}
	for _, bound := range intent.CostBound {
		if err = api.ValidateRecord("Amount", bound); err != nil {
			return api.UsageSnapshot{}, err
		}
		if _, exists := units[bound.Unit]; exists {
			return api.UsageSnapshot{}, api.E("invalid_request", "duplicate_cost_unit")
		}
		units[bound.Unit] = "0"
	}
	if len(units) == 0 {
		return api.UsageSnapshot{}, api.E("accounting_unknown", "frozen_cost_units_missing")
	}
	for _, amount := range r.Operation.Usage {
		if _, exists := units[amount.Unit]; !exists {
			return api.UsageSnapshot{}, api.E("accounting_unknown", "usage_unit_not_reserved")
		}
		units[amount.Unit] = amount.Value
	}
	keys := make([]string, 0, len(units))
	for unit := range units {
		keys = append(keys, unit)
	}
	sort.Strings(keys)
	cumulative := make([]api.Amount, 0, len(keys))
	for _, unit := range keys {
		cumulative = append(cumulative, api.Amount{Unit: unit, Value: units[unit]})
	}
	snapshot := api.UsageSnapshot{SourceRef: sc.Ref(p.OperationID, r.Revision), UsageRevision: r.Revision, Cumulative: cumulative, SpendingClosed: r.NewAttemptsClosed, UsageFinal: r.Operation.UsageFinal, ProofRefs: []api.ContentRef{}}
	proof.OperationRef, proof.IntentHash, proof.UsageRevision = snapshot.SourceRef, r.Invoke.IntentHash, r.Revision
	proof.Cumulative, proof.SpendingClosed, proof.UsageFinal = cumulative, snapshot.SpendingClosed, snapshot.UsageFinal
	ref, err := s.cfg.Content.Publish(ctx, sc, r.Principal, Publication{ContentID: stableID("content", p.OperationID+":usage:"+strconv.FormatUint(r.Revision, 10)), MediaType: "application/json", Purpose: "execution_usage_proof", Location: s.cfg.Location, ProcessedSources: append([]api.ContentRef{r.Invoke.IntentRef}, r.Operation.EvidenceRefs...), DisclosedSources: []api.ContentRef{}}, api.Raw(proof))
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	snapshot.ProofRefs = []api.ContentRef{ref}
	snapshot.UsageDigest, err = api.Digest(snapshot)
	return snapshot, err
}
