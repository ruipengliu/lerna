package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) modelConfirmationAdmission(ctx context.Context, caller *v1.Caller, ref, grant *v1.Ref) (*v1.Admission, *v1.ContextSnapshot, error) {
	if ref == nil || command.CheckName(caller, ref.Name, s.user, s.domain, "model-call") != nil {
		return nil, nil, command.Fail("INVALID_REFERENCE")
	}
	call, e := s.store.(modelCallStore).LoadModelCallRef(ctx, ref)
	if e != nil {
		return nil, nil, e
	}
	if call == nil || !proto.Equal(call.Ref, ref) || call.State != "SEALED" || call.AdmissionRef != nil {
		return nil, nil, command.Fail("CONFIRMATION_INVALID")
	}
	request, e := s.QueryProposalRequest(ctx, caller, call.RequestRef)
	if e != nil {
		return nil, nil, e
	}
	if request == nil {
		return nil, nil, command.Fail("CONFIRMATION_INVALID")
	}
	if e = s.currentModelRequest(ctx, caller, request); e != nil {
		return nil, nil, e
	}
	if e = s.checkModelInput(ctx, caller, call); e != nil {
		return nil, nil, e
	}
	snap, e := s.store.LoadSnapshot(ctx, request.SnapshotRef)
	if e != nil {
		return nil, nil, e
	}
	cap, e := s.QueryCurrentCapability(ctx, caller, call.CapabilityRef)
	if e != nil {
		return nil, nil, e
	}
	if cap == nil || !proto.Equal(cap.Ref, call.CapabilityRef) {
		return nil, nil, command.Fail("CAPABILITY_INVALID")
	}
	return &v1.Admission{TaskId: request.TaskId, Origin: call.Ref, GrantRefs: []*v1.Ref{grant}, RequirementsVersion: snap.RequirementsVersion, InputVersion: snap.InputVersion, ControlGeneration: snap.ControlGeneration, StepId: fmt.Sprintf("model:%d", call.Position), CapabilitySnapshot: cap, ParametersRef: call.InputRef, ContentRefs: call.InputRefs}, snap, nil
}

func (s *Service) requestModelConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RequestAdmissionConfirmationCommand) (*v1.CommandReceipt, error) {
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request-model-confirmation", c.TaskId, c.ProposalRef, c.GrantRef, c.SessionId), "tasks.confirmation", func(tx context.Context) (*v1.Ref, error) {
		a, snap, e := s.modelConfirmationAdmission(tx, caller, c.ProposalRef, c.GrantRef)
		if e != nil {
			return nil, e
		}
		if !proto.Equal(a.TaskId, c.TaskId) {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		body, e := s.modelContent.Read(tx, caller, a.ParametersRef)
		if e != nil {
			return nil, e
		}
		description, e := json.Marshal(struct {
			Matter        string
			Capability    *v1.Capability
			Parameters    command.ParameterDescription
			ParametersRef *v1.Ref
			ContentRefs   []*v1.Ref
		}{"MODEL_ADMISSION", a.CapabilitySnapshot, command.DescribeParameters(body), a.ParametersRef, a.ContentRefs})
		if e != nil {
			return nil, e
		}
		confirmation, e := s.confirmationPublisher.CreateConfirmationInTransaction(tx, &v1.Confirmation{SessionId: c.SessionId, MatterType: "OPERATION_ADMISSION", Description: string(description), ExpiresAtUnixMs: snap.ExpiresAtUnixMs, Matter: &v1.Confirmation_OperationAdmission{OperationAdmission: command.OperationMatter(a)}})
		if e != nil {
			return nil, e
		}
		return confirmation.Ref, nil
	})
}
func (s *Service) checkModelConfirmationMatter(ctx context.Context, c *v1.Confirmation) error {
	m := c.GetOperationAdmission()
	if m == nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	a, _, e := s.modelConfirmationAdmission(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, m.ProposalRef, m.GrantRef)
	if e != nil {
		return e
	}
	if !proto.Equal(command.OperationMatter(a), m) {
		return command.Fail("CONFIRMATION_INVALID")
	}
	return nil
}
