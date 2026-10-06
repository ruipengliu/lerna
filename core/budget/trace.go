package budget

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveBudget(ctx context.Context, v *v1.Budget) error {
	if err := s.store.SaveBudget(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: "BUDGET_CHANGED", SourceRecordRef: v.Ref, TaskId: v.TaskId})
}
func (s *Service) saveReservation(ctx context.Context, v *v1.Reservation) error {
	if err := s.store.SaveReservation(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: "RESERVATION_CHANGED", SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, RelatedRefs: append([]*v1.Ref{v.AdmissionRef}, v.BudgetRefs...)})
}
func (s *Service) saveBillingSource(ctx context.Context, v *v1.BillingSource) error {
	if err := s.store.(billingStore).SaveBillingSource(ctx, v); err != nil {
		return err
	}
	source := s.store
	kind := "BILLING_UNKNOWN"
	switch v.Status {
	case "SETTLED":
		kind = "BILLING_SETTLED"
	case "CONFLICT":
		kind = "BILLING_CONFLICT"
	case "UNUSED_CLOSED":
		kind = "BILLING_RELEASED"
	}
	return source.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: kind, SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, SendRef: v.SendRef, RelatedRefs: []*v1.Ref{v.ReservationRef, v.AdmissionRef, v.EntryRef, v.PriceRuleRef, v.ConflictRef}})
}
func (s *Service) saveUsage(ctx context.Context, v *v1.UsageReport) error {
	if err := s.store.(usageStore).SaveUsage(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "budget", &v1.TraceEvent{EventType: "USAGE_ACCEPTED", SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, AttemptId: v.AttemptId, SendRef: v.SendRef, RelatedRefs: []*v1.Ref{v.BillingSource, v.MeasurementRef, v.PriceRuleRef}})
}
