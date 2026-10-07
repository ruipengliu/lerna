package tasks

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) closureConfirmationAdmission(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Admission, error) {
	if s.closureSource == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	w, e := s.closureSource.QueryClosureWork(ctx, c, r)
	if e != nil {
		return nil, e
	}
	task, e := s.QueryTask(ctx, c, w.TaskId)
	if e != nil {
		return nil, e
	}
	if task == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	cap, e := s.QueryCurrentCapability(ctx, c, w.CapabilityRef)
	if e != nil {
		return nil, e
	}
	if cap == nil || !proto.Equal(cap.Ref, w.CapabilityRef) {
		return nil, command.Fail("CAPABILITY_INVALID")
	}
	return &v1.Admission{TaskId: task.TaskId, Origin: w.Ref, GrantRefs: []*v1.Ref{w.GrantRef}, RequirementsVersion: task.RequirementsVersion, InputVersion: task.InputVersion, ControlGeneration: task.ControlGeneration, StepId: w.Ref.Name.LocalId, CapabilitySnapshot: cap, ParametersRef: w.ParametersRef, QuerySubject: w.QuerySubject, WorkCategory: "CLOSURE"}, nil
}

// RequestClosureConfirmation 发布已保存核心意图的精确事项，不创建目标提议。
func (s *Service) RequestClosureConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RequestClosureConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request-closure-confirmation", c.WorkRef, c.SessionId, c.ExpiresAtUnixMs), "tasks.closure_confirmation", func(tx context.Context) (*v1.Ref, error) {
		a, e := s.closureConfirmationAdmission(tx, caller, c.WorkRef)
		if e != nil {
			return nil, e
		}
		if e = s.content.CheckUsable(tx, caller, a.ParametersRef); e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		expires := c.ExpiresAtUnixMs
		if expires == 0 {
			expires = now + 300000
		}
		if expires <= now || expires > now+86400000 {
			return nil, command.Fail("INVALID_INPUT")
		}
		description, e := json.Marshal(struct {
			Matter                                                  string
			Owner                                                   *v1.Ref
			QuerySubject                                            *v1.QuerySubject
			Action, Resource, Endpoint, UseRight, ProcessingPurpose string
			ParametersRef, GrantRef                                 *v1.Ref
			Unit                                                    string
			FeeCeiling                                              *int64
		}{"OPERATION_ADMISSION", a.Origin, a.QuerySubject, a.CapabilitySnapshot.Action, a.CapabilitySnapshot.Resource, a.CapabilitySnapshot.ExecutorEndpointId, a.CapabilitySnapshot.UseRight, a.CapabilitySnapshot.ProcessingPurpose, a.ParametersRef, a.GrantRefs[0], a.CapabilitySnapshot.Unit, a.CapabilitySnapshot.FeeCeiling})
		if e != nil {
			return nil, e
		}
		confirmation, e := s.confirmationPublisher.CreateConfirmationInTransaction(tx, &v1.Confirmation{SessionId: c.SessionId, MatterType: "OPERATION_ADMISSION", Description: string(description), ExpiresAtUnixMs: expires, Matter: &v1.Confirmation_OperationAdmission{OperationAdmission: command.OperationMatter(a)}})
		if e != nil {
			return nil, e
		}
		return confirmation.Ref, nil
	})
}

// ValidateClosureConfirmation 不消费批准；P1 仍在自身事务重查并原子消费。
func (s *Service) ValidateClosureConfirmation(ctx context.Context, c *v1.Caller, cmd *v1.AdmitClosureCommand, required bool) error {
	a, e := s.closureConfirmationAdmission(ctx, c, cmd.WorkRef)
	if e != nil {
		return e
	}
	return s.confirmations.CheckAdmissionConfirmation(ctx, cmd.ConfirmationRef, a, required)
}
func (s *Service) checkClosureConfirmationMatter(ctx context.Context, c *v1.Confirmation) error {
	m := c.GetOperationAdmission()
	a, e := s.closureConfirmationAdmission(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, m.ProposalRef)
	if e != nil {
		return e
	}
	if !proto.Equal(m, command.OperationMatter(a)) {
		return command.Fail("CONFIRMATION_INVALID")
	}
	return s.content.CheckUsable(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, a.ParametersRef)
}
