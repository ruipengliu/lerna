package ledger

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// projectPhysicalEvidence 从所有发送的不可变解释重建效果；消息到达顺序不能覆盖历史事实。
func (s *Service) projectPhysicalEvidence(ctx context.Context, op *v1.Operation, latest *v1.EffectInterpretation) error {
	oldOutcome, oldLate := op.Effect.Outcome, op.Effect.LateEffect
	applied := oldOutcome == "APPLIED"
	terminalProof := op.Dispatch == "SEALED" && oldLate == "RULED_OUT"
	allNotApplied, allTerminal := true, true
	sameAttemptTerminal := false
	for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
		if send.Phase == "REGISTERED" || send.Phase == "CLOSED" {
			continue
		}
		if send.ObservationRef == nil {
			allNotApplied = false
			allTerminal = false
			continue
		}
		fact, e := s.store.(interpretationStore).LoadInterpretation(ctx, interpretationRef(send.ObservationRef))
		if e != nil {
			return e
		}
		if fact == nil {
			allNotApplied = false
			allTerminal = false
			continue
		}
		if fact.Reason == "EVIDENCE_CONFLICT" {
			op.Effect.EvidenceConflict = true
		}
		if fact.Outcome == "APPLIED" {
			applied = true
		}
		if fact.Outcome != "NOT_APPLIED" {
			allNotApplied = false
		}
		if fact.LateEffect != "RULED_OUT" {
			allTerminal = false
		}
		cap := op.Execution.Attempt.Capabilities
		if fact.Outcome == "APPLIED" && fact.LateEffect == "RULED_OUT" && cap.GetIdempotent() && cap.GetIdempotencyMechanism() == "NATIVE_KEY" && cap.GetConcurrencyGuarantee() == "SAME_KEY_ALL_SENDS" && cap.GetParameterBinding() == "EXACT_REQUEST" {
			// 原键的目标原生去重覆盖已登记的每个发送，不把单次请求失败推广为整次尝试未生效。
			sameAttemptTerminal = true
		}
	}
	if (oldOutcome == "NOT_APPLIED" && terminalProof && applied) || (len(op.Execution.PreviousSends) == 0 && oldOutcome == "APPLIED" && latest.Outcome == "NOT_APPLIED") {
		op.Effect.EvidenceConflict = true
	}
	op.Effect.Outcome = "UNKNOWN"
	op.Effect.LateEffect = "MAY_OCCUR"
	if op.Effect.EvidenceConflict {
		return nil
	}
	if applied {
		op.Effect.Outcome = "APPLIED"
	} else if allNotApplied {
		op.Effect.Outcome = "NOT_APPLIED"
	}
	if allTerminal || sameAttemptTerminal || terminalProof {
		op.Effect.LateEffect = "RULED_OUT"
	}
	return nil
}
