package ledger

import (
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
	if e := durable.RequireDependencies("ledger", durable.Dependency{Name: "rules", Value: s.rules}); e != nil {
		return nil, e
	}
	return s.rules.Interpret(op, raw, body)
}
