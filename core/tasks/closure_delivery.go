package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ClosureJobs 保存和领取三种固定封闭交付工作；工作类型只能是 closureKinds 中的原值。
type ClosureJobs interface {
	QueryJob(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Job, error)
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	EnqueueClosureInTransaction(ctx context.Context, jobType string, intentRef *v1.Ref, responsibility *v1.CommandIdentity, executorEndpointID, ledgerDomainID string) (*v1.Ref, error)
	ReadClosureClaim(context.Context, *v1.Job) (*v1.Job, error)
	CompleteClosureInTransaction(context.Context, *v1.Job) error
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}

// ClosureRecipient 是原执行端点的封闭接纳与原命令回执查询。
type ClosureRecipient interface {
	CloseForCompletion(context.Context, *v1.Caller, *v1.CloseCompletionCommand) (*v1.CommandReceipt, error)
	CloseForCancellation(context.Context, *v1.Caller, *v1.CloseCancellationCommand) (*v1.CommandReceipt, error)
	CloseForTaskClose(context.Context, *v1.Caller, *v1.CloseTaskEndpointCommand) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

func (s *Service) WithClosures(j ClosureJobs, r ClosureRecipient) *Service {
	s.closureJobs = j
	s.closureRecipient = r
	return s
}

// closureSpec 是一种封闭已经保存和登记的原值，均不随重构改变。
type closureSpec struct {
	jobType        string
	issuer         string
	fingerprintTag string
	resultSchema   string
}

// closureIntent 是交付算法需要的原意图视图；message 保留类别自己的原意图。
type closureIntent struct {
	message  proto.Message
	command  proto.Message
	identity *v1.CommandIdentity
	domain   string
	receipt  *v1.CommandReceipt
}

// closureKind 是编译期固定的封闭集合，没有注册入口；类别只提供原值和类型化读写。
type closureKind interface {
	spec() closureSpec
	// receiptTransaction 在调用处保留原固定持久化点名称，供故障登记检查核对。
	receiptTransaction(context.Context, *Service, func(context.Context) error) error
	load(context.Context, *Service, *v1.Caller, *v1.Ref) (*closureIntent, error)
	send(context.Context, ClosureRecipient, *v1.Caller, *closureIntent) (*v1.CommandReceipt, error)
	acknowledge(context.Context, *Service, *v1.Caller, *closureIntent, *v1.CommandReceipt) error
	// process 是该类别的原推进入口，恢复时用它重读本类别的业务记录。
	process(context.Context, *Service, *v1.Caller) error
}

var closureKinds = []closureKind{completionClosure{}, cancellationClosure{}, taskClosure{}}

func closureKindFor(jobType string) (closureKind, error) {
	for _, kind := range closureKinds {
		if kind.spec().jobType == jobType {
			return kind, nil
		}
	}
	return nil, command.Fail("INVALID_JOB")
}

type completionClosure struct{}

func (completionClosure) spec() closureSpec {
	return closureSpec{jobType: "DELIVER_COMPLETION_CLOSURE", issuer: "tasks-completion", fingerprintTag: "completion-seal", resultSchema: "lerna.v1.CompletionSeal"}
}
func (completionClosure) receiptTransaction(ctx context.Context, s *Service, fn func(context.Context) error) error {
	return s.store.Transaction(ctx, "tasks.completion_receipt", fn)
}
func (completionClosure) load(ctx context.Context, s *Service, actor *v1.Caller, ref *v1.Ref) (*closureIntent, error) {
	intent, e := s.QueryCompletionIntent(ctx, actor, ref)
	if e != nil || intent == nil {
		return nil, e
	}
	c := intent.Command
	return &closureIntent{message: intent, command: c, identity: c.Header.Identity, domain: c.OperationId.AuthorityDomainId, receipt: intent.RecipientReceipt}, nil
}
func (completionClosure) send(ctx context.Context, r ClosureRecipient, actor *v1.Caller, intent *closureIntent) (*v1.CommandReceipt, error) {
	return r.CloseForCompletion(ctx, actor, intent.command.(*v1.CloseCompletionCommand))
}
func (completionClosure) acknowledge(ctx context.Context, s *Service, _ *v1.Caller, intent *closureIntent, r *v1.CommandReceipt) error {
	saved := intent.message.(*v1.CompletionClosureIntent)
	saved.RecipientReceipt = r
	return s.saveCompletionIntent(ctx, saved)
}
func (completionClosure) process(ctx context.Context, s *Service, c *v1.Caller) error {
	return s.ProcessCompletions(ctx, c)
}

type cancellationClosure struct{}

func (cancellationClosure) spec() closureSpec {
	return closureSpec{jobType: "DELIVER_CANCELLATION_CLOSURE", issuer: "tasks-cancellation", fingerprintTag: "cancellation-seal", resultSchema: "lerna.v1.CancellationSeal"}
}
func (cancellationClosure) receiptTransaction(ctx context.Context, s *Service, fn func(context.Context) error) error {
	return s.store.Transaction(ctx, "tasks.cancellation_receipt", fn)
}
func (cancellationClosure) load(ctx context.Context, s *Service, actor *v1.Caller, ref *v1.Ref) (*closureIntent, error) {
	intent, e := s.QueryCancellationIntent(ctx, actor, ref)
	if e != nil || intent == nil {
		return nil, e
	}
	c := intent.Command
	return &closureIntent{message: intent, command: c, identity: c.Header.Identity, domain: c.OperationId.AuthorityDomainId, receipt: intent.RecipientReceipt}, nil
}
func (cancellationClosure) send(ctx context.Context, r ClosureRecipient, actor *v1.Caller, intent *closureIntent) (*v1.CommandReceipt, error) {
	return r.CloseForCancellation(ctx, actor, intent.command.(*v1.CloseCancellationCommand))
}
func (cancellationClosure) acknowledge(ctx context.Context, s *Service, actor *v1.Caller, intent *closureIntent, r *v1.CommandReceipt) error {
	saved := intent.message.(*v1.CancellationClosureIntent)
	saved.RecipientReceipt = r
	if e := s.saveCancellationIntent(ctx, saved); e != nil {
		return e
	}
	return s.updateCancellationWaiting(ctx, actor, saved.TaskId)
}
func (cancellationClosure) process(ctx context.Context, s *Service, c *v1.Caller) error {
	return s.ProcessCancellations(ctx, c)
}

type taskClosure struct{}

func (taskClosure) spec() closureSpec {
	return closureSpec{jobType: "DELIVER_TASK_CLOSURE", issuer: "tasks-closing", fingerprintTag: "task-closure-seal", resultSchema: "lerna.v1.TaskClosureSeal"}
}
func (taskClosure) receiptTransaction(ctx context.Context, s *Service, fn func(context.Context) error) error {
	return s.store.Transaction(ctx, "tasks.task_closure_receipt", fn)
}
func (taskClosure) load(ctx context.Context, s *Service, actor *v1.Caller, ref *v1.Ref) (*closureIntent, error) {
	intent, e := s.QueryTaskClosureIntent(ctx, actor, ref)
	if e != nil || intent == nil {
		return nil, e
	}
	c := intent.Command
	return &closureIntent{message: intent, command: c, identity: c.Header.Identity, domain: c.OperationId.AuthorityDomainId, receipt: intent.RecipientReceipt}, nil
}
func (taskClosure) send(ctx context.Context, r ClosureRecipient, actor *v1.Caller, intent *closureIntent) (*v1.CommandReceipt, error) {
	return r.CloseForTaskClose(ctx, actor, intent.command.(*v1.CloseTaskEndpointCommand))
}
func (taskClosure) acknowledge(ctx context.Context, s *Service, _ *v1.Caller, intent *closureIntent, r *v1.CommandReceipt) error {
	saved := intent.message.(*v1.TaskClosureIntent)
	saved.RecipientReceipt = r
	return s.saveTaskClosureIntent(ctx, saved)
}
func (taskClosure) process(ctx context.Context, s *Service, c *v1.Caller) error {
	return s.processClosureDeliveries(ctx, c, taskClosure{})
}

// ProcessClosureClaim 保留意图、接收方接纳与源确认三个提交域；按保存的工作类型分派，只补交原身份。
func (s *Service) ProcessClosureClaim(ctx context.Context, j *v1.Job) error {
	claim, e := s.closureJobs.ReadClosureClaim(ctx, j)
	if e != nil {
		return e
	}
	kind, e := closureKindFor(claim.JobType)
	if e != nil {
		return e
	}
	spec := kind.spec()
	actor := &v1.Caller{UserId: s.user, IssuerId: spec.issuer}
	intent, e := kind.load(ctx, s, actor, claim.SpecificationRef)
	if e != nil {
		return e
	}
	if intent == nil || !proto.Equal(intent.identity, claim.Responsibility) {
		return command.Fail("INVARIANT_VIOLATION")
	}
	q, e := s.closureRecipient.QueryReceipt(ctx, actor, intent.identity)
	if e != nil {
		return e
	}
	var r *v1.CommandReceipt
	switch q.State {
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
		r = q.Receipt
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
		r, e = kind.send(ctx, s.closureRecipient, actor, intent)
		if e != nil {
			return e
		}
	default:
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || !proto.Equal(r.Identity, intent.identity) || r.ResponsibleDomainId != intent.domain || r.Fingerprint != command.SemanticFingerprint(spec.fingerprintTag, intent.command) || r.ResultRef.GetSchemaId() != spec.resultSchema {
		return command.Fail("HANDOFF_RECEIPT_INVALID")
	}
	return kind.receiptTransaction(ctx, s, func(tx context.Context) error {
		saved, e := kind.load(tx, s, actor, claim.SpecificationRef)
		if e != nil {
			return e
		}
		if saved == nil || !proto.Equal(saved.command, intent.command) || (saved.receipt != nil && !proto.Equal(saved.receipt, r)) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		// 原领取必须在源确认事务内再次有效，不能用已经过期的交付完成工作。
		if e = s.closureJobs.CompleteClosureInTransaction(tx, claim); e != nil {
			return e
		}
		return kind.acknowledge(tx, s, actor, saved, r)
	})
}
