package tasks

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) pendingClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureKind) (bool, error) {
	jobs, e := s.closureJobs.Pending(ctx, c)
	if e != nil {
		return false, e
	}
	for _, j := range jobs {
		if j.Module == "tasks" && j.JobType == kind.spec().jobType {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) processClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureKind) error {
	if c.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	pending, e := s.pendingClosureDeliveries(ctx, c, kind)
	if e != nil || !pending {
		return e
	}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	for {
		id := &v1.CommandIdentity{UserId: s.user, IssuerId: c.IssuerId, TargetDomainId: s.domain, CommandId: command.NewRef(s.user, s.domain, "command", "command").Name.LocalId}
		r, e := s.closureJobs.ExecuteJob(ctx, c, &v1.JobCommand{Identity: id, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{kind.spec().jobType}, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			return nil
		}
		if e = s.ProcessClosureClaim(ctx, r.Jobs[0]); e != nil {
			return e
		}
	}
}

// recoverClosureDeliveries 等待自然租约到期，不持有事务；业务重读仍归各类型流程。
func (s *Service) recoverClosureDeliveries(ctx context.Context, c *v1.Caller, kind closureKind) error {
	for {
		if e := kind.process(ctx, s, c); e != nil {
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
