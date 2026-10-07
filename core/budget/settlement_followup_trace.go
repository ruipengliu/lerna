package budget

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) saveSettlementFollowup(ctx context.Context, f *v1.SettlementFollowup) error {
	if e := s.store.SaveSettlementFollowup(ctx, f); e != nil {
		return e
	}
	refs := append([]*v1.Ref{f.TaskClosingRef, f.AdmissionRef, f.JobRef}, f.ReservationRefs...)
	refs = append(refs, f.BillingSourceRefs...)
	return s.store.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: "SETTLEMENT_FOLLOWUP_RETAINED", SourceRecordRef: f.Ref, TaskId: f.TaskId, OperationId: f.OperationId, RelatedRefs: refs})
}

func (s *Service) saveSettlementFollowupJob(ctx context.Context, f *v1.SettlementFollowup, job *v1.Job) error {
	if e := s.store.SaveJob(ctx, job); e != nil {
		return e
	}
	refs := append([]*v1.Ref{f.Ref, f.TaskClosingRef, f.AdmissionRef, job.SpecificationRef}, f.ReservationRefs...)
	refs = append(refs, f.BillingSourceRefs...)
	return s.store.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: "SETTLEMENT_FOLLOWUP_" + job.State, SourceRecordRef: job.Ref, TaskId: f.TaskId, OperationId: f.OperationId, OriginCommand: job.Responsibility, ReasonCode: job.WaitingReason, RelatedRefs: refs})
}
