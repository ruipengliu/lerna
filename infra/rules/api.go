// Package rules 提供独立装配的受信固定证据规则，不持有出口或持久化能力。
package rules

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
)

// API 只解释原固定的参考 API 版本；普通请求编译适配器不能替换它。
type API struct{}

var _ ledger.EvidenceRules = API{}

// CheckSupported 纯核验原规则资格；完整描述与历史绑定仍由原编译器独立核验。
func (API) CheckSupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap.GetAdapterRef().GetName().GetLocalId() != "api-reference-v1" || command.ValidateAPIDescriptor(cap.GetApiDescriptor(), op.GetRef().GetName().GetUserId(), cap.GetResource()) != nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	protocol := "lerna-reference-api-v1"
	if cap.Action == "QUERY" {
		protocol = "lerna-reference-api-query-v1"
	} else if cap.Action != "CREATE" {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution != nil {
		declaration := op.Execution.GetAttempt().GetCapabilities()
		if declaration.GetProtocolVersion() != protocol || declaration.GetVerificationBasis() != "reference-api-v1" || declaration.GetDeclarationVersion() != "1" {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	return nil
}

// Interpret 返回原观察的候选事实；执行管理仍决定全部发送历史的效果和收尾。
func (API) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	x := op.Execution
	facts := &ledger.EvidenceFacts{Interpretation: interpretAPI(raw, body, x.CallDescriptor, x.Attempt), Wait: apiWait(raw, x.CallDescriptor, x.Attempt)}
	if op.QuerySubject != nil {
		response, conflict := parseQueryResponse(raw, body, op)
		facts.Interpretation = &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-api-query-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
		if conflict {
			facts.Interpretation.Reason = "EVIDENCE_CONFLICT"
		}
		if response != nil && *response.ReadTerminal {
			facts.Interpretation.Outcome, facts.Interpretation.LateEffect, facts.Interpretation.Reason = "APPLIED", "RULED_OUT", "TERMINAL_READ_RECEIPT"
		}
		facts.Query = queryEvidence(response, conflict, "reference-api-query-subject-v1")
	}
	return facts, nil
}

func interpretAPI(raw *v1.RawObservation, body []byte, d *v1.CallDescriptor, attempt *v1.ExecutionAttempt) *v1.EffectInterpretation {
	r := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-api-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
	applied, terminal, valid, conflict := command.ReferenceAPIResponse(raw, body, d, attempt)
	if conflict {
		r.Reason = "EVIDENCE_CONFLICT"
	}
	if !valid {
		return r
	}
	if applied {
		r.Outcome = "APPLIED"
	}
	if terminal {
		r.LateEffect, r.Reason = "RULED_OUT", "TERMINAL_PROTOCOL_EVIDENCE"
		if !applied {
			r.Outcome = "NOT_APPLIED"
		}
	} else {
		r.Reason = "LATE_EFFECT_POSSIBLE"
	}
	return r
}
