package tasks

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) invalidatePlanning(ctx context.Context, t *v1.Task, p *v1.PlanningState) error {
	t.PlanningGeneration++
	p.Snapshot = nil
	p.Proposal = nil
	p.ProposalConsumed = false
	return s.supersedeVerification(ctx, t, p)
}

// supersedeVerification 是输入、控制和条件接纳共同使用的原子替代入口。
// 工单 07 建立核验记录后，必须在这里使用原 ctx 同时把当前记录写为 SUPERSEDED；
// 不得只清除冻结后异步修正。动作清单和已开始未收尾的阻断记录必须始终保留。
func (s *Service) supersedeVerification(_ context.Context, _ *v1.Task, p *v1.PlanningState) error {
	if p.VerificationFreeze != 0 {
		p.SupersededVerificationGenerations = append(p.SupersededVerificationGenerations, p.VerificationFreeze)
		p.VerificationFreeze = 0
	}
	return nil
}
