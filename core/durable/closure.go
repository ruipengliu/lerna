package durable

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// closurePurposes 只登记任务编排三种固定封闭交付；其他工作类型不能借这条路径领取或完成。
var closurePurposes = map[string]string{
	"DELIVER_COMPLETION_CLOSURE":   "completion-closure:",
	"DELIVER_CANCELLATION_CLOSURE": "cancellation-closure:",
	"DELIVER_TASK_CLOSURE":         "task-closure:",
}

// EnqueueClosureInTransaction 与源域范围同事务保存封闭责任。
func (s *Service) EnqueueClosureInTransaction(ctx context.Context, jobType string, intentRef *v1.Ref, responsibility *v1.CommandIdentity, executorEndpointID, ledgerDomainID string) (*v1.Ref, error) {
	purpose, ok := closurePurposes[jobType]
	if !ok {
		return nil, command.Fail("INVALID_JOB")
	}
	ref := command.NewRef(s.user, s.domain, "job", "lerna.v1.Job")
	j := &v1.Job{Ref: ref, Module: "tasks", JobType: jobType, ContractVersion: 1, Responsibility: responsibility, State: "READY", PurposeKey: purpose + intentRef.Name.LocalId, SpecificationRef: intentRef, ExecutorEndpointId: executorEndpointID, LedgerDomainId: ledgerDomainID}
	return ref, s.store.SaveJob(ctx, j)
}

// closureClaim 核验保存的领取，再要求调用方副本与保存记录的固定字段一致。
func (s *Service) closureClaim(ctx context.Context, j *v1.Job) (*v1.Job, error) {
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
	if _, ok := closurePurposes[current.JobType]; current.Module != "tasks" || !ok {
		return nil, command.Fail("INVALID_JOB")
	}
	if current.ContractVersion != 1 || j.ContractVersion != 1 || len(j.ProtoReflect().GetUnknown()) != 0 || j.Module != current.Module || j.JobType != current.JobType || !proto.Equal(j.SpecificationRef, current.SpecificationRef) || !proto.Equal(j.Responsibility, current.Responsibility) || j.ExecutorEndpointId != current.ExecutorEndpointId || j.LedgerDomainId != current.LedgerDomainId {
		return nil, command.Fail("INVALID_JOB")
	}
	return current, nil
}

// ReadClosureClaim 返回保存的工作；调用方按保存的类型分派，不信任副本。
func (s *Service) ReadClosureClaim(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	var current *v1.Job
	e := s.store.Transaction(ctx, "durable.jobs", func(tx context.Context) error { var e error; current, e = s.closureClaim(tx, j); return e })
	return current, e
}

// CompleteClosureInTransaction 在源确认事务内再次核验原领取。
func (s *Service) CompleteClosureInTransaction(ctx context.Context, j *v1.Job) error {
	current, e := s.closureClaim(ctx, j)
	if e != nil {
		return e
	}
	current.State = "COMPLETED"
	current.Ref.Revision++
	return s.store.SaveJob(ctx, current)
}
