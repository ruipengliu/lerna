package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) advanceReasonerAction(ctx context.Context, caller *v1.Caller, d *v1.ReasonerDriver, p *v1.Proposal, process string) (*v1.ReasonerDriver, bool, error) {
	if d.AdmissionRef == nil {
		planning, e := s.QueryPlanning(ctx, caller, d.TaskId)
		if e != nil {
			return nil, false, e
		}
		var admission *v1.Ref
		// 准入已提交、driver 回执尚未保存时从原提议找回唯一责任。
		for _, ref := range planning.AdmissionRefs {
			a, e := s.QueryAdmission(ctx, caller, ref)
			if e != nil {
				return nil, false, e
			}
			if a != nil && proto.Equal(a.Origin, p.Ref) {
				admission = a.Ref
				break
			}
		}
		if admission == nil {
			var authority *v1.ReasonerActionAuthority
			for _, a := range d.Policy.Actions {
				if proto.Equal(a.CapabilityRef, p.Step.CapabilityRef) {
					authority = a
					break
				}
			}
			if authority == nil {
				return nil, false, command.Fail("ACTION_AUTHORITY_REQUIRED")
			}
			r, e := s.Admit(ctx, caller, &v1.AdmitCommand{Header: s.modelHeader("driver-admit:"+p.Ref.Name.LocalId+":"+command.SemanticFingerprint("configured", d.ConfiguredBy), s.domain), TaskId: d.TaskId, ProposalRef: p.Ref, GrantRef: authority.GrantRef, ConfirmationRef: authority.ConfirmationRef})
			if e = modelReceipt(r, e); e != nil {
				return nil, false, e
			}
			admission = r.ResultRef
		}
		next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
			next.AdmissionRef = admission
			next.State = "READY"
			next.WaitingReason = ""
		})
		return next, true, e
	}
	a, e := s.QueryAdmission(ctx, caller, d.AdmissionRef)
	if e != nil {
		return nil, false, e
	}
	if a == nil || !proto.Equal(a.Origin, p.Ref) || !proto.Equal(a.TaskId, d.TaskId) {
		return nil, false, command.Fail("INVARIANT_VIOLATION")
	}
	if e = s.runReasonerAction(ctx, caller, d, a, process); e != nil {
		return nil, false, e
	}
	snapshot, e := s.requestProposal(ctx, caller, &v1.RequestProposalCommand{Header: s.modelHeader("driver-next:"+d.Ref.Name.LocalId+":"+d.RequestRef.Name.LocalId, s.domain), TaskId: d.TaskId}, func(tx context.Context) error {
		e := s.checkReasonerContinuation(tx, caller, d, a)
		var failure *command.Failure
		if errors.As(e, &failure) {
			// 尚无可推进事实时没有新请求决定；等待只保存到原 driver。
			detail := proto.Clone(failure.Detail).(*v1.ContractError)
			detail.Category = v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT
			detail.CommandAcceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED
			return &command.Failure{Detail: detail}
		}
		return e
	})
	if e != nil {
		return nil, false, e
	}
	next, e := s.updateReasonerDriver(ctx, d, func(next *v1.ReasonerDriver) {
		next.RequestRef = snapshot.RequestRef
		next.OutcomeRef = nil
		next.AdmissionRef = nil
		next.QuestionRef = nil
		next.State = "READY"
		next.WaitingReason = ""
	})
	return next, true, e
}

func (s *Service) checkReasonerContinuation(ctx context.Context, caller *v1.Caller, d *v1.ReasonerDriver, a *v1.Admission) error {
	current, e := s.QueryReasonerDriver(ctx, caller, d.TaskId)
	if e != nil {
		return e
	}
	if current == nil || !current.Enabled || !proto.Equal(current.Ref, d.Ref) {
		return command.Fail("STALE_REFERENCE")
	}
	t, e := s.QueryTask(ctx, caller, d.TaskId)
	if e != nil {
		return e
	}
	planning, e := s.QueryPlanning(ctx, caller, d.TaskId)
	if e != nil {
		return e
	}
	snap := planning.Snapshot
	if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || planning.VerificationFreeze != 0 {
		return command.Fail("TASK_NOT_ACTIVE")
	}
	if snap == nil || !proto.Equal(snap.RequestRef, d.RequestRef) || snap.InputVersion != t.InputVersion || snap.RequirementsVersion != t.RequirementsVersion || snap.ControlGeneration != t.ControlGeneration || snap.PlanningGeneration != t.PlanningGeneration {
		return command.Fail("STALE_PROPOSAL")
	}
	operation, e := s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	// RULED_OUT 只描述迟到效果；UNKNOWN 不因此变成已知终局。
	if operation == nil || operation.Lifecycle != "SETTLED" || operation.Dispatch != "SEALED" || operation.Effect == nil || (operation.Effect.Outcome != "APPLIED" && operation.Effect.Outcome != "NOT_APPLIED") || operation.Effect.LateEffect != "RULED_OUT" || operation.Effect.EvidenceConflict {
		return command.Fail("OPERATION_PENDING")
	}
	return s.checkReasonerProgress(ctx, caller, planning)
}

// runReasonerAction 只消费既有准入和原发送；P5 之后只查询，不创建重发。
func (s *Service) runReasonerAction(ctx context.Context, caller *v1.Caller, d *v1.ReasonerDriver, a *v1.Admission, process string) error {
	if e := s.ProcessHandoffs(ctx, caller); e != nil {
		return e
	}
	op, e := s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	if op == nil {
		return command.Fail("EXECUTION_NOT_READY")
	}
	if op.Execution != nil && op.Execution.Send.Phase != "REGISTERED" {
		return nil
	}
	jobs, e := s.modelWork.Pending(ctx, caller)
	if e != nil {
		return e
	}
	var job *v1.Job
	for _, j := range jobs {
		if j.JobType == "EXECUTE_OPERATION" && proto.Equal(j.SpecificationRef.GetName(), a.OperationId) {
			job = j
			break
		}
	}
	if job == nil {
		return command.Fail("EXECUTION_NOT_READY")
	}
	claimed, e := s.modelWork.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: s.modelHeader("driver-action-claim:"+a.Ref.Name.LocalId+":"+process, s.domain+"/ledger").Identity, ContractVersion: 1, Action: "CLAIM", JobRef: job.Ref, AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 60000, ProcessInstance: process})
	if e = modelReceipt(claimed, e); e != nil {
		return e
	}
	if len(claimed.Jobs) != 1 {
		return command.Fail("CLAIM_PENDING")
	}
	claim := claimed.Jobs[0]
	prepared, e := s.modelLedger.Prepare(ctx, caller, &v1.PrepareExecutionCommand{Header: s.modelHeader(fmt.Sprintf("driver-action-prepare:%s:%s:%d", a.Ref.Name.LocalId, process, claim.ClaimEpoch), s.domain+"/ledger"), OperationId: a.OperationId, ProcessInstance: process, Claim: claim})
	if e = modelReceipt(prepared, e); e != nil {
		return e
	}
	op, e = s.modelLedger.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	x := op.Execution
	actor := &v1.Caller{UserId: s.user, IssuerId: "egress"}
	header := s.modelHeader("start:"+x.Send.Ref.Name.LocalId, s.domain)
	header.Identity.IssuerId = "egress"
	old, e := s.QueryStartReceipt(ctx, actor, header.Identity)
	if e != nil {
		return e
	}
	var binding *v1.ExitCredentialBinding
	var credential *v1.Ref
	if old.Receipt != nil {
		if e = modelReceipt(old.Receipt, nil); e != nil {
			return e
		}
		start, e := s.QueryStart(ctx, actor, old.Receipt.ResultRef)
		if e != nil {
			return e
		}
		if start == nil {
			return command.Fail("START_RECEIPT_INVALID")
		}
		binding = start.Binding
		credential = start.CredentialRef
	} else {
		binding = &v1.ExitCredentialBinding{UserId: s.user, TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
		grant, e := s.modelCredentials.QueryGrant(ctx, caller, a.GrantRefs[0])
		if e != nil {
			return e
		}
		if grant == nil {
			return command.Fail("GRANT_INVALID")
		}
		request, e := s.QueryProposalRequest(ctx, caller, d.RequestRef)
		if e != nil {
			return e
		}
		snap, e := s.QuerySnapshot(ctx, caller, request.SnapshotRef)
		if e != nil {
			return e
		}
		issued, e := s.modelCredentials.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: s.modelHeader(fmt.Sprintf("driver-action-credential:%s:%s:%d", a.Ref.Name.LocalId, process, claim.ClaimEpoch), s.domain), AdmissionRef: a.Ref, Binding: binding, ExpiresAtUnixMs: min(snap.ExpiresAtUnixMs, grant.ValidUntilUnixMs)})
		if e = modelReceipt(issued, e); e != nil {
			return e
		}
		credential = issued.ResultRef
	}
	sent, e := s.modelEgress.Invoke(ctx, actor, &v1.StartExecutionCommand{Header: header, AdmissionRef: a.Ref, CredentialRef: credential, Binding: binding, CallDescriptor: x.CallDescriptor, Claim: claim})
	return modelReceipt(sent, e)
}

func (s *Service) checkReasonerProgress(ctx context.Context, caller *v1.Caller, planning *v1.PlanningState) error {
	var all []*v1.Ref
	for _, ref := range planning.AdmissionRefs {
		admitted, e := s.QueryAdmission(ctx, caller, ref)
		if e != nil {
			return e
		}
		if admitted == nil {
			return command.Fail("EXECUTION_FACTS_UNAVAILABLE")
		}
		op, e := s.modelLedger.QueryOperation(ctx, caller, admitted.OperationId)
		if e != nil {
			return e
		}
		if op == nil {
			return command.Fail("EXECUTION_FACTS_UNAVAILABLE")
		}
		all = append(all, op.Ref)
		if op.Lifecycle != "SETTLED" || op.Dispatch != "SEALED" || op.Effect == nil || (op.Effect.Outcome != "APPLIED" && op.Effect.Outcome != "NOT_APPLIED") || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
			return command.Fail("BLOCKING_OPERATION")
		}
		if op.Execution != nil {
			if op.Execution.Send.Phase == "DISPATCH_POSSIBLE" {
				return command.Fail("REPORT_PENDING")
			}
			noSend, e := s.reasonerCompletionNoSend(ctx, caller, admitted, op)
			if e != nil {
				return e
			}
			if noSend {
				continue
			}
			raw, e := s.modelLedger.QueryObservation(ctx, caller, op.Execution.Send.ObservationRef)
			if e != nil {
				return e
			}
			if raw == nil {
				return command.Fail("REPORT_PENDING")
			}
			reports, e := s.modelLedger.QueryReports(ctx, caller, raw.Ref)
			if e != nil {
				return e
			}
			if reports == nil || reports.UsageReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || reports.TraceReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
				return command.Fail("REPORT_PENDING")
			}
		}
	}
	blocked, e := s.execution.BlocksAdmission(ctx, caller, all, planning.RejectedVerificationOperations)
	if e != nil {
		return e
	}
	if blocked {
		return command.Fail("BLOCKING_OPERATION")
	}
	return nil
}

// reasonerCompletionNoSend 只接纳原端点保存的全历史未发送封闭，不从缺失观察推断未发送。
func (s *Service) reasonerCompletionNoSend(ctx context.Context, caller *v1.Caller, admission *v1.Admission, op *v1.Operation) (bool, error) {
	if s.completionFacts == nil || op.Execution == nil || op.Effect == nil || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
		return false, nil
	}
	for _, ref := range op.ClosureEvidenceRefs {
		if ref.SchemaId != "lerna.v1.CompletionSeal" {
			continue
		}
		seal, e := s.completionFacts.QueryCompletionSeal(ctx, caller, ref)
		if e != nil {
			return false, e
		}
		if seal == nil {
			return false, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
		}
		if !seal.NoSendProven || seal.PhysicalSendWasPossible || !proto.Equal(seal.AdmissionRef, admission.Ref) || !proto.Equal(seal.OperationId, op.Ref.Name) || seal.ExecutorEndpointId != admission.ExecutorEndpointId || !proto.Equal(seal.OperationRef.GetName(), op.Ref.Name) {
			continue
		}
		closed := true
		for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
			found := false
			for _, closedRef := range seal.ClosedSendRefs {
				found = found || proto.Equal(closedRef, send.Ref)
			}
			closed = closed && send.Phase == "CLOSED" && send.ObservationRef == nil && found
		}
		if closed {
			return true, nil
		}
	}
	return false, nil
}
