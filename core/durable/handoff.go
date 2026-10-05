package durable

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// EnqueueHandoffInTransaction 固定任务责任及目的；不接受可执行回调或任意工作类型。
func (s *Service) EnqueueHandoffInTransaction(ctx context.Context, a *v1.Admission) (*v1.Ref, error) {
	ref := command.NewRef(s.user, s.domain, "job", "lerna.v1.Job")
	j := &v1.Job{Ref: ref, Module: "tasks", JobType: "DELIVER_HANDOFF", ContractVersion: 1, Responsibility: a.HandoffIdentity, State: "READY", PurposeKey: "handoff:" + a.OperationId.LocalId, SpecificationRef: a.Ref, ExecutorEndpointId: a.ExecutorEndpointId, LedgerDomainId: a.LedgerDomainId}
	return ref, s.store.SaveJob(ctx, j)
}

// CheckClaimInTransaction 在源域最终记账时重新检查围栏；跨域接纳不能使用源域租约授权。
func (s *Service) CheckClaimInTransaction(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	if j == nil || j.Ref == nil || j.Ref.Name == nil {
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
	if current.Module != "tasks" || current.JobType != "DELIVER_HANDOFF" {
		return nil, command.Fail("INVALID_JOB")
	}
	return current, nil
}
func (s *Service) CompleteHandoffInTransaction(ctx context.Context, j *v1.Job) error {
	current, e := s.CheckClaimInTransaction(ctx, j)
	if e != nil {
		return e
	}
	current.State = "COMPLETED"
	current.Ref.Revision++
	return s.store.SaveJob(ctx, current)
}

// ReadHandoffClaim 固定读取已领取的源记录；不得信任调用者提供的载荷。
func (s *Service) ReadHandoffClaim(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	var current *v1.Job
	e := s.store.Transaction(ctx, "durable.jobs", func(tx context.Context) error { var e error; current, e = s.CheckClaimInTransaction(tx, j); return e })
	if e != nil {
		return nil, e
	}
	return current, nil
}
