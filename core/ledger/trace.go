package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveOperation(ctx context.Context, v *v1.Operation) error {
	if err := s.store.SaveOperation(ctx, v); err != nil {
		return err
	}
	source := s.store
	admission, err := s.starts.QueryAdmission(ctx, &v1.Caller{UserId: s.user, IssuerId: "ledger-report"}, v.AdmissionRef)
	if err != nil {
		return err
	}
	event := &v1.TraceEvent{EventType: "OPERATION_CHANGED", SourceRecordRef: v.Ref, TaskId: admission.TaskId, OperationId: v.Ref.Name, RelatedRefs: []*v1.Ref{v.AdmissionRef, v.EffectRef, v.ClosureWorkRef}}
	event.RelatedRefs = append(event.RelatedRefs, v.ClosureEvidenceRefs...)
	if v.Effect != nil {
		event.RelatedRefs = append(event.RelatedRefs, v.Effect.EvidenceRefs...)
		event.EffectOutcome = v.Effect.Outcome
		event.LateEffect = v.Effect.LateEffect
	}
	if v.Execution != nil {
		if v.Execution.Attempt != nil {
			event.AttemptId = v.Execution.Attempt.Ref.Name
			event.RelatedRefs = append(event.RelatedRefs, v.Execution.Attempt.Ref)
		}
		if v.Execution.Send != nil {
			event.SendRef = v.Execution.Send.Ref
			event.ObservationRef = v.Execution.Send.ObservationRef
			event.RelatedRefs = append(event.RelatedRefs, v.Execution.Send.ResendQueryObservationRef)
		}
		for _, send := range v.Execution.PreviousSends {
			event.RelatedRefs = append(event.RelatedRefs, send.Ref, send.ObservationRef, send.ResendQueryObservationRef)
		}
	}
	return source.SaveTraceSource(ctx, "ledger", event)
}

func (s *Service) saveCompletionSeal(ctx context.Context, v *v1.CompletionSeal, task *v1.GlobalName, origin *v1.CommandIdentity) error {
	if err := s.store.(completionSealStore).SaveCompletionSeal(ctx, v); err != nil {
		return err
	}
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "COMPLETION_SEALED", SourceRecordRef: v.Ref, TaskId: task, OperationId: v.OperationId, OriginCommand: origin, RelatedRefs: append([]*v1.Ref{v.IntentRef, v.VerificationRef, v.AdmissionRef, v.OperationRef}, v.ClosedSendRefs...)})
}

func (s *Service) recordReconciliationTrace(ctx context.Context, p *v1.Reconciliation) error {
	refs := []*v1.Ref{p.OriginalAttemptRef, p.QueryCapabilityRef, p.ParametersRef, p.GrantRef, p.ConfirmationRef, p.ActiveQueryRef, p.JobRef}
	refs = append(refs, p.QueryRefs...)
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "RECONCILIATION_" + p.State, SourceRecordRef: p.Ref, TaskId: p.TaskId, OperationId: p.OperationId, OriginCommand: p.RequestedBy, ObservationRef: p.LastObservationRef, ReasonCode: p.PauseReason, RelatedRefs: refs})
}

func (s *Service) saveReconciliationQuery(ctx context.Context, q *v1.ReconciliationQuery) error {
	if err := s.store.(reconciliationStore).SaveReconciliationQuery(ctx, q); err != nil {
		return err
	}
	refs := []*v1.Ref{q.Work.Ref, q.Work.OwnerRef, q.Work.SourceRef, q.Work.CapabilityRef, q.Work.ParametersRef, q.Work.GrantRef, q.QueryOperationRef, q.InterpretationRef}
	if q.AdmissionReceipt != nil {
		refs = append(refs, q.AdmissionReceipt.ResultRef, q.AdmissionReceipt.DecisionRef)
	}
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "RECONCILIATION_QUERY_" + q.State, SourceRecordRef: q.Ref, TaskId: q.Work.TaskId, OperationId: q.Work.QuerySubject.OperationId, OriginCommand: q.Work.AdmissionIdentity, ObservationRef: q.ObservationRef, ReasonCode: q.Reason, RelatedRefs: refs})
}

func (s *Service) saveReconciliationFinding(ctx context.Context, v *v1.ReconciliationFinding, task *v1.GlobalName, query *v1.Ref) error {
	if err := s.store.(reconciliationStore).SaveReconciliationFinding(ctx, v); err != nil {
		return err
	}
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "RECONCILIATION_FINDING", SourceRecordRef: v.Ref, TaskId: task, OperationId: v.OperationId, ObservationRef: v.ObservationRef, EffectOutcome: v.Outcome, LateEffect: v.LateEffect, ReasonCode: v.Reason, RelatedRefs: []*v1.Ref{v.QueryRef, query}})
}

func (s *Service) saveOperationProgressHandoff(ctx context.Context, h *v1.OperationProgressHandoff) error {
	if err := s.store.(progressStore).SaveOperationProgressHandoff(ctx, h); err != nil {
		return err
	}
	n := h.Notice
	kind := "PROGRESS_HANDOFF"
	refs := []*v1.Ref{n.OperationRef, n.ReconciliationRef, n.EffectRef}
	if h.RecipientReceipt != nil {
		kind = "PROGRESS_ACKNOWLEDGED"
		refs = append(refs, h.RecipientReceipt.DecisionRef)
	}
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: kind, SourceRecordRef: n.Ref, TaskId: n.TaskId, OperationId: n.OperationRef.Name, OriginCommand: h.Command.Header.Identity, ObservationRef: n.LastObservationRef, ReasonCode: n.PauseReason, RelatedRefs: refs})
}

func (s *Service) recordResendTrace(ctx context.Context, op *v1.Operation, c *v1.PrepareResendCommand) error {
	admission, err := s.starts.QueryAdmission(ctx, &v1.Caller{UserId: s.user, IssuerId: "ledger-report"}, op.AdmissionRef)
	if err != nil {
		return err
	}
	x := op.Execution
	return s.store.SaveTraceSource(ctx, "ledger", &v1.TraceEvent{EventType: "RESEND_PREPARED", SourceRecordRef: x.Send.Ref, OriginCommand: c.Header.Identity, TaskId: admission.TaskId, OperationId: op.Ref.Name, AttemptId: x.Attempt.Ref.Name, SendRef: x.Send.Ref, RelatedRefs: []*v1.Ref{op.Ref, op.AdmissionRef, x.Attempt.Ref, c.PreviousSendRef, x.Send.ResendQueryObservationRef}})
}

type observedWork interface {
	ExecuteObserved(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error), func(context.Context, *v1.CommandReceipt) error) (*v1.CommandReceipt, error)
}

func (s *Service) executeResendDecision(ctx context.Context, caller *v1.Caller, c *v1.PrepareResendCommand, fn func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error) {
	work, ok := s.work.(observedWork)
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return work.ExecuteObserved(ctx, caller, c.Header, command.SemanticFingerprint("prepare-resend", c.OperationId, c.PreviousSendRef, c.Claim), "ledger.resend", fn, func(tx context.Context, r *v1.CommandReceipt) error {
		if r.Decision != v1.Decision_DECISION_REJECTED {
			return nil
		}
		ev := &v1.TraceEvent{EventType: "DECISION_REJECTED", SourceRecordRef: r.DecisionRef, OriginCommand: r.Identity, ReasonCode: r.GetError().GetCode()}
		if command.CheckName(caller, c.OperationId, s.user, s.domain, "operation") == nil {
			op, err := s.store.LoadOperation(tx, c.OperationId)
			if err != nil {
				return err
			}
			if op != nil {
				admission, err := s.starts.QueryAdmission(tx, caller, op.AdmissionRef)
				if err != nil {
					return err
				}
				ev.TaskId = admission.TaskId
				ev.OperationId = op.Ref.Name
				ev.RelatedRefs = []*v1.Ref{op.Ref, op.AdmissionRef}
			}
		}
		return s.store.SaveTraceSource(tx, "ledger", ev)
	})
}
