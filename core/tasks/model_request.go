package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type modelStore interface {
	ReadAuthorityTime(context.Context) (int64, error)
	SaveProposalRequest(context.Context, *v1.ProposalRequest) error
	LoadProposalRequest(context.Context, *v1.Ref) (*v1.ProposalRequest, error)
	SaveJob(context.Context, *v1.Job) error
	LoadJob(context.Context, *v1.GlobalName) (*v1.Job, error)
}

func (s *Service) saveProposalRequest(ctx context.Context, snap *v1.ContextSnapshot, t *v1.Task, id *v1.CommandIdentity) error {
	purpose := "PLAN"
	if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
		purpose = "INTERPRET_INPUT"
	}
	r := &v1.ProposalRequest{Ref: snap.RequestRef, TaskId: t.TaskId, SnapshotRef: snap.Ref, JobRef: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job"), Purpose: purpose, MaxCallPositions: 1, MaxPhysicalSends: 1, State: "PENDING"}
	store := s.store.(modelStore)
	if e := s.saveModelRequest(ctx, r); e != nil {
		return e
	}
	return store.SaveJob(ctx, &v1.Job{Ref: r.JobRef, Module: "tasks", JobType: "PROPOSE", ContractVersion: 1, Responsibility: id, State: "READY", PurposeKey: "propose:" + r.Ref.Name.LocalId, SpecificationRef: r.Ref})
}

// QueryProposalRequest 按原请求查询持久责任，不替换已失效请求的快照。
func (s *Service) QueryProposalRequest(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ProposalRequest, error) {
	if r == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "proposal-request"); e != nil {
		return nil, e
	}
	p, e := s.store.(modelStore).LoadProposalRequest(ctx, r)
	if e == nil && p != nil && !proto.Equal(p.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	if e == nil && p != nil && p.OutcomeIdentity != nil {
		q, err := s.decisions.(interface {
			QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
		}).QueryReceipt(ctx, &v1.Caller{UserId: s.user, IssuerId: p.OutcomeIdentity.IssuerId}, p.OutcomeIdentity)
		if err != nil {
			return nil, err
		}
		p.OutcomeReceipt = q.Receipt
	}
	return p, e
}

func (s *Service) supersedeProposalRequest(ctx context.Context, snap *v1.ContextSnapshot) error {
	if snap == nil {
		return nil
	}
	r, e := s.store.(modelStore).LoadProposalRequest(ctx, snap.RequestRef)
	if e != nil || r == nil {
		return e
	}
	if r.State == "REPORTED" || r.State == "SUPERSEDED" || r.State == "STOPPED" {
		return nil
	}
	r.State = "SUPERSEDED"
	r.OutcomeReceipt = nil
	job, e := s.store.(modelStore).LoadJob(ctx, r.JobRef.Name)
	if e != nil {
		return e
	}
	if job == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	job.State = "CLOSED"
	job.Ref.Revision++
	if e = s.store.(modelStore).SaveJob(ctx, job); e != nil {
		return e
	}
	return s.saveModelRequest(ctx, r)
}

// StopProposalRequest 只封闭提议推进，原模型动作和费用仍由原记录负责。
func (s *Service) StopProposalRequest(ctx context.Context, caller *v1.Caller, c *v1.StopProposalRequestCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.traceModelDecisions(c.RequestRef).Execute(ctx, caller, c.Header, command.SemanticFingerprint("stop-proposal-request", c.RequestRef), "tasks.proposal_stop", func(tx context.Context) (*v1.Ref, error) {
		if caller.GetIssuerId() != "host" && caller.GetIssuerId() != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		r, e := s.QueryProposalRequest(tx, caller, c.RequestRef)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if r.State == "STOPPED" || r.State == "REPORTED" || r.State == "SUPERSEDED" {
			return r.Ref, nil
		}
		job, e := s.store.(modelStore).LoadJob(tx, r.JobRef.Name)
		if e != nil {
			return nil, e
		}
		if job == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		job.State = "CLOSED"
		job.Ref.Revision++
		r.State = "STOPPED"
		r.OutcomeReceipt = nil
		if e = s.store.(modelStore).SaveJob(tx, job); e != nil {
			return nil, e
		}
		return r.Ref, s.saveModelRequest(tx, r)
	})
}
