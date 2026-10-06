package tasks

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) saveTaskClosing(ctx context.Context, closing *v1.TaskClosing) error {
	if e := s.store.(taskClosingStore).SaveTaskClosing(ctx, closing); e != nil {
		return e
	}
	refs := append([]*v1.Ref{closing.RequirementsRef, closing.CancellationRef}, closing.AdmissionRefs...)
	refs = append(refs, closing.ClosureIntentRefs...)
	refs = append(refs, closing.SettlementFollowupRefs...)
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "TASK_CLOSE_REQUESTED", SourceRecordRef: closing.Ref, TaskId: closing.TaskId, OriginCommand: closing.RequestedBy, RequirementsVersion: closing.RequirementsVersion, ReasonCode: closing.CloseReason, RelatedRefs: refs})
}

func (s *Service) saveTaskClosureIntent(ctx context.Context, intent *v1.TaskClosureIntent) error {
	if e := s.store.(taskClosureIntentStore).SaveTaskClosureIntent(ctx, intent); e != nil {
		return e
	}
	c := intent.Command
	kind := "TASK_CLOSURE_REQUESTED"
	refs := []*v1.Ref{c.TaskClosingRef, c.AdmissionRef, intent.JobRef}
	if intent.RecipientReceipt != nil {
		kind = "TASK_CLOSURE_ACKNOWLEDGED"
		refs = append(refs, intent.RecipientReceipt.DecisionRef, intent.RecipientReceipt.ResultRef)
	}
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: kind, SourceRecordRef: intent.Ref, TaskId: intent.TaskId, OperationId: c.OperationId, OriginCommand: c.Header.Identity, RelatedRefs: refs})
}
