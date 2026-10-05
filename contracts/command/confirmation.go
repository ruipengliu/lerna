package command

import v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"

// OperationMatter 固定用户确认与准入共用的语义投影，不包含消费后才生成的标识。
func OperationMatter(a *v1.Admission) *v1.OperationConfirmationMatter {
	var grant *v1.Ref
	if len(a.GetGrantRefs()) == 1 {
		grant = a.GrantRefs[0]
	}
	return &v1.OperationConfirmationMatter{TaskId: a.TaskId, ProposalRef: a.Origin, GrantRef: grant, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, StepId: a.StepId, Capability: a.CapabilitySnapshot, ParametersRef: a.ParametersRef, ContentRefs: a.ContentRefs}
}

// ConfirmationDigest 同时绑定事项类型、会话、内容版本和有效期。
func ConfirmationDigest(c *v1.Confirmation) string {
	return SemanticFingerprint("confirmation", c.MatterType, c.SessionId, c.ExpiresAtUnixMs, c.GetOperationAdmission(), c.GetGrantIssuance())
}
