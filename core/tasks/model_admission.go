package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// AdmitModelCall 在同一裁决事务内绑定逻辑调用位置、授权、费用预留与派发责任。
func (s *Service) AdmitModelCall(ctx context.Context, caller *v1.Caller, c *v1.AdmitModelCallCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.traceModelDecisions(c.RequestRef).Execute(ctx, caller, c.Header, command.SemanticFingerprint("model-admit", c.RequestRef, c.Position, c.DescriptorDigest, c.GrantRef, c.ConfirmationRef), "tasks.model_admit", func(tx context.Context) (*v1.Ref, error) {
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
		call, e := s.store.(modelCallStore).LoadModelCall(tx, r.Ref, c.Position)
		if e != nil {
			return nil, e
		}
		if call == nil || call.State == "PREPARING" {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		if call.DescriptorDigest != c.DescriptorDigest {
			return nil, command.Fail("MODEL_POSITION_CONFLICT")
		}
		if e = s.checkModelInput(tx, caller, call); e != nil {
			return nil, e
		}
		if call.AdmissionRef != nil {
			return call.AdmissionRef, nil
		}
		if e = s.currentModelClaim(tx, caller, r, c.Claim); e != nil {
			return nil, e
		}
		if e = s.currentModelRequest(tx, caller, r); e != nil {
			return nil, e
		}
		if c.Position >= r.MaxCallPositions || len(r.ModelOperationRefs) >= int(r.MaxCallPositions) {
			return nil, command.Fail("MODEL_CALL_LIMIT")
		}
		t, e := s.QueryTask(tx, caller, r.TaskId)
		if e != nil {
			return nil, e
		}
		p, e := s.store.LoadPlanning(tx, r.TaskId)
		if e != nil {
			return nil, e
		}
		category := "TARGET"
		if r.Purpose == "INTERPRET_INPUT" {
			category = "PREPARATION"
		} else if r.Purpose != "PLAN" {
			return nil, command.Fail("INVALID_MODEL_PURPOSE")
		}
		if category == "TARGET" && (t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion) {
			return nil, command.Fail("REQUIREMENTS_NOT_ACCEPTED")
		}
		operations := make([]*v1.Ref, 0, len(p.AdmissionRefs))
		for _, ref := range p.AdmissionRefs {
			a, e := s.store.LoadAdmission(tx, ref)
			if e != nil {
				return nil, e
			}
			if a == nil {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			operations = append(operations, &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"})
		}
		if len(operations) > 0 || len(p.RejectedVerificationOperations) > 0 {
			if s.execution == nil {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			blocked, e := s.execution.BlocksAdmission(tx, caller, operations, p.RejectedVerificationOperations)
			if e != nil {
				return nil, e
			}
			if blocked {
				return nil, command.Fail("BLOCKING_OPERATION")
			}
		}
		cap, e := s.QueryCurrentCapability(tx, caller, call.CapabilityRef)
		if e != nil {
			return nil, e
		}
		if cap == nil || !proto.Equal(cap.Ref, call.CapabilityRef) || cap.MaxSends != r.MaxPhysicalSends || cap.MaxSends != 1 {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		ref := command.NewRef(s.user, s.domain, "admission", "lerna.v1.Admission")
		op := command.NewRef(s.user, s.domain+"/ledger", "operation", "lerna.v1.Operation")
		a := &v1.Admission{Ref: ref, Origin: call.Ref, ModelDescriptorDigest: call.DescriptorDigest, StepId: fmt.Sprintf("model:%d", call.Position), TaskId: t.TaskId, RequirementsVersion: t.RequirementsVersion, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, OperationId: op.Name, LedgerDomainId: s.domain + "/ledger", ExecutorEndpointId: cap.ExecutorEndpointId, ParametersRef: call.InputRef, CapabilityRef: cap.Ref, CapabilitySnapshot: cap, WorkCategory: category, ContentRefs: call.InputRefs, HandoffIdentity: &v1.CommandIdentity{UserId: s.user, IssuerId: "tasks-handoff", TargetDomainId: s.domain + "/ledger", CommandId: op.Name.LocalId}}
		use, required, e := s.grants.OccupyInTransaction(tx, c.GrantRef, t.TaskId, op.Name, ref, cap, call.InputRef)
		if e != nil {
			return nil, e
		}
		a.GrantRefs = []*v1.Ref{use.GrantRef}
		a.GrantUseRef = use.Ref
		a.BudgetBasis, e = s.budget.ReserveInTransaction(tx, t.TaskId, op.Name, ref, cap)
		if e != nil {
			return nil, e
		}
		a.ConfirmationRef = c.ConfirmationRef
		if e = s.confirmations.ConsumeAdmissionConfirmation(tx, c.ConfirmationRef, a, required); e != nil {
			return nil, e
		}
		if e = s.content.CheckUsable(tx, caller, cap.RateBasisRef); e != nil {
			return nil, e
		}
		if e = s.saveAdmission(tx, a); e != nil {
			return nil, e
		}
		job, e := s.scheduling.EnqueueHandoffInTransaction(tx, a)
		if e != nil {
			return nil, e
		}
		if e = s.saveHandoff(tx, &v1.Handoff{Ref: command.NewRef(s.user, s.domain, "handoff", "lerna.v1.Handoff"), AdmissionRef: ref, Identity: a.HandoffIdentity, JobRef: job, State: "PENDING"}); e != nil {
			return nil, e
		}
		call.AdmissionRef = ref
		call.State = "ADMITTED"
		if e = s.saveModelCall(tx, call); e != nil {
			return nil, e
		}
		r.ModelOperationRefs = append(r.ModelOperationRefs, op)
		if e = s.saveModelRequest(tx, r); e != nil {
			return nil, e
		}
		p.AdmissionRefs = append(p.AdmissionRefs, ref)
		return ref, s.store.SavePlanning(tx, p)
	})
}

func (s *Service) checkModelInput(ctx context.Context, caller *v1.Caller, c *v1.ModelCall) error {
	if c.InputRef == nil || c.DerivationRef == nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	d, e := s.modelContent.QueryDerivation(ctx, caller, c.DerivationRef)
	if e != nil {
		return e
	}
	if d == nil || d.State != "COMMITTED" || d.OutputKind != "CONTEXT" || d.GeneratorVersion != c.Settings.EncoderVersion || !proto.Equal(d.OutputRef, c.InputRef) || !sameRefs(d.ActualInputRefs, uniqueRefs(c.InputRefs)) || !sameRefs(d.SealedInputRefs, c.InputRefs) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	body, e := s.modelContent.Read(ctx, caller, c.InputRef)
	if e != nil || body == nil {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	sum := sha256.Sum256(command.ContentBytes(body))
	if hex.EncodeToString(sum[:]) != c.DescriptorDigest {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	for _, ref := range c.InputRefs {
		if e = s.content.CheckUsable(ctx, caller, ref); e != nil {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	return nil
}

// checkModelAdmission 只信任原调用位置和提议请求记录，不把动作自报类别当作准备权限。
func (s *Service) checkModelAdmission(ctx context.Context, caller *v1.Caller, a *v1.Admission) (bool, error) {
	if a.CapabilitySnapshot.GetAction() != "MODEL_INFER" {
		if a.WorkCategory == "PREPARATION" || a.ModelDescriptorDigest != "" {
			return false, command.Fail("MODEL_HOST_REQUIRED")
		}
		return false, nil
	}
	if a.Origin == nil || command.CheckName(caller, a.Origin.Name, s.user, s.domain, "model-call") != nil {
		return false, command.Fail("MODEL_HOST_REQUIRED")
	}
	call, e := s.store.(modelCallStore).LoadModelCallRef(ctx, a.Origin)
	if e != nil {
		return false, e
	}
	if call == nil || !proto.Equal(call.Ref, a.Origin) || !proto.Equal(call.AdmissionRef, a.Ref) || !proto.Equal(call.InputRef, a.ParametersRef) || call.DescriptorDigest != a.ModelDescriptorDigest || !proto.Equal(call.CapabilityRef, a.CapabilityRef) {
		return false, command.Fail("MODEL_HOST_REQUIRED")
	}
	request, e := s.QueryProposalRequest(ctx, caller, call.RequestRef)
	if e != nil {
		return false, e
	}
	if request == nil || !proto.Equal(request.TaskId, a.TaskId) {
		return false, command.Fail("MODEL_HOST_REQUIRED")
	}
	if e = s.currentModelRequest(ctx, caller, request); e != nil {
		return false, e
	}
	if e = s.checkModelInput(ctx, caller, call); e != nil {
		return false, e
	}
	return request.Purpose == "INTERPRET_INPUT" && a.WorkCategory == "PREPARATION", nil
}
