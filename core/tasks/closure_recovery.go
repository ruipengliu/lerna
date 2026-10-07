package tasks

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type closureWork interface {
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}

func (s *Service) closureJobs(kind closureDeliveryKind) (closureWork, string, error) {
	switch kind {
	case completionClosureDelivery:
		return s.completionJobs, "DELIVER_COMPLETION_CLOSURE", nil
	case cancellationClosureDelivery:
		return s.cancellationJobs, "DELIVER_CANCELLATION_CLOSURE", nil
	default:
		return nil, "", command.Fail("INVALID_JOB")
	}
}

func (s *Service) pendingClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureDeliveryKind) (bool, error) {
	work, jobType, e := s.closureJobs(kind)
	if e != nil {
		return false, e
	}
	jobs, e := work.Pending(ctx, c)
	if e != nil {
		return false, e
	}
	for _, j := range jobs {
		if j.Module == "tasks" && j.JobType == jobType {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) processClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureDeliveryKind) error {
	if c.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	pending, e := s.pendingClosureDeliveries(ctx, c, kind)
	if e != nil || !pending {
		return e
	}
	work, jobType, e := s.closureJobs(kind)
	if e != nil {
		return e
	}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	for {
		id := &v1.CommandIdentity{UserId: s.user, IssuerId: c.IssuerId, TargetDomainId: s.domain, CommandId: command.NewRef(s.user, s.domain, "command", "command").Name.LocalId}
		r, e := work.ExecuteJob(ctx, c, &v1.JobCommand{Identity: id, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{jobType}, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			return nil
		}
		if e = s.processClosureClaim(ctx, r.Jobs[0], kind); e != nil {
			return e
		}
	}
}

// recoverClosureDeliveries 等待自然租约到期，不持有事务；业务重读仍归各类型流程。
func (s *Service) recoverClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureDeliveryKind) error {
	for {
		var e error
		switch kind {
		case completionClosureDelivery:
			e = s.ProcessCompletions(ctx, c)
		case cancellationClosureDelivery:
			e = s.ProcessCancellations(ctx, c)
		default:
			return command.Fail("INVALID_JOB")
		}
		if e != nil {
			return e
		}
		pending, e := s.pendingClosureDeliveries(ctx, c, kind)
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
