package rules

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
)

// Fixed 是宿主编译进的原 API 与 FILE 规则清单，不提供注册或未知版本回退。
type Fixed struct{}

var _ ledger.EvidenceRules = Fixed{}

func (Fixed) CheckSupported(op *v1.Operation) error {
	switch op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() {
	case "api-reference-v1":
		return (API{}).CheckSupported(op)
	case "managed-file":
		return (File{}).CheckSupported(op)
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
}

func (Fixed) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	switch op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() {
	case "api-reference-v1":
		return (API{}).Interpret(op, raw, body)
	case "managed-file":
		return (File{}).Interpret(op, raw, body)
	default:
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
}
