package tasks

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type outcomeStore interface {
	SaveProposalOutcome(context.Context, *v1.ProposalOutcome) error
	LoadProposalOutcome(context.Context, *v1.Ref) (*v1.ProposalOutcome, error)
}

// SubmitProposalOutcome 回报接纳与提议推进分别记账；旧工作者的真实回报不会覆盖当前提议。
func (s *Service) SubmitProposalOutcome(ctx context.Context, caller *v1.Caller, c *v1.SubmitProposalOutcomeCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.traceModelDecisions(c.RequestRef).Execute(ctx, caller, c.Header, command.SemanticFingerprint("proposal-outcome", c.RequestRef, c.Claim, c.Proposal, c.ErrorCode, c.ModelCallRef, c.OutputRef, c.UsageRef), "tasks.proposal_outcome", func(tx context.Context) (*v1.Ref, error) {
		if caller.GetIssuerId() != "host" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		r, e := s.QueryProposalRequest(tx, caller, c.RequestRef)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		snap, e := s.store.LoadSnapshot(tx, r.SnapshotRef)
		if e != nil {
			return nil, e
		}
		if c.Claim == nil || c.Claim.Ref == nil || !proto.Equal(c.Claim.Ref.Name, r.JobRef.Name) || c.Claim.ProcessInstance == "" || c.Claim.ClaimEpoch == 0 {
			return nil, command.Fail("INVALID_OUTCOME")
		}
		if c.Proposal != nil {
			q := c.Proposal
			if c.ErrorCode != "" || q.Ref != nil || !validProposalBody(q) || q.ReasonerRef.GetName().GetUserId() != s.user || q.ReasonerRef.GetName().GetObjectKind() != "reasoner" || q.ReasonerRef.GetRevision() == 0 || q.ReasonerRef.GetSchemaId() != "lerna.v1.Reasoner" || !proto.Equal(q.TaskId, r.TaskId) || !proto.Equal(q.RequestRef, r.Ref) || !proto.Equal(q.ContextSnapshotRef, snap.Ref) || q.RequirementsVersion != snap.RequirementsVersion || q.InputVersion != snap.InputVersion || q.ControlGeneration != snap.ControlGeneration || q.PlanningGeneration != snap.PlanningGeneration {
				return nil, command.Fail("INVALID_OUTCOME")
			}
		} else {
			switch c.ErrorCode {
			case "UNKNOWN", "REFUSED", "INCOMPLETE", "INVALID_OUTPUT", "PREPARATION_UNRECOVERABLE", "CONTEXT_TOO_LARGE", "INSUFFICIENT_CONTEXT", "DEPENDENCY_UNAVAILABLE":
			default:
				return nil, command.Fail("INVALID_OUTCOME")
			}
		}
		if c.ModelCallRef != nil {
			if command.CheckName(caller, c.ModelCallRef.Name, s.user, s.domain, "model-call") != nil {
				return nil, command.Fail("INVALID_OUTCOME")
			}
			call, e := s.store.(modelCallStore).LoadModelCallRef(tx, c.ModelCallRef)
			if e != nil {
				return nil, e
			}
			if call == nil || !proto.Equal(call.Ref, c.ModelCallRef) || !proto.Equal(call.RequestRef, r.Ref) || call.Result == nil || !proto.Equal(call.Result.OutputRef, c.OutputRef) || !proto.Equal(call.Result.UsageRef, c.UsageRef) {
				return nil, command.Fail("INVALID_OUTCOME")
			}
			if c.Proposal != nil && call.Result.Status != "COMPLETED" {
				return nil, command.Fail("INVALID_OUTCOME")
			}
		} else if c.Proposal != nil || c.OutputRef != nil || c.UsageRef != nil {
			return nil, command.Fail("INVALID_OUTCOME")
		}
		outcome := &v1.ProposalOutcome{Ref: command.NewRef(s.user, s.domain, "proposal-outcome", "lerna.v1.ProposalOutcome"), RequestRef: r.Ref, Claim: c.Claim, Proposal: c.Proposal, ErrorCode: c.ErrorCode, ModelCallRef: c.ModelCallRef, OutputRef: c.OutputRef, UsageRef: c.UsageRef, Identity: c.Header.Identity}
		eligible := s.currentModelClaim(tx, caller, r, c.Claim)
		if eligible == nil {
			eligible = s.currentModelRequest(tx, caller, r)
		}
		if eligible == nil && c.OutputRef != nil {
			eligible = s.content.CheckUsable(tx, caller, c.OutputRef)
		}
		if eligible != nil {
			var failure *command.Failure
			if !errors.As(eligible, &failure) {
				return nil, eligible
			}
			outcome.ReasonCode = failure.Detail.Code
		} else {
			p, e := s.store.LoadPlanning(tx, r.TaskId)
			if e != nil {
				return nil, e
			}
			if p.Proposal != nil {
				return nil, command.Fail("PROPOSAL_ALREADY_REPORTED")
			}
			if c.Proposal != nil {
				p.Proposal = proto.Clone(c.Proposal).(*v1.Proposal)
				p.Proposal.Ref = command.NewRef(s.user, s.domain, "proposal", "lerna.v1.Proposal")
				outcome.ProposalRef = p.Proposal.Ref
				if e = s.saveProposal(tx, p.Proposal); e != nil {
					return nil, e
				}
				if e = s.store.SavePlanning(tx, p); e != nil {
					return nil, e
				}
			}
			outcome.AcceptedForProgress = true
			r.State = "REPORTED"
			r.OutcomeIdentity = c.Header.Identity
			r.OutcomeReceipt = nil
			job, e := s.store.(modelStore).LoadJob(tx, r.JobRef.Name)
			if e != nil {
				return nil, e
			}
			job.State = "COMPLETED"
			job.Ref.Revision++
			if e = s.store.(modelStore).SaveJob(tx, job); e != nil {
				return nil, e
			}
		}
		r.OutcomeRefs = append(r.OutcomeRefs, outcome.Ref)
		r.OutcomeReceipt = nil
		if e = s.saveModelRequest(tx, r); e != nil {
			return nil, e
		}
		return outcome.Ref, s.saveModelOutcome(tx, outcome)
	})
}
func (s *Service) QueryProposalOutcome(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ProposalOutcome, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.ProposalOutcome" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "proposal-outcome"); e != nil {
		return nil, e
	}
	o, e := s.store.(outcomeStore).LoadProposalOutcome(ctx, r)
	if e == nil && o != nil && !proto.Equal(o.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return o, e
}

func (s *Service) finishScriptedRequest(ctx context.Context, r *v1.Ref) error {
	request, e := s.store.(modelStore).LoadProposalRequest(ctx, r)
	if e != nil || request == nil {
		return e
	}
	call, e := s.store.(modelCallStore).LoadModelCall(ctx, r, 0)
	if e != nil {
		return e
	}
	job, e := s.store.(modelStore).LoadJob(ctx, request.JobRef.Name)
	if e != nil {
		return e
	}
	if call != nil || job == nil || job.State != "READY" {
		return command.Fail("MODEL_OUTCOME_REQUIRED")
	}
	request.State = "REPORTED"
	job.State = "COMPLETED"
	job.Ref.Revision++
	if e = s.store.(modelStore).SaveJob(ctx, job); e != nil {
		return e
	}
	return s.saveModelRequest(ctx, request)
}
