package sessions

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveInputDelivery(ctx context.Context, v *v1.InputDelivery) error {
	if err := s.store.(DeliveryStore).SaveInputDelivery(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "sessions", &v1.TraceEvent{EventType: "INPUT_RECORDED", SourceRecordRef: v.Ref, TaskId: v.Input.TaskId, OriginCommand: v.Input.CommandIdentity, RelatedRefs: []*v1.Ref{v.Input.ContentRef, v.Input.RequestRef, v.Input.ConfirmationRef}})
}

func (s *Service) saveQuestion(ctx context.Context, q *v1.Question) error {
	if err := s.store.(QuestionStore).SaveQuestion(ctx, q); err != nil {
		return err
	}
	return s.store.SaveTraceSource(ctx, "sessions", &v1.TraceEvent{EventType: "QUESTION_" + q.Status, SourceRecordRef: q.Ref, TaskId: q.TaskId, OriginCommand: q.PublishedBy, RequirementsVersion: q.RequirementsVersion, BodyRef: q.ContentRef, RelatedRefs: []*v1.Ref{q.ContentRef, q.ResponseInputRef}})
}

// saveConfirmation 只读取已固定的类型化事项，不渲染正文或查询当前可消费性。
func (s *Service) saveConfirmation(ctx context.Context, c *v1.Confirmation) error {
	if err := s.confirmationStore.SaveConfirmation(ctx, c); err != nil {
		return err
	}
	event := &v1.TraceEvent{EventType: "CONFIRMATION_" + c.State, SourceRecordRef: c.Ref}
	switch c.State {
	case "APPROVED", "REJECTED":
		event.OriginCommand = c.RespondedBy
	case "WITHDRAWN":
		event.OriginCommand = c.WithdrawnBy
	}
	if matter := c.GetOperationAdmission(); matter != nil {
		event.TaskId = matter.TaskId
		event.RequirementsVersion = matter.RequirementsVersion
		event.BodyRef = matter.ParametersRef
		event.RelatedRefs = append([]*v1.Ref{matter.ProposalRef, matter.GrantRef, matter.GetCapability().GetRef(), matter.ParametersRef}, matter.ContentRefs...)
		if subject := matter.QuerySubject; subject != nil {
			event.OperationId = subject.OperationId
			event.AttemptId = subject.AttemptId
		}
	}
	if matter := c.GetGrantIssuance(); matter != nil {
		event.TaskId = matter.GetGrant().GetSubject()
		event.RelatedRefs = []*v1.Ref{matter.IssuanceRef, matter.GetGrant().GetRef()}
		for _, permission := range matter.GetGrant().GetPermissions() {
			if permission.ParameterMode == "EXACT" {
				event.RelatedRefs = append(event.RelatedRefs, permission.ParametersRef)
			}
		}
	}
	if matter := c.GetConditionEvaluation(); matter != nil {
		event.TaskId = matter.TaskId
		event.RelatedRefs = append([]*v1.Ref{matter.RequirementsRef}, matter.EvidenceRefs...)
	}
	event.RelatedRefs = append(event.RelatedRefs, c.GetConsumedAdmissionRef(), c.GetConsumedGrantIssuanceRef(), c.GetConsumedVerificationRef())
	return s.store.SaveTraceSource(ctx, "sessions", event)
}
