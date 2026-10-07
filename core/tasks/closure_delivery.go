package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type closureDeliveryKind uint8

const (
	completionClosureDelivery closureDeliveryKind = iota
	cancellationClosureDelivery
	taskClosureDelivery
)

type closureReceiptSource interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

// closureDelivery 只携带核心固定类型的原意图及其回执约束，不接受处理函数。
type closureDelivery struct {
	kind         closureDeliveryKind
	claim        *v1.Job
	actor        *v1.Caller
	identity     *v1.CommandIdentity
	domain       string
	fingerprint  string
	resultSchema string
	recipient    closureReceiptSource
	completion   *v1.CompletionClosureIntent
	cancellation *v1.CancellationClosureIntent
	taskClosing  *v1.TaskClosureIntent
}

// processClosureClaim 保留意图、接收方接纳与源确认三个提交域；只补交原身份。
func (s *Service) processClosureClaim(ctx context.Context, j *v1.Job, kind closureDeliveryKind) error {
	delivery, e := s.readClosureDelivery(ctx, j, kind)
	if e != nil {
		return e
	}
	q, e := delivery.recipient.QueryReceipt(ctx, delivery.actor, delivery.identity)
	if e != nil {
		return e
	}
	var r *v1.CommandReceipt
	switch q.State {
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
		r = q.Receipt
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
		r, e = s.deliverClosure(ctx, delivery)
		if e != nil {
			return e
		}
	default:
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || !proto.Equal(r.Identity, delivery.identity) || r.ResponsibleDomainId != delivery.domain || r.Fingerprint != delivery.fingerprint || r.ResultRef.GetSchemaId() != delivery.resultSchema {
		return command.Fail("HANDOFF_RECEIPT_INVALID")
	}
	acknowledge := func(tx context.Context) error {
		return s.acknowledgeClosureReceipt(tx, delivery, r)
	}
	switch delivery.kind {
	case completionClosureDelivery:
		return s.store.Transaction(ctx, "tasks.completion_receipt", acknowledge)
	case cancellationClosureDelivery:
		return s.store.Transaction(ctx, "tasks.cancellation_receipt", acknowledge)
	case taskClosureDelivery:
		return s.store.Transaction(ctx, "tasks.task_closure_receipt", acknowledge)
	default:
		return command.Fail("INVALID_JOB")
	}
}

func (s *Service) readClosureDelivery(ctx context.Context, j *v1.Job, kind closureDeliveryKind) (*closureDelivery, error) {
	delivery := &closureDelivery{kind: kind}
	switch kind {
	case completionClosureDelivery:
		current, e := s.completionJobs.ReadCompletionClosureClaim(ctx, j)
		if e != nil {
			return nil, e
		}
		delivery.claim = current
		delivery.actor = &v1.Caller{UserId: s.user, IssuerId: "tasks-completion"}
		intent, e := s.QueryCompletionIntent(ctx, delivery.actor, current.SpecificationRef)
		if e != nil {
			return nil, e
		}
		if intent == nil || !proto.Equal(intent.Command.Header.Identity, current.Responsibility) {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		delivery.completion = intent
		c := intent.Command
		delivery.identity, delivery.domain = c.Header.Identity, c.OperationId.AuthorityDomainId
		delivery.fingerprint = command.SemanticFingerprint("completion-seal", c)
		delivery.resultSchema = "lerna.v1.CompletionSeal"
		delivery.recipient = s.completionCloser
	case cancellationClosureDelivery:
		current, e := s.cancellationJobs.ReadCancellationClosureClaim(ctx, j)
		if e != nil {
			return nil, e
		}
		delivery.claim = current
		delivery.actor = &v1.Caller{UserId: s.user, IssuerId: "tasks-cancellation"}
		intent, e := s.QueryCancellationIntent(ctx, delivery.actor, current.SpecificationRef)
		if e != nil {
			return nil, e
		}
		if intent == nil || !proto.Equal(intent.Command.Header.Identity, current.Responsibility) {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		delivery.cancellation = intent
		c := intent.Command
		delivery.identity, delivery.domain = c.Header.Identity, c.OperationId.AuthorityDomainId
		delivery.fingerprint = command.SemanticFingerprint("cancellation-seal", c)
		delivery.resultSchema = "lerna.v1.CancellationSeal"
		delivery.recipient = s.cancellationCloser
	case taskClosureDelivery:
		current, e := s.taskClosingJobs.ReadTaskClosureClaim(ctx, j)
		if e != nil {
			return nil, e
		}
		delivery.claim = current
		delivery.actor = &v1.Caller{UserId: s.user, IssuerId: "tasks-closing"}
		intent, e := s.QueryTaskClosureIntent(ctx, delivery.actor, current.SpecificationRef)
		if e != nil {
			return nil, e
		}
		if intent == nil || !proto.Equal(intent.Command.Header.Identity, current.Responsibility) {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		delivery.taskClosing = intent
		c := intent.Command
		delivery.identity, delivery.domain = c.Header.Identity, c.OperationId.AuthorityDomainId
		delivery.fingerprint = command.SemanticFingerprint("task-closure-seal", c)
		delivery.resultSchema = "lerna.v1.TaskClosureSeal"
		delivery.recipient = s.taskCloser
	default:
		return nil, command.Fail("INVALID_JOB")
	}
	return delivery, nil
}

func (s *Service) deliverClosure(ctx context.Context, delivery *closureDelivery) (*v1.CommandReceipt, error) {
	switch delivery.kind {
	case completionClosureDelivery:
		return s.completionCloser.CloseForCompletion(ctx, delivery.actor, delivery.completion.Command)
	case cancellationClosureDelivery:
		return s.cancellationCloser.CloseForCancellation(ctx, delivery.actor, delivery.cancellation.Command)
	case taskClosureDelivery:
		return s.taskCloser.CloseForTaskClose(ctx, delivery.actor, delivery.taskClosing.Command)
	default:
		return nil, command.Fail("INVALID_JOB")
	}
}

func (s *Service) acknowledgeClosureReceipt(ctx context.Context, delivery *closureDelivery, r *v1.CommandReceipt) error {
	var original *v1.CommandReceipt
	var completion *v1.CompletionClosureIntent
	var cancellation *v1.CancellationClosureIntent
	var taskClosing *v1.TaskClosureIntent
	var e error
	switch delivery.kind {
	case completionClosureDelivery:
		completion, e = s.QueryCompletionIntent(ctx, delivery.actor, delivery.completion.Ref)
		if e != nil {
			return e
		}
		original = completion.RecipientReceipt
	case cancellationClosureDelivery:
		cancellation, e = s.QueryCancellationIntent(ctx, delivery.actor, delivery.cancellation.Ref)
		if e != nil {
			return e
		}
		if cancellation == nil || !proto.Equal(cancellation.Command, delivery.cancellation.Command) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		original = cancellation.RecipientReceipt
	case taskClosureDelivery:
		taskClosing, e = s.QueryTaskClosureIntent(ctx, delivery.actor, delivery.taskClosing.Ref)
		if e != nil {
			return e
		}
		original = taskClosing.RecipientReceipt
	default:
		return command.Fail("INVALID_JOB")
	}
	if original != nil && !proto.Equal(original, r) {
		return command.Fail("INVARIANT_VIOLATION")
	}
	// 原领取必须在源确认事务内再次有效，不能用已经过期的交付完成工作。
	switch delivery.kind {
	case completionClosureDelivery:
		if e = s.completionJobs.CompleteCompletionClosureInTransaction(ctx, delivery.claim); e != nil {
			return e
		}
		completion.RecipientReceipt = r
		return s.saveCompletionIntent(ctx, completion)
	case cancellationClosureDelivery:
		if e = s.cancellationJobs.CompleteCancellationClosureInTransaction(ctx, delivery.claim); e != nil {
			return e
		}
		cancellation.RecipientReceipt = r
		if e = s.saveCancellationIntent(ctx, cancellation); e != nil {
			return e
		}
		return s.updateCancellationWaiting(ctx, delivery.actor, cancellation.TaskId)
	case taskClosureDelivery:
		if e = s.taskClosingJobs.CompleteTaskClosureInTransaction(ctx, delivery.claim); e != nil {
			return e
		}
		taskClosing.RecipientReceipt = r
		return s.saveTaskClosureIntent(ctx, taskClosing)
	default:
		return command.Fail("INVALID_JOB")
	}
}
