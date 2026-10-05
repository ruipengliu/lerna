package durable

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// CheckReconciliationClaimInTransaction 只授予核对调度推进，不授予事实接纳或物理发送。
func (s *Service) CheckReconciliationClaimInTransaction(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	if j == nil || j.Ref == nil || command.CheckName(&v1.Caller{UserId: s.user, IssuerId: "host"}, j.Ref.Name, s.user, s.domain, "job") != nil {
		return nil, command.Fail("STALE_CLAIM")
	}
	current, e := s.store.LoadJob(ctx, j.Ref.Name)
	if e != nil {
		return nil, e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return nil, e
	}
	if e = validClaim(current, j.ProcessInstance, j.ClaimEpoch, j.Ref.Revision, now); e != nil {
		return nil, e
	}
	if current.Module != "ledger" || current.JobType != "RECONCILE_OPERATION" {
		return nil, command.Fail("INVALID_JOB")
	}
	return current, nil
}
