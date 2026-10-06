package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) invalidatePlanning(ctx context.Context, t *v1.Task, p *v1.PlanningState) error {
	t.PlanningGeneration++
	p.Snapshot = nil
	p.Proposal = nil
	p.ProposalConsumed = false
	return s.supersedeVerification(ctx, t, p)
}

// supersedeVerification 与输入、控制或条件接纳共用原事务；只释放本轮的冻结。
func (s *Service) supersedeVerification(ctx context.Context, t *v1.Task, p *v1.PlanningState) error {
	if p.VerificationFreeze == 0 {
		return nil
	}
	if p.VerificationRef == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	v, e := s.store.(completionStore).LoadVerification(ctx, p.VerificationRef)
	if e != nil {
		return e
	}
	if v == nil || v.Status != "VERIFYING" || v.Round != p.VerificationFreeze {
		return command.Fail("INVARIANT_VIOLATION")
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	v.Status = "SUPERSEDED"
	v.EndedReason = "TASK_BASIS_CHANGED"
	v.FinishedAtUnixMs = now
	v.Ref.Revision++
	if e = s.saveVerification(ctx, v); e != nil {
		return e
	}
	p.VerificationRef = v.Ref
	p.SupersededVerificationGenerations = append(p.SupersededVerificationGenerations, p.VerificationFreeze)
	p.VerificationFreeze = 0
	setCompletionWaiting(t, nil)
	return nil
}
