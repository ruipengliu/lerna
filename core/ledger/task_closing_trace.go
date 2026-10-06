package ledger

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) saveTaskClosureSeal(ctx context.Context, seal *v1.TaskClosureSeal, task *v1.GlobalName, origin *v1.CommandIdentity) error {
	if e := s.store.(taskClosureSealStore).SaveTaskClosureSeal(ctx, seal); e != nil {
		return e
	}
	refs := append([]*v1.Ref{seal.IntentRef, seal.TaskClosingRef, seal.AdmissionRef, seal.OperationRef, seal.ExecutionFollowupRef}, seal.ClosedSendRefs...)
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "TASK_CLOSURE_SEALED", SourceRecordRef: seal.Ref, TaskId: task, OperationId: seal.OperationId, OriginCommand: origin, RelatedRefs: refs})
}

func (s *Service) saveExecutionFollowup(ctx context.Context, f *v1.ExecutionFollowup) error {
	if e := s.store.(executionFollowupStore).SaveExecutionFollowup(ctx, f); e != nil {
		return e
	}
	refs := append([]*v1.Ref{f.TaskClosingRef, f.AdmissionRef, f.OperationRef, f.AttemptRef, f.JobRef}, f.SendRefs...)
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "EXECUTION_FOLLOWUP_RETAINED", SourceRecordRef: f.Ref, TaskId: f.TaskId, OperationId: f.OperationId, AttemptId: f.GetAttemptRef().GetName(), ReasonCode: f.WaitingReason, RelatedRefs: refs})
}

func (s *Service) saveExecutionFollowupJob(ctx context.Context, f *v1.ExecutionFollowup, job *v1.Job) error {
	if e := s.store.SaveLedgerJob(ctx, job); e != nil {
		return e
	}
	refs := append([]*v1.Ref{f.Ref, f.TaskClosingRef, f.AdmissionRef, f.OperationRef, f.AttemptRef, job.SpecificationRef}, f.SendRefs...)
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "EXECUTION_FOLLOWUP_" + job.State, SourceRecordRef: job.Ref, TaskId: f.TaskId, OperationId: f.OperationId, AttemptId: f.GetAttemptRef().GetName(), OriginCommand: job.Responsibility, ReasonCode: job.WaitingReason, RelatedRefs: refs})
}
