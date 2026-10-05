package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Decisions interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}

func (s *Service) WithDecisions(d Decisions) *Service { s.decisions = d; return s }
func (s *Service) QueryPlanning(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.PlanningState, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.LoadPlanning(ctx, id)
}
func (s *Service) AcceptRequirements(ctx context.Context, caller *v1.Caller, c *v1.AcceptRequirementsCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("requirements", c.TaskRef, c.InputVersion, c.Conditions, c.Source), "tasks.planning", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" || c.Source != "TRUSTED_TEMPLATE" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.TaskRef == nil || c.TaskRef.SchemaId != "lerna.v1.Task" {
			return nil, command.Fail("INVALID_INPUT")
		}
		t, e := s.QueryTask(tx, caller, c.TaskRef.Name)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if t.Revision != c.TaskRef.Revision || c.InputVersion > t.InputVersion || c.InputVersion == 0 || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
			return nil, command.Fail("STALE_INPUT")
		}
		if t.BoundInputVersion < t.InputVersion && c.InputVersion != t.BoundInputVersion+1 {
			return nil, command.Fail("INPUT_ORDER")
		}
		if c.InputVersion < t.BoundInputVersion {
			return nil, command.Fail("STALE_INPUT")
		}
		if len(c.Conditions) == 0 {
			return nil, command.Fail("INVALID_REQUIREMENTS")
		}
		seen := map[string]bool{}
		for _, condition := range c.Conditions {
			if condition == nil || condition.ConditionId == "" || seen[condition.ConditionId] || condition.DescriptionRef == nil || condition.RuleVersion != 1 || (condition.VerificationRule != "TARGET_RECORD" && condition.VerificationRule != "USER_EVALUATION") {
				return nil, command.Fail("INVALID_REQUIREMENTS")
			}
			if e := s.content.CheckUsable(tx, caller, condition.DescriptionRef); e != nil {
				return nil, e
			}
			seen[condition.ConditionId] = true
		}
		p, e := s.store.LoadPlanning(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		boundVersion := c.InputVersion
		if c.InputVersion > t.BoundInputVersion {
			history, e := s.store.(InputStore).LoadTaskInputs(tx, t.TaskId)
			if e != nil {
				return nil, e
			}
			found := false
			for _, input := range history.Inputs {
				if input.InputVersion == c.InputVersion {
					input.ProcessingStatus = "PROCESSED"
					input.ProcessingDecision = c.Header.Identity
					found = true
				}
			}
			for _, input := range history.Inputs {
				if input.InputVersion == boundVersion+1 && input.ProcessingStatus == "PROCESSED" {
					boundVersion++
				}
			}
			if !found {
				return nil, command.Fail("INPUT_ORDER")
			}
			if e = s.store.(InputStore).SaveTaskInputs(tx, history); e != nil {
				return nil, e
			}
		}
		if e = s.supersedeVerification(tx, t, p); e != nil {
			return nil, e
		}
		if p.Requirements != nil {
			t.RequirementsVersion++
		}
		t.ControlGeneration++
		t.Revision++
		t.RequirementsStatus = v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED
		t.BoundInputVersion = boundVersion
		if boundVersion < t.InputVersion {
			setInputWaiting(t, "INPUT_PROCESSING")
		} else {
			setInputWaiting(t, "")
		}
		ref := command.NewRef(s.user, s.domain, "requirements", "lerna.v1.Requirements")
		p.Requirements = &v1.Requirements{Ref: ref, TaskId: t.TaskId, RequirementsVersion: t.RequirementsVersion, BoundInputVersion: boundVersion, Source: c.Source, Conditions: c.Conditions, AcceptedBy: c.Header.Identity}
		if e = s.store.SaveTask(tx, t); e != nil {
			return nil, e
		}
		if e = s.store.SaveRequirements(tx, p.Requirements); e != nil {
			return nil, e
		}
		return ref, s.store.SavePlanning(tx, p)
	})
}
func (s *Service) RequestProposal(ctx context.Context, caller *v1.Caller, c *v1.RequestProposalCommand) (*v1.ContextSnapshot, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	r, e := s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request", c.TaskId), "tasks.planning", func(tx context.Context) (*v1.Ref, error) {
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		p, e := s.store.LoadPlanning(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		t.PlanningGeneration++
		t.Revision++
		p.Snapshot = &v1.ContextSnapshot{Ref: command.NewRef(s.user, s.domain, "snapshot", "lerna.v1.ContextSnapshot"), TaskRef: &v1.Ref{Name: t.TaskId, Revision: t.Revision, SchemaId: "lerna.v1.Task"}, RequirementsVersion: t.RequirementsVersion, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, PlanningGeneration: t.PlanningGeneration, RequestRef: command.NewRef(s.user, s.domain, "proposal-request", "lerna.v1.ContextSnapshot"), ExpiresAtUnixMs: now + 300000, ContentRefs: []*v1.Ref{t.GoalRef}}
		inputs, e := s.store.(InputStore).LoadTaskInputs(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		for _, input := range inputs.Inputs {
			if input.InputVersion > t.InputVersion {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			p.Snapshot.InputRefs = append(p.Snapshot.InputRefs, input.InputRef)
			if input.InputVersion > 1 {
				p.Snapshot.ContentRefs = append(p.Snapshot.ContentRefs, input.ContentRef)
			}
		}
		caps, e := s.store.CapabilityRefs(tx)
		if e != nil {
			return nil, e
		}
		p.Snapshot.CapabilityRefs = caps
		p.Snapshot.ProgressRefs = p.AdmissionRefs
		if p.Requirements != nil {
			p.Snapshot.RequirementsRef = p.Requirements.Ref
		}
		p.Proposal = nil
		p.ProposalConsumed = false
		if e = s.store.SaveTask(tx, t); e != nil {
			return nil, e
		}
		if e = s.store.SaveSnapshot(tx, p.Snapshot); e != nil {
			return nil, e
		}
		return p.Snapshot.Ref, s.store.SavePlanning(tx, p)
	})
	if e != nil {
		return nil, e
	}
	if r.Error != nil {
		return nil, &command.Failure{Detail: r.Error}
	}
	return s.store.LoadSnapshot(ctx, r.ResultRef)
}
func (s *Service) ReceiveProposal(ctx context.Context, caller *v1.Caller, c *v1.ReceiveProposalCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("proposal", c.Proposal), "tasks.planning", func(tx context.Context) (*v1.Ref, error) {
		q := c.Proposal
		if q == nil || q.ReasonerRef == nil || q.ReasonerRef.Name == nil || q.ReasonerRef.Name.UserId != s.user || q.ReasonerRef.Name.ObjectKind != "reasoner" || q.ReasonerRef.Revision == 0 || q.ReasonerRef.SchemaId != "lerna.v1.Reasoner" || q.Ref != nil || q.Step == nil || q.Step.StepId == "" || q.Kind != "ACTION" {
			return nil, command.Fail("INVALID_PROPOSAL")
		}
		if e := command.CheckName(caller, q.TaskId, s.user, s.domain, "task"); e != nil {
			return nil, e
		}
		p, e := s.store.LoadPlanning(tx, q.TaskId)
		if e != nil {
			return nil, e
		}
		if p.Snapshot == nil || p.Proposal != nil || !proto.Equal(q.ContextSnapshotRef, p.Snapshot.Ref) || !proto.Equal(q.RequestRef, p.Snapshot.RequestRef) {
			return nil, command.Fail("STALE_PROPOSAL")
		}
		p.Proposal = proto.Clone(q).(*v1.Proposal)
		p.Proposal.Ref = command.NewRef(s.user, s.domain, "proposal", "lerna.v1.Proposal")
		if e = s.store.SaveProposal(tx, p.Proposal); e != nil {
			return nil, e
		}
		return p.Proposal.Ref, s.store.SavePlanning(tx, p)
	})
}

func (s *Service) QueryProposal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Proposal, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "proposal"); e != nil {
		return nil, e
	}
	p, e := s.store.LoadProposal(ctx, r)
	if e == nil && p != nil && !proto.Equal(p.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return p, e
}
func (s *Service) QueryRequirements(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Requirements, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "requirements"); e != nil {
		return nil, e
	}
	p, e := s.store.LoadRequirements(ctx, r)
	if e == nil && p != nil && !proto.Equal(p.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return p, e
}
func (s *Service) QuerySnapshot(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ContextSnapshot, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "snapshot"); e != nil {
		return nil, e
	}
	p, e := s.store.LoadSnapshot(ctx, r)
	if e == nil && p != nil && !proto.Equal(p.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return p, e
}
