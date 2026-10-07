package rules

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
)

// Model 解释原参考模型的供应商回报，不采纳生成内容中的授权或任务效果。
type Model struct{}

var _ ledger.EvidenceRules = Model{}

// CheckSupported 只核验原模型规则资格，不读正文、重编或访问凭据。
func (Model) CheckSupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap.GetAdapterRef().GetName().GetLocalId() != "model-reference-v1" || cap.GetAdapterRef().GetRevision() != 1 || cap.GetAction() != "MODEL_INFER" {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution != nil {
		declaration := op.Execution.GetAttempt().GetCapabilities()
		if declaration.GetProtocolVersion() != "lerna-model-v1" || declaration.GetVerificationBasis() != "reference-model-v1" || declaration.GetDeclarationVersion() != "1" || declaration.GetEffect() != "MODEL_INFERENCE" {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	return nil
}

func (Model) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	_, _, terminal := command.ReferenceModelOutput(raw, body, op.Execution.Attempt)
	finding := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-model-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
	if terminal {
		finding.Outcome, finding.LateEffect, finding.Reason = "APPLIED", "RULED_OUT", "TERMINAL_PROTOCOL_EVIDENCE"
	}
	return &ledger.EvidenceFacts{Interpretation: finding}, nil
}
