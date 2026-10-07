package tasks

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type completionIntentStore interface {
	SaveCompletionIntent(context.Context, *v1.CompletionClosureIntent) error
	LoadCompletionIntent(context.Context, *v1.Ref) (*v1.CompletionClosureIntent, error)
}
type CompletionJobs interface {
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	EnqueueCompletionClosureInTransaction(context.Context, *v1.CompletionClosureIntent) (*v1.Ref, error)
	ReadCompletionClosureClaim(context.Context, *v1.Job) (*v1.Job, error)
	CompleteCompletionClosureInTransaction(context.Context, *v1.Job) error
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}
type CompletionCloser interface {
	CloseForCompletion(context.Context, *v1.Caller, *v1.CloseCompletionCommand) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

func (s *Service) WithCompletionClosures(j CompletionJobs, c CompletionCloser) *Service {
	s.completionJobs = j
	s.completionCloser = c
	return s
}

func (s *Service) addCompletionClosures(ctx context.Context, v *v1.Verification, refs []*v1.Ref, scopeRef *v1.Ref) error {
	seen := map[string]bool{}
	for _, ref := range v.ClosureIntentRefs {
		intent, e := s.store.LoadCompletionIntent(ctx, ref)
		if e != nil {
			return e
		}
		if intent == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		seen[intent.Command.AdmissionRef.Name.LocalId] = true
	}
	for _, ref := range refs {
		if seen[ref.Name.LocalId] {
			continue
		}
		a, e := s.store.LoadAdmission(ctx, ref)
		if e != nil {
			return e
		}
		if a == nil || !proto.Equal(a.TaskId, v.TaskId) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		intent := &v1.CompletionClosureIntent{Ref: command.NewRef(s.user, s.domain, "completion-intent", "lerna.v1.CompletionClosureIntent"), TaskId: v.TaskId}
		intent.Command = &v1.CloseCompletionCommand{Header: completionHeader(s.user, a.LedgerDomainId, "tasks-completion", "seal:"+intent.Ref.Name.LocalId), IntentRef: intent.Ref, VerificationRef: proto.Clone(scopeRef).(*v1.Ref), AdmissionRef: a.Ref, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId}
		intent.JobRef, e = s.completionJobs.EnqueueCompletionClosureInTransaction(ctx, intent)
		if e != nil {
			return e
		}
		if e = s.saveCompletionIntent(ctx, intent); e != nil {
			return e
		}
		v.ClosureIntentRefs = append(v.ClosureIntentRefs, intent.Ref)
	}
	return nil
}
func (s *Service) QueryCompletionIntent(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.CompletionClosureIntent, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.CompletionClosureIntent" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "completion-intent"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadCompletionIntent(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

// ValidateCompletionClosure 不依赖临时轮次状态；已经保存的准确动作封闭永不撤回。
func (s *Service) ValidateCompletionClosure(ctx context.Context, c *v1.Caller, request *v1.CloseCompletionCommand) (*v1.Admission, error) {
	if c.GetIssuerId() != "tasks-completion" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	intent, e := s.QueryCompletionIntent(ctx, c, request.IntentRef)
	if e != nil {
		return nil, e
	}
	if intent == nil || !proto.Equal(intent.Command, request) {
		return nil, command.Fail("INVALID_CLOSURE")
	}
	v, e := s.QueryVerification(ctx, c, request.VerificationRef)
	if e != nil {
		return nil, e
	}
	found := false
	if v != nil && proto.Equal(v.TaskId, intent.TaskId) {
		for _, ref := range v.ClosureIntentRefs {
			if proto.Equal(ref, intent.Ref) {
				found = true
			}
		}
	}
	if !found {
		return nil, command.Fail("INVALID_CLOSURE")
	}
	a, e := s.QueryAdmission(ctx, c, request.AdmissionRef)
	if e != nil {
		return nil, e
	}
	if a == nil || !proto.Equal(a.TaskId, intent.TaskId) || !proto.Equal(a.OperationId, request.OperationId) || a.ExecutorEndpointId != request.ExecutorEndpointId {
		return nil, command.Fail("INVALID_CLOSURE")
	}
	return a, nil
}
func (s *Service) ProcessCompletionClosureClaim(ctx context.Context, j *v1.Job) error {
	current, e := s.completionJobs.ReadCompletionClosureClaim(ctx, j)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "tasks-completion"}
	intent, e := s.QueryCompletionIntent(ctx, actor, current.SpecificationRef)
	if e != nil {
		return e
	}
	if intent == nil || !proto.Equal(intent.Command.Header.Identity, current.Responsibility) {
		return command.Fail("INVARIANT_VIOLATION")
	}
	c := intent.Command
	q, e := s.completionCloser.QueryReceipt(ctx, actor, c.Header.Identity)
	if e != nil {
		return e
	}
	var r *v1.CommandReceipt
	switch q.State {
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
		r = q.Receipt
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
		r, e = s.completionCloser.CloseForCompletion(ctx, actor, c)
		if e != nil {
			return e
		}
	default:
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || !proto.Equal(r.Identity, c.Header.Identity) || r.ResponsibleDomainId != c.OperationId.AuthorityDomainId || r.Fingerprint != command.SemanticFingerprint("completion-seal", c) || r.ResultRef.GetSchemaId() != "lerna.v1.CompletionSeal" {
		return command.Fail("HANDOFF_RECEIPT_INVALID")
	}
	return s.store.Transaction(ctx, "tasks.completion_receipt", func(tx context.Context) error {
		currentIntent, e := s.QueryCompletionIntent(tx, actor, intent.Ref)
		if e != nil {
			return e
		}
		if currentIntent.RecipientReceipt != nil && !proto.Equal(currentIntent.RecipientReceipt, r) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		if e = s.completionJobs.CompleteCompletionClosureInTransaction(tx, current); e != nil {
			return e
		}
		currentIntent.RecipientReceipt = r
		return s.saveCompletionIntent(tx, currentIntent)
	})
}

// ProcessCompletionClosures 只交付封闭，不替代原意图接纳或省略源域回执。
func (s *Service) ProcessCompletionClosures(ctx context.Context, c *v1.Caller) error {
	if c.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	pending, e := s.pendingCompletionClosures(ctx, c)
	if e != nil || !pending {
		return e
	}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	for {
		id := &v1.CommandIdentity{UserId: s.user, IssuerId: c.IssuerId, TargetDomainId: s.domain, CommandId: command.NewRef(s.user, s.domain, "command", "command").Name.LocalId}
		r, e := s.completionJobs.ExecuteJob(ctx, c, &v1.JobCommand{Identity: id, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_COMPLETION_CLOSURE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			return nil
		}
		if e = s.ProcessCompletionClosureClaim(ctx, r.Jobs[0]); e != nil {
			return e
		}
	}
}

func (s *Service) pendingCompletionClosures(ctx context.Context, c *v1.Caller) (bool, error) {
	jobs, e := s.completionJobs.Pending(ctx, c)
	if e != nil {
		return false, e
	}
	for _, j := range jobs {
		if j.Module == "tasks" && j.JobType == "DELIVER_COMPLETION_CLOSURE" {
			return true, nil
		}
	}
	return false, nil
}

// RecoverCompletions 在宿主的有界恢复期限内等待旧领取自然失效，不提前接管。
func (s *Service) RecoverCompletions(ctx context.Context, c *v1.Caller) error {
	for {
		if e := s.ProcessCompletions(ctx, c); e != nil {
			return e
		}
		pending, e := s.pendingCompletionClosures(ctx, c)
		if e != nil {
			return e
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
