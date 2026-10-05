package durable

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// CheckExecutionClaimInTransaction 使用锁后时间和原工作记录，源域领取不能授予执行域权限。
func (s *Service) CheckExecutionClaimInTransaction(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	if j == nil || j.Ref == nil || command.CheckName(&v1.Caller{UserId: s.user, IssuerId: "egress"}, j.Ref.Name, s.user, s.domain, "job") != nil {
		return nil, command.Fail("STALE_CLAIM")
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return nil, e
	}
	return s.CheckExecutionClaimAt(ctx, j, now)
}

// CheckExecutionClaimAt 在已经持有裁决锁的开始门禁中只读，使用该排序点的权威时间。
func (s *Service) CheckExecutionClaimAt(ctx context.Context, j *v1.Job, now int64) (*v1.Job, error) {
	if j == nil || j.Ref == nil || command.CheckName(&v1.Caller{UserId: s.user, IssuerId: "egress"}, j.Ref.Name, s.user, s.domain, "job") != nil {
		return nil, command.Fail("STALE_CLAIM")
	}
	current, e := s.store.LoadJob(ctx, j.Ref.Name)
	if e != nil {
		return nil, e
	}
	if e = validClaim(current, j.ProcessInstance, j.ClaimEpoch, j.Ref.Revision, now); e != nil {
		return nil, e
	}
	if current.Module != "ledger" || current.JobType != "EXECUTE_OPERATION" {
		return nil, command.Fail("INVALID_JOB")
	}
	return current, nil
}
func (s *Service) CompleteExecutionInTransaction(ctx context.Context, j *v1.Job) error {
	current, e := s.CheckExecutionClaimInTransaction(ctx, j)
	if e != nil {
		return e
	}
	current.State = "COMPLETED"
	current.Ref.Revision++
	return s.store.SaveJob(ctx, current)
}
