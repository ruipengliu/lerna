package tasks

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ModelLedger interface {
	Prepare(context.Context, *v1.Caller, *v1.PrepareExecutionCommand) (*v1.CommandReceipt, error)
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	QueryObservation(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
	QueryReports(context.Context, *v1.Caller, *v1.Ref) (*v1.ObservationReports, error)
}
type ModelWork interface {
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}
type ModelCredentials interface {
	IssueCredential(context.Context, *v1.Caller, *v1.IssueExitCredentialCommand) (*v1.CommandReceipt, error)
	QueryGrant(context.Context, *v1.Caller, *v1.Ref) (*v1.Grant, error)
}
type ModelEgress interface {
	Invoke(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
}

func (s *Service) WithModelExecution(l ModelLedger, w ModelWork, g ModelCredentials, e ModelEgress) *Service {
	s.modelLedger = l
	s.modelWork = w
	s.modelCredentials = g
	s.modelEgress = e
	return s
}

// QueryModelCall 只查询原位置并重新检查治理许可，绝不增加采样或发送。
func (s *Service) QueryModelCall(ctx context.Context, c *v1.Caller, request *v1.Ref, position uint32) (*v1.ModelCall, error) {
	r, e := s.QueryProposalRequest(ctx, c, request)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	call, e := s.store.(modelCallStore).LoadModelCall(ctx, request, position)
	if e != nil || call == nil {
		return call, e
	}
	if call.State != "PREPARING" {
		if e = s.checkModelInput(ctx, c, call); e != nil {
			return nil, e
		}
	}
	if call.Result.GetOutputRef() != nil {
		if e = s.modelContent.CheckUsable(ctx, c, call.Result.OutputRef); e != nil {
			return nil, modelPreparationError(e)
		}
	}
	return call, nil
}

// RunModelCall 只接续核心保存的原位置；P5 后缺少结果时返回未知，不产生新发送。
func (s *Service) RunModelCall(ctx context.Context, caller *v1.Caller, c *v1.RunModelCallCommand) (*v1.ModelCallResult, error) {
	call, e := s.PrepareModelCall(ctx, caller, c.GetPreparation())
	if e != nil {
		return nil, e
	}
	p := c.Preparation
	admitted, e := s.AdmitModelCall(ctx, caller, &v1.AdmitModelCallCommand{Header: s.modelHeader("model-admit:"+call.Ref.Name.LocalId+":"+command.SemanticFingerprint("authority", c.GrantRef, c.ConfirmationRef, p.Header.Identity), s.domain), RequestRef: p.RequestRef, Position: p.Position, DescriptorDigest: call.DescriptorDigest, GrantRef: c.GrantRef, ConfirmationRef: c.ConfirmationRef, Claim: p.Claim})
	if e = modelReceipt(admitted, e); e != nil {
		return nil, e
	}
	call, e = s.QueryModelCall(ctx, caller, p.RequestRef, p.Position)
	if e != nil {
		return nil, e
	}
	if call.Result != nil && call.Result.Status != "UNKNOWN" {
		return call.Result, nil
	}
	a, e := s.QueryAdmission(ctx, caller, call.AdmissionRef)
	if e != nil {
		return nil, e
	}
	if e = s.ProcessHandoffs(ctx, caller); e != nil {
		return nil, e
	}
	op, e := s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return nil, e
	}
	if op == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if op.Execution != nil && op.Execution.Send.Phase != "REGISTERED" {
		return s.collectModelOutput(ctx, caller, call, a, op)
	}
	r, e := s.QueryProposalRequest(ctx, caller, p.RequestRef)
	if e != nil {
		return nil, e
	}
	// 新使用必须仍受当前提议工作围栏约束；结果恢复不需要续领旧工作。
	if e = s.currentModelClaim(ctx, caller, r, p.Claim); e != nil {
		return nil, e
	}
	if e = s.currentModelRequest(ctx, caller, r); e != nil {
		return nil, e
	}
	jobs, e := s.modelWork.Pending(ctx, caller)
	if e != nil {
		return nil, e
	}
	var job *v1.Job
	for _, j := range jobs {
		if j.JobType == "EXECUTE_OPERATION" && proto.Equal(j.SpecificationRef.GetName(), a.OperationId) {
			job = j
			break
		}
	}
	if job == nil {
		return nil, command.Fail("EXECUTION_NOT_READY")
	}
	claimed, e := s.modelWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: s.modelHeader(fmt.Sprintf("model-execution-claim:%s:%s:%d", call.Ref.Name.LocalId, p.Claim.ProcessInstance, p.Claim.ClaimEpoch), s.domain+"/ledger").Identity, ContractVersion: 1, Action: "CLAIM", JobRef: job.Ref, AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: p.Claim.ProcessInstance})
	if e = modelReceipt(claimed, e); e != nil {
		return nil, e
	}
	if len(claimed.Jobs) != 1 {
		return nil, command.Fail("EXECUTION_NOT_READY")
	}
	claim := claimed.Jobs[0]
	prepared, e := s.modelLedger.Prepare(ctx, caller, &v1.PrepareExecutionCommand{Header: s.modelHeader(fmt.Sprintf("model-execution:%s:%s:%d", call.Ref.Name.LocalId, claim.ProcessInstance, claim.ClaimEpoch), s.domain+"/ledger"), OperationId: a.OperationId, ProcessInstance: claim.ProcessInstance, Claim: claim})
	if e = modelReceipt(prepared, e); e != nil {
		return nil, e
	}
	op, e = s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return nil, e
	}
	x := op.Execution
	actor := &v1.Caller{UserId: s.user, IssuerId: "egress"}
	h := s.modelHeader("start:"+x.Send.Ref.Name.LocalId, s.domain)
	h.Identity.IssuerId = "egress"
	old, e := s.QueryStartReceipt(ctx, actor, h.Identity)
	if e != nil {
		return nil, e
	}
	var binding *v1.ExitCredentialBinding
	var credential *v1.Ref
	if old.Receipt != nil {
		if e = modelReceipt(old.Receipt, nil); e != nil {
			return nil, e
		}
		original, e := s.QueryStart(ctx, actor, old.Receipt.ResultRef)
		if e != nil {
			return nil, e
		}
		binding = original.Binding
		credential = original.CredentialRef
	} else {
		binding = &v1.ExitCredentialBinding{UserId: s.user, TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
		grant, e := s.modelCredentials.QueryGrant(ctx, caller, a.GrantRefs[0])
		if e != nil {
			return nil, e
		}
		if grant == nil {
			return nil, command.Fail("GRANT_INVALID")
		}
		snap, e := s.store.LoadSnapshot(ctx, r.SnapshotRef)
		if e != nil {
			return nil, e
		}
		issued, e := s.modelCredentials.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: s.modelHeader(fmt.Sprintf("model-credential:%s:%s:%d", call.Ref.Name.LocalId, claim.ProcessInstance, claim.ClaimEpoch), s.domain), AdmissionRef: a.Ref, Binding: binding, ExpiresAtUnixMs: min(snap.ExpiresAtUnixMs, grant.ValidUntilUnixMs)})
		if e = modelReceipt(issued, e); e != nil {
			return nil, e
		}
		credential = issued.ResultRef
	}
	sent, e := s.modelEgress.Invoke(ctx, actor, &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: credential, Binding: binding, CallDescriptor: x.CallDescriptor, Claim: claim})
	if e = modelReceipt(sent, e); e != nil {
		return nil, e
	}
	op, e = s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return nil, e
	}
	return s.collectModelOutput(ctx, caller, call, a, op)
}
