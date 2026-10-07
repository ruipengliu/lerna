package rules

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
)

// Fixed 是宿主编译进的全部原 API、FILE、模拟目标、模型和查询规则清单，不提供注册或未知版本回退。
type Fixed struct{}

var _ ledger.EvidenceRules = Fixed{}

func (Fixed) CheckSupported(op *v1.Operation) error {
	rules, e := fixedRules(op)
	if e != nil {
		return e
	}
	return rules.CheckSupported(op)
}

func (Fixed) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	rules, e := fixedRules(op)
	if e != nil {
		return nil, e
	}
	if e = rules.CheckSupported(op); e != nil {
		return nil, e
	}
	return rules.Interpret(op, raw, body)
}

// fixedRules 共用固定受信清单；历史资格与解释不得选择不同规则。
func fixedRules(op *v1.Operation) (ledger.EvidenceRules, error) {
	switch op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() {
	case "api-reference-v1":
		return API{}, nil
	case "managed-file":
		return File{}, nil
	case "simulator-idempotent", "simulator-idempotent-expiring", "simulator-idempotent-evicting", "simulator-idempotent-queryable", "simulator-queryable", "simulator-opaque":
		return Simulator{}, nil
	case "model-reference-v1":
		return Model{}, nil
	default:
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
}
