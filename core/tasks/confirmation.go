package tasks

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ConfirmationPublisher interface {
	CreateConfirmationInTransaction(context.Context, *v1.Confirmation) (*v1.Confirmation, error)
}
type ConfirmationContent interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
}

func (s *Service) WithConfirmationRequests(p ConfirmationPublisher, c ConfirmationContent) *Service {
	s.confirmationPublisher = p
	s.confirmationContent = c
	return s
}
func (s *Service) RequestAdmissionConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RequestAdmissionConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request-admission-confirmation", c.TaskId, c.ProposalRef, c.GrantRef, c.SessionId), "tasks.confirmation", func(tx context.Context) (*v1.Ref, error) {
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		p, e := s.store.LoadPlanning(tx, c.TaskId)
		if e != nil {
			return nil, e
		}
		if p == nil || p.Proposal == nil || p.Snapshot == nil || p.Proposal.Step == nil || !proto.Equal(p.Proposal.Ref, c.ProposalRef) {
			return nil, command.Fail("STALE_PROPOSAL")
		}
		cap, e := s.store.LoadCapability(tx, p.Proposal.Step.CapabilityRef)
		if e != nil {
			return nil, e
		}
		if cap == nil {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		step := p.Proposal.Step
		a := &v1.Admission{TaskId: t.TaskId, Origin: p.Proposal.Ref, GrantRefs: []*v1.Ref{c.GrantRef}, RequirementsVersion: p.Proposal.RequirementsVersion, InputVersion: p.Proposal.InputVersion, ControlGeneration: p.Proposal.ControlGeneration, StepId: step.StepId, CapabilitySnapshot: cap, ParametersRef: step.ParametersRef, ContentRefs: step.ContentRefs}
		if e = s.content.CheckUsable(tx, caller, step.ParametersRef); e != nil {
			return nil, e
		}
		body, e := s.confirmationContent.Read(tx, caller, step.ParametersRef)
		if e != nil {
			return nil, e
		}
		if body == nil {
			return nil, command.Fail("CONTENT_UNUSABLE")
		}
		description, e := json.Marshal(struct {
			Matter            string
			Action            string
			Resource          string
			Endpoint          string
			UseRight          string
			ProcessingPurpose string
			Parameters        command.ParameterDescription
			ParametersRef     *v1.Ref
			ContentRefs       []*v1.Ref
			Unit              string
			FeeCeiling        *int64
		}{"OPERATION_ADMISSION", cap.Action, cap.Resource, cap.ExecutorEndpointId, cap.UseRight, cap.ProcessingPurpose, command.DescribeParameters(body), step.ParametersRef, step.ContentRefs, cap.Unit, cap.FeeCeiling})
		if e != nil {
			return nil, e
		}
		confirmation, e := s.confirmationPublisher.CreateConfirmationInTransaction(tx, &v1.Confirmation{SessionId: c.SessionId, MatterType: "OPERATION_ADMISSION", Description: string(description), ExpiresAtUnixMs: p.Snapshot.ExpiresAtUnixMs, Matter: &v1.Confirmation_OperationAdmission{OperationAdmission: command.OperationMatter(a)}})
		if e != nil {
			return nil, e
		}
		return confirmation.Ref, nil
	})
}

// CheckConfirmationMatter 在回应时核对当前任务和提议，批准不递增任何任务版本。
func (s *Service) CheckConfirmationMatter(ctx context.Context, c *v1.Confirmation) error {
	m := c.GetOperationAdmission()
	if c.MatterType != "OPERATION_ADMISSION" || m == nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	if m.ProposalRef.GetSchemaId() == "lerna.v1.ClosureWorkRequest" {
		return s.checkClosureConfirmationMatter(ctx, c)
	}
	caller := &v1.Caller{UserId: s.user, IssuerId: "host"}
	t, e := s.QueryTask(ctx, caller, m.TaskId)
	if e != nil {
		return e
	}
	if t == nil || t.RequirementsVersion != m.RequirementsVersion || t.InputVersion != m.InputVersion || t.ControlGeneration != m.ControlGeneration || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
		return command.Fail("CONFIRMATION_INVALID")
	}
	p, e := s.store.LoadPlanning(ctx, m.TaskId)
	if e != nil {
		return e
	}
	if p == nil || p.Proposal == nil || p.Snapshot == nil || p.Proposal.Step == nil || p.ProposalConsumed || !proto.Equal(p.Proposal.Ref, m.ProposalRef) || p.Proposal.PlanningGeneration != t.PlanningGeneration || p.VerificationFreeze != 0 {
		return command.Fail("CONFIRMATION_INVALID")
	}
	step := p.Proposal.Step
	if !proto.Equal(step.ParametersRef, m.ParametersRef) || !proto.Equal(step.CapabilityRef, m.Capability.GetRef()) || step.StepId != m.StepId || !proto.Equal(&v1.ActionStep{ContentRefs: step.ContentRefs}, &v1.ActionStep{ContentRefs: m.ContentRefs}) {
		return command.Fail("CONFIRMATION_INVALID")
	}
	return s.content.CheckUsable(ctx, caller, m.ParametersRef)
}
