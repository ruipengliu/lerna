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
	switch op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() {
	case "api-reference-v1":
		return (API{}).CheckSupported(op)
	case "managed-file":
		return (File{}).CheckSupported(op)
	case "simulator-idempotent", "simulator-idempotent-expiring", "simulator-idempotent-evicting", "simulator-idempotent-queryable", "simulator-queryable", "simulator-opaque":
		return (Simulator{}).CheckSupported(op)
	case "model-reference-v1":
		return (Model{}).CheckSupported(op)
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
}

func (rules Fixed) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	if e := rules.CheckSupported(op); e != nil {
		return nil, e
	}
	switch op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() {
	case "api-reference-v1":
		return (API{}).Interpret(op, raw, body)
	case "managed-file":
		return (File{}).Interpret(op, raw, body)
	case "simulator-idempotent", "simulator-idempotent-expiring", "simulator-idempotent-evicting", "simulator-idempotent-queryable", "simulator-queryable", "simulator-opaque":
		return (Simulator{}).Interpret(op, raw, body)
	case "model-reference-v1":
		return (Model{}).Interpret(op, raw, body)
	default:
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
}
