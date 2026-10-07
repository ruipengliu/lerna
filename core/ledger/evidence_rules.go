package ledger

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// EvidenceRules 解释固定版本的原始证据；实现不得执行 I/O 或修改任何持久事实。
type EvidenceRules interface {
	CheckSupported(*v1.Operation) error
	Interpret(*v1.Operation, *v1.RawObservation, []byte) (*EvidenceFacts, error)
}

// EvidenceFacts 是受信规则核验后的候选事实；引用和最终历史投影由执行管理保存。
type EvidenceFacts struct {
	Interpretation *v1.EffectInterpretation
	Wait           *v1.ApiWait
	Query          *QueryEvidence
}

// QueryEvidence 描述查询所见的原责任；查询自身的终局在 Interpretation 中独立返回。
type QueryEvidence struct {
	Rule, Outcome, LateEffect, Reason string
	Conflict                          bool
	RetryAfterMs                      *int64
}

func (s *Service) WithEvidenceRules(rules EvidenceRules) *Service { s.rules = rules; return s }

func (s *Service) interpretEvidence(op *v1.Operation, raw *v1.RawObservation, body []byte) (*EvidenceFacts, error) {
	protocol := op.Execution.Attempt.GetCapabilities().GetProtocolVersion()
	if op.Execution.CallDescriptor.ApiDescriptor != nil || protocol == "lerna-managed-file-v1" {
		if e := durable.RequireDependencies("ledger", durable.Dependency{Name: "rules", Value: s.rules}); e != nil {
			return nil, e
		}
		return s.rules.Interpret(op, raw, body)
	}
	// 未迁移协议只保留原固定实现，不为其他协议或版本提供回退。
	var finding *v1.EffectInterpretation
	switch protocol {
	case "lerna-simulator-v1":
		finding = interpretSimulator(raw, body, op.Execution.Attempt)
	case "lerna-simulator-query-v1":
		finding = interpretQuery(raw, body, op)
	case "lerna-model-v1":
		_, _, terminal := command.ReferenceModelOutput(raw, body, op.Execution.Attempt)
		finding = &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-model-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
		if terminal {
			finding.Outcome, finding.LateEffect, finding.Reason = "APPLIED", "RULED_OUT", "TERMINAL_PROTOCOL_EVIDENCE"
		}
	default:
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
	facts := &EvidenceFacts{Interpretation: finding}
	if op.QuerySubject != nil {
		facts.Interpretation = interpretQuery(raw, body, op)
		facts.Query = queryEvidence(raw, body, op)
	}
	return facts, nil
}
