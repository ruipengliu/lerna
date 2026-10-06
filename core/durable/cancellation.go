package durable

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// EnqueueCancellationClosureInTransaction 与取消控制同事务保存封闭责任。
func (s *Service) EnqueueCancellationClosureInTransaction(ctx context.Context, intent *v1.CancellationClosureIntent) (*v1.Ref, error) {
	ref := command.NewRef(s.user, s.domain, "job", "lerna.v1.Job")
	c := intent.Command
	j := &v1.Job{Ref: ref, Module: "tasks", JobType: "DELIVER_CANCELLATION_CLOSURE", ContractVersion: 1, Responsibility: c.Header.Identity, State: "READY", PurposeKey: "cancellation-closure:" + intent.Ref.Name.LocalId, SpecificationRef: intent.Ref, ExecutorEndpointId: c.ExecutorEndpointId, LedgerDomainId: c.OperationId.AuthorityDomainId}
	return ref, s.store.SaveJob(ctx, j)
}
func (s *Service) cancellationClaim(ctx context.Context, j *v1.Job) (*v1.Job, error) {
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
	if current.Module != "tasks" || current.JobType != "DELIVER_CANCELLATION_CLOSURE" {
		return nil, command.Fail("INVALID_JOB")
	}
	return current, nil
}
func (s *Service) ReadCancellationClosureClaim(ctx context.Context, j *v1.Job) (*v1.Job, error) {
	var current *v1.Job
	e := s.store.Transaction(ctx, "durable.jobs", func(tx context.Context) error { var e error; current, e = s.cancellationClaim(tx, j); return e })
	return current, e
}
func (s *Service) CompleteCancellationClosureInTransaction(ctx context.Context, j *v1.Job) error {
	current, e := s.cancellationClaim(ctx, j)
	if e != nil {
		return e
	}
	current.State = "COMPLETED"
	current.Ref.Revision++
	return s.store.SaveJob(ctx, current)
}
