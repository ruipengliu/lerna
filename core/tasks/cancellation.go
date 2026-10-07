package tasks

import (
	"context"
	"slices"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type cancellationStore interface {
	SaveCancellation(context.Context, *v1.Cancellation) error
	LoadCancellation(context.Context, *v1.GlobalName) (*v1.Cancellation, error)
	AllCancellations(context.Context) ([]*v1.Cancellation, error)
	SaveCancellationIntent(context.Context, *v1.CancellationClosureIntent) error
	LoadCancellationIntent(context.Context, *v1.Ref) (*v1.CancellationClosureIntent, error)
}
type CancellationJobs interface {
	EnqueueCancellationClosureInTransaction(context.Context, *v1.CancellationClosureIntent) (*v1.Ref, error)
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	ReadCancellationClosureClaim(context.Context, *v1.Job) (*v1.Job, error)
	CompleteCancellationClosureInTransaction(context.Context, *v1.Job) error
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}

func (s *Service) saveCancellation(ctx context.Context, t *v1.Task, p *v1.PlanningState, c *v1.SubmitInputCommand) error {
	if s.cancellationJobs == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	scope := &v1.Cancellation{Ref: command.NewRef(s.user, s.domain, "cancellation", "lerna.v1.Cancellation"), TaskId: t.TaskId, ControlIdentity: c.Header.Identity, ControlGeneration: t.ControlGeneration, SavedAtUnixMs: now, AdmissionRefs: p.AdmissionRefs}
	for _, ref := range scope.AdmissionRefs {
		a, e := s.store.LoadAdmission(ctx, ref)
		if e != nil {
			return e
		}
		if a == nil || !proto.Equal(a.TaskId, t.TaskId) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		intent := &v1.CancellationClosureIntent{Ref: command.NewRef(s.user, s.domain, "cancellation-intent", "lerna.v1.CancellationClosureIntent"), TaskId: t.TaskId}
		intent.Command = &v1.CloseCancellationCommand{Header: completionHeader(s.user, a.LedgerDomainId, "tasks-cancellation", "seal:"+intent.Ref.Name.LocalId), IntentRef: intent.Ref, CancellationRef: scope.Ref, AdmissionRef: a.Ref, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId}
		intent.JobRef, e = s.cancellationJobs.EnqueueCancellationClosureInTransaction(ctx, intent)
		if e != nil {
			return e
		}
		if e = s.saveCancellationIntent(ctx, intent); e != nil {
			return e
		}
		scope.ClosureIntentRefs = append(scope.ClosureIntentRefs, intent.Ref)
	}
	if e = s.store.SaveCancellation(ctx, scope); e != nil {
		return e
	}
	refs := append([]*v1.Ref{}, scope.AdmissionRefs...)
	refs = append(refs, scope.ClosureIntentRefs...)
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "CANCELLATION_SAVED", SourceRecordRef: scope.Ref, TaskId: scope.TaskId, OriginCommand: scope.ControlIdentity, RelatedRefs: refs})
}

func (s *Service) QueryCancellation(ctx context.Context, caller *v1.Caller, task *v1.GlobalName) (*v1.Cancellation, error) {
	if e := command.CheckName(caller, task, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.LoadCancellation(ctx, task)
}
func (s *Service) QueryCancellationIntent(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.CancellationClosureIntent, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.CancellationClosureIntent" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "cancellation-intent"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadCancellationIntent(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

type CancellationCloser interface {
	CloseForCancellation(context.Context, *v1.Caller, *v1.CloseCancellationCommand) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

func (s *Service) WithCancellationClosures(j CancellationJobs, c CancellationCloser) *Service {
	s.cancellationJobs = j
	s.cancellationCloser = c
	return s
}

// ValidateCancellationClosure 只接受同事务保存的取消依据和准确清单，不借用完成核验。
func (s *Service) ValidateCancellationClosure(ctx context.Context, caller *v1.Caller, c *v1.CloseCancellationCommand) (*v1.Admission, error) {
	if caller.GetIssuerId() != "tasks-cancellation" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	intent, e := s.QueryCancellationIntent(ctx, caller, c.IntentRef)
	if e != nil {
		return nil, e
	}
	if intent == nil || !proto.Equal(intent.Command, c) {
		return nil, command.Fail("INVALID_CLOSURE")
	}
	scope, e := s.QueryCancellation(ctx, caller, intent.TaskId)
	if e != nil {
		return nil, e
	}
	listed := false
	admitted := false
	if scope != nil && proto.Equal(scope.Ref, c.CancellationRef) && scope.ControlGeneration > 0 && scope.ControlIdentity != nil {
		for _, ref := range scope.ClosureIntentRefs {
			if proto.Equal(ref, intent.Ref) {
				listed = true
			}
		}
		for _, ref := range scope.AdmissionRefs {
			if proto.Equal(ref, c.AdmissionRef) {
				admitted = true
			}
		}
	}
	a, e := s.QueryAdmission(ctx, caller, c.AdmissionRef)
	if e != nil {
		return nil, e
	}
	if !listed || !admitted || a == nil || !proto.Equal(a.TaskId, intent.TaskId) || !proto.Equal(a.OperationId, c.OperationId) || a.ExecutorEndpointId != c.ExecutorEndpointId {
		return nil, command.Fail("INVALID_CLOSURE")
	}
	return a, nil
}
func (s *Service) ProcessCancellationClosureClaim(ctx context.Context, j *v1.Job) error {
	return s.processClosureClaim(ctx, j, cancellationClosureDelivery)
}

// ProcessCancellations 只交付封闭，不替代原意图接纳或省略源域回执。
func (s *Service) ProcessCancellations(ctx context.Context, c *v1.Caller) error {
	if c.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	all, e := s.store.AllCancellations(ctx)
	if e != nil {
		return e
	}
	for _, scope := range all {
		if len(scope.AdmissionRefs) == 0 {
			if e = s.store.Transaction(ctx, "tasks.cancellation_receipt", func(tx context.Context) error {
				return s.updateCancellationWaiting(tx, c, scope.TaskId)
			}); e != nil {
				return e
			}
		}
	}
	return s.processClosureDeliveries(ctx, c, cancellationClosureDelivery)
}

// updateCancellationWaiting 端点回执齐备只解除封闭等待，不推断效果或关闭任务。
func (s *Service) updateCancellationWaiting(ctx context.Context, caller *v1.Caller, id *v1.GlobalName) error {
	scope, e := s.QueryCancellation(ctx, caller, id)
	if e != nil || scope == nil {
		return e
	}
	for _, ref := range scope.ClosureIntentRefs {
		intent, e := s.QueryCancellationIntent(ctx, caller, ref)
		if e != nil {
			return e
		}
		if intent == nil || intent.RecipientReceipt == nil {
			return nil
		}
	}
	task, e := s.QueryTask(ctx, caller, id)
	if e != nil {
		return e
	}
	if task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING {
		return nil
	}
	if !slices.Contains(task.WaitingOn, "CANCELLATION_CLOSURE") {
		return nil
	}
	task.WaitingOn = slices.DeleteFunc(task.WaitingOn, func(wait string) bool { return wait == "CANCELLATION_CLOSURE" })
	task.Revision++
	return s.saveTask(ctx, task)
}

// RecoverCancellations 等待旧领取自然失效后继续原交接。
func (s *Service) RecoverCancellations(ctx context.Context, caller *v1.Caller) error {
	return s.recoverClosureDeliveries(ctx, caller, cancellationClosureDelivery)
}
