package command

import (
	"encoding/json"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// OperationMatter 固定用户确认与准入共用的语义投影，不包含消费后才生成的标识。
func OperationMatter(a *v1.Admission) *v1.OperationConfirmationMatter {
	var grant *v1.Ref
	if len(a.GetGrantRefs()) == 1 {
		grant = a.GrantRefs[0]
	}
	return &v1.OperationConfirmationMatter{TaskId: a.TaskId, ProposalRef: a.Origin, GrantRef: grant, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, StepId: a.StepId, Capability: a.CapabilitySnapshot, ParametersRef: a.ParametersRef, ContentRefs: a.ContentRefs, QuerySubject: a.QuerySubject}
}

// ConfirmationDigest 同时绑定事项类型、会话、内容版本和有效期。
func ConfirmationDigest(c *v1.Confirmation) string {
	if c.MatterType == "CONDITION_EVALUATION" {
		return SemanticFingerprint("confirmation", c.MatterType, c.SessionId, c.ExpiresAtUnixMs, c.GetConditionEvaluation())
	}
	return SemanticFingerprint("confirmation", c.MatterType, c.SessionId, c.ExpiresAtUnixMs, c.GetOperationAdmission(), c.GetGrantIssuance())
}

// RenderConfirmationParameters 只构造临时展示，不改变事项摘要或持久确认。
func RenderConfirmationParameters(c *v1.Confirmation, parameters []ParameterDescription) (string, error) {
	var description map[string]json.RawMessage
	if c == nil || json.Unmarshal([]byte(c.Description), &description) != nil || description == nil {
		return "", Fail("CONFIRMATION_INVALID")
	}
	switch c.MatterType {
	case "OPERATION_ADMISSION":
		if len(parameters) != 1 {
			return "", Fail("CONFIRMATION_INVALID")
		}
		description["Parameters"], _ = json.Marshal(parameters[0])
	case "GRANT_ISSUANCE":
		description["Parameters"], _ = json.Marshal(parameters)
	case "CONDITION_EVALUATION":
		description["Materials"], _ = json.Marshal(parameters)
	default:
		return "", Fail("CONFIRMATION_INVALID")
	}
	encoded, e := json.Marshal(description)
	return string(encoded), e
}
