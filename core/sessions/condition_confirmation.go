package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ConsumeConditionConfirmationInTransaction 把真实批准一次性绑定到原核验轮次。
func (s *Service) ConsumeConditionConfirmationInTransaction(ctx context.Context, ref, requirements *v1.Ref, condition string, target *v1.Ref) (*v1.Ref, error) {
	if ref == nil || target == nil || target.SchemaId != "lerna.v1.Verification" {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	c, e := s.QueryCurrentConfirmation(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, ref.Name)
	if e != nil {
		return nil, e
	}
	if c == nil || !proto.Equal(c.Ref, ref) || c.State != "APPROVED" || c.MatterType != "CONDITION_EVALUATION" || c.RespondedBy == nil || c.SessionId == nil || c.GetConditionEvaluation() == nil || !proto.Equal(c.GetConditionEvaluation().RequirementsRef, requirements) || c.GetConditionEvaluation().ConditionId != condition {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	if e = s.checkConfirmation(ctx, c); e != nil {
		return nil, e
	}
	if e = s.consumeConfirmation(ctx, c, "CONDITION_EVALUATION", target); e != nil {
		return nil, e
	}
	return c.Ref, nil
}

// QueryTaskConfirmations 只提供固定快照需要的事项投影，不暴露凭据或正文。
func (s *Service) QueryTaskConfirmations(ctx context.Context, caller *v1.Caller, task *v1.GlobalName) ([]*v1.SnapshotConfirmation, error) {
	if e := command.CheckName(caller, task, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	records, e := s.confirmationStore.AllConfirmations(ctx)
	if e != nil {
		return nil, e
	}
	var result []*v1.SnapshotConfirmation
	for _, c := range records {
		summary := &v1.SnapshotConfirmation{Ref: c.Ref, MatterType: c.MatterType, State: c.State, ExpiresAtUnixMs: c.ExpiresAtUnixMs}
		switch c.MatterType {
		case "CONDITION_EVALUATION":
			m := c.GetConditionEvaluation()
			if m == nil || !proto.Equal(m.TaskId, task) {
				continue
			}
			summary.ConditionId = m.ConditionId
			summary.RequirementsRef = m.RequirementsRef
			summary.EvidenceRefs = m.EvidenceRefs
			summary.InputVersion = m.InputVersion
			summary.ControlGeneration = m.ControlGeneration
		case "OPERATION_ADMISSION":
			m := c.GetOperationAdmission()
			if m == nil || !proto.Equal(m.TaskId, task) {
				continue
			}
			summary.ProposalRef = m.ProposalRef
			summary.EvidenceRefs = append([]*v1.Ref{m.ParametersRef}, m.ContentRefs...)
			summary.InputVersion = m.InputVersion
			summary.ControlGeneration = m.ControlGeneration
		case "GRANT_ISSUANCE":
			m := c.GetGrantIssuance()
			if m == nil || !proto.Equal(m.Grant.GetSubject(), task) {
				continue
			}
			summary.ProposalRef = m.IssuanceRef
		default:
			continue
		}
		result = append(result, summary)
	}
	return result, nil
}
