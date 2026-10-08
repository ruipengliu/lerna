package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type completionIntentStore interface {
	SaveCompletionIntent(context.Context, *v1.CompletionClosureIntent) error
	LoadCompletionIntent(context.Context, *v1.Ref) (*v1.CompletionClosureIntent, error)
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
		intent.JobRef, e = s.closureJobs.EnqueueClosureInTransaction(ctx, completionClosure{}.spec().jobType, intent.Ref, intent.Command.Header.Identity, intent.Command.ExecutorEndpointId, intent.Command.OperationId.AuthorityDomainId)
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

// ProcessCompletionClosures 只交付封闭，不替代原意图接纳或省略源域回执。
func (s *Service) ProcessCompletionClosures(ctx context.Context, c *v1.Caller) error {
	return s.processClosureDeliveries(ctx, c, completionClosure{})
}

// RecoverCompletions 在宿主的有界恢复期限内等待旧领取自然失效，不提前接管。
func (s *Service) RecoverCompletions(ctx context.Context, c *v1.Caller) error {
	return s.recoverClosureDeliveries(ctx, c, completionClosure{})
}
