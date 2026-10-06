package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) requestRejectedContinuation(ctx context.Context, caller *v1.Caller, t *v1.Task, p *v1.PlanningState, v *v1.Verification) error {
	if v.ContinuationRequestRef != nil || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || p.VerificationFreeze != 0 || !proto.Equal(p.VerificationRef, v.Ref) || t.InputVersion != v.InputVersion || t.RequirementsVersion != v.RequirementsVersion || t.ControlGeneration != v.ControlGeneration || !proto.Equal(p.Requirements.GetRef(), v.RequirementsRef) {
		return nil
	}
	if s.execution == nil {
		return command.Fail("EXECUTION_FACTS_UNAVAILABLE")
	}
	var all []*v1.Ref
	for _, ref := range p.AdmissionRefs {
		a, e := s.QueryAdmission(ctx, caller, ref)
		if e != nil {
			return e
		}
		if a == nil || !proto.Equal(a.TaskId, t.TaskId) {
			return command.Fail("INVARIANT_VIOLATION")
		}
		all = append(all, &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"})
	}
	blocked, e := s.execution.BlocksAdmission(ctx, caller, all, p.RejectedVerificationOperations)
	if e != nil {
		setCompletionWaiting(t, []string{"COMPLETION:BLOCKER_FACTS_UNAVAILABLE"})
		return nil
	}
	if blocked {
		setCompletionWaiting(t, []string{"COMPLETION:BLOCKING_OPERATIONS"})
		return nil
	}
	setCompletionWaiting(t, nil)
	id := &v1.CommandIdentity{UserId: s.user, IssuerId: "tasks-completion", TargetDomainId: s.domain, CommandId: "continuation:" + v.Ref.Name.LocalId}
	if e = s.requestProposalInTransaction(ctx, caller, t, p, id); e != nil {
		return e
	}
	v.ContinuationRequestRef = p.Snapshot.RequestRef
	return nil
}

// continueRejectedCompletion 重查当前轮次；通知和恢复不得再次生成已登记的继续请求。
func (s *Service) continueRejectedCompletion(ctx context.Context, c *v1.Caller, old *v1.Verification) error {
	return s.store.Transaction(ctx, "tasks.completion", func(tx context.Context) error {
		t, e := s.QueryTask(tx, c, old.TaskId)
		if e != nil {
			return e
		}
		p, e := s.store.LoadPlanning(tx, old.TaskId)
		if e != nil {
			return e
		}
		if t == nil || p == nil || !proto.Equal(p.VerificationRef.GetName(), old.Ref.Name) {
			return nil
		}
		v, e := s.QueryVerification(tx, c, p.VerificationRef)
		if e != nil {
			return e
		}
		if v.Status != "REJECTED" || v.ContinuationRequestRef != nil {
			return nil
		}
		before := proto.Clone(t).(*v1.Task)
		if e = s.requestRejectedContinuation(tx, c, t, p, v); e != nil {
			return e
		}
		if v.ContinuationRequestRef != nil {
			v.Ref.Revision++
			p.VerificationRef = v.Ref
			if e = s.saveVerification(tx, v); e != nil {
				return e
			}
			if e = s.store.SavePlanning(tx, p); e != nil {
				return e
			}
		}
		if !proto.Equal(before, t) {
			if t.Revision == before.Revision {
				t.Revision++
			}
			return s.saveTask(tx, t)
		}
		return nil
	})
}
