// Package ledger 持久接纳原动作身份；接纳本身不执行动作。
package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"google.golang.org/protobuf/proto"
)

// Store 声明执行管理全部命令、查询与恢复需要的持久能力。
type Store interface {
	progressStore
	cancellationSealStore
	completionSealStore
	historyStore
	interpretationStore
	executionFollowupStore
	taskClosureSealStore
	reportStore
	observationStore
	grantClosureStore
	recoveryOperations
	metricFacts
	reconciliationStore
	CheckRecoveryAllowed(context.Context) error
	TraceSource
	LedgerTransaction(context.Context, func(context.Context) error) error
	LedgerPosition(context.Context) (uint64, int64, error)
	LoadReceipt(context.Context, *v1.CommandIdentity) (*v1.CommandReceipt, error)
	SaveLedgerReceipt(context.Context, *v1.CommandReceipt) error
	LoadOperation(context.Context, *v1.GlobalName) (*v1.Operation, error)
	SaveOperation(context.Context, *v1.Operation) error
	SaveLedgerJob(context.Context, *v1.Job) error
	LedgerJobs(context.Context, *v1.GlobalName) ([]*v1.Job, error)
}
type Service struct {
	rules                      EvidenceRules
	cancellationClosures       CancellationClosureSource
	taskClosures               TaskClosureSource
	completionClosures         CompletionClosureSource
	progressReceiver           OperationProgressReceiver
	reconciliationTasks        ReconciliationTasks
	reconciliationGrants       ReconciliationGrants
	reconciliationEgress       ReconciliationEgress
	store                      Store
	user, domain, sourceDomain string
	work                       ExecutionWork
	adapter                    Compiler
	starts                     StartFacts
	observations               ObservationContent
	usage                      UsageReceiver
	usageReceipts              ReceiptReader
	trace                      TraceReceiver
	grantClosures              GrantClosures
}

func New(s Store, user, domain, sourceDomain string) (*Service, error) {
	if err := durable.RequireDependencies("ledger", durable.Dependency{Name: "store", Value: s}); err != nil {
		return nil, err
	}
	return &Service{store: s, user: user, domain: domain, sourceDomain: sourceDomain}, nil
}
func (s *Service) Accept(ctx context.Context, caller *v1.Caller, c *v1.AcceptOperationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if e := command.CheckIdentity(caller, c.Header.Identity, s.user, s.domain); e != nil {
		return nil, e
	}
	if caller.IssuerId != "tasks-handoff" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	fingerprint := command.SemanticFingerprint("accept-operation", c.Admission)
	var r *v1.CommandReceipt
	e := s.store.LedgerTransaction(ctx, func(tx context.Context) error {
		old, e := s.store.LoadReceipt(tx, c.Header.Identity)
		if e != nil {
			return e
		}
		if old != nil {
			if old.Fingerprint != fingerprint {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			r = old
			return nil
		}
		reject := func(code string) error {
			position, now, e := s.store.LedgerPosition(tx)
			if e != nil {
				return e
			}
			failure := &v1.ContractError{Code: code, Category: v1.ErrorCategory_ERROR_CATEGORY_PERMANENT, CommandAcceptance: v1.CommandAcceptance_COMMAND_ACCEPTANCE_DECIDED, RecoveryAction: "CORRECT_REQUEST"}
			r = &v1.CommandReceipt{Identity: c.Header.Identity, FingerprintVersion: 1, Fingerprint: fingerprint, Phase: v1.ReceiptPhase_RECEIPT_PHASE_DECIDED, Decision: v1.Decision_DECISION_REJECTED, DecisionRef: command.NewRef(s.user, s.domain, "decision", "lerna.v1.CommandReceipt"), ResponsibleDomainId: s.domain, CommitPosition: position, DecidedAtUnixMs: now, DurabilityProfile: "LOCAL", Error: failure}
			return s.store.SaveLedgerReceipt(tx, r)
		}
		a := c.Admission
		if a == nil || a.Ref == nil || a.Ref.Name == nil || a.Ref.Name.UserId != s.user || a.Ref.Name.AuthorityDomainId != s.sourceDomain || a.Ref.Name.ObjectKind != "admission" || a.Ref.Revision != 1 || a.Ref.SchemaId != "lerna.v1.Admission" || a.OperationId == nil || a.OperationId.AuthorityDomainId != s.domain || a.OperationId.UserId != s.user || a.OperationId.ObjectKind != "operation" || a.OperationId.LocalId != c.Header.Identity.CommandId || !proto.Equal(a.HandoffIdentity, c.Header.Identity) || a.LedgerDomainId != s.domain || a.ExecutorEndpointId == "" || a.CapabilitySnapshot == nil || a.ParametersRef == nil || a.BudgetBasis == nil || a.BudgetBasis.ReservationRef == nil || a.GrantUseRef == nil {
			return reject("INVALID_HANDOFF")
		}
		if a.ExecutorEndpointId != a.CapabilitySnapshot.ExecutorEndpointId || !proto.Equal(a.CapabilityRef, a.CapabilitySnapshot.Ref) {
			return reject("INVALID_HANDOFF")
		}
		existing, e := s.store.LoadOperation(tx, a.OperationId)
		if e != nil {
			return e
		}
		if existing != nil {
			return reject("OPERATION_IDENTITY_CONFLICT")
		}
		effect := &v1.Effect{Ref: command.NewRef(s.user, s.domain, "effect", "lerna.v1.Effect"), OperationId: a.OperationId, Outcome: "NOT_APPLIED", LateEffect: "RULED_OUT"}
		op := &v1.Operation{Ref: &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"}, AdmissionRef: a.Ref, ExecutorEndpointId: a.ExecutorEndpointId, AdapterRef: a.CapabilitySnapshot.AdapterRef, ParametersRef: a.ParametersRef, CapabilitySnapshot: a.CapabilitySnapshot, ModelDescriptorDigest: a.ModelDescriptorDigest, Lifecycle: "ACCEPTED", Dispatch: "OPEN", EffectRef: effect.Ref, Effect: effect}
		if a.WorkCategory == "CLOSURE" {
			op.ClosureWorkRef = a.Origin
			op.QuerySubject = a.QuerySubject
		}
		job := &v1.Job{Ref: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job"), Module: "ledger", JobType: "EXECUTE_OPERATION", ContractVersion: 1, Responsibility: c.Header.Identity, State: "READY", PurposeKey: "execute:" + a.OperationId.LocalId, SpecificationRef: op.Ref, ExecutorEndpointId: a.ExecutorEndpointId, LedgerDomainId: s.domain}
		seal, e := s.store.CompletionSealForOperation(tx, a.OperationId)
		if e != nil {
			return e
		}
		if seal != nil {
			if !proto.Equal(seal.AdmissionRef, a.Ref) {
				return reject("INVALID_CLOSURE")
			}
			op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, seal.Ref)
			applyCompletionNoSend(op)
			job.State = "COMPLETED"
		}
		cancellationSeal, e := s.store.CancellationSealForOperation(tx, a.OperationId)
		if e != nil {
			return e
		}
		if cancellationSeal != nil {
			if !proto.Equal(cancellationSeal.AdmissionRef, a.Ref) || cancellationSeal.ExecutorEndpointId != a.ExecutorEndpointId {
				return reject("INVALID_CLOSURE")
			}
			op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, cancellationSeal.Ref)
			applyCompletionNoSend(op)
			job.State = "COMPLETED"
		}
		taskSeal, e := s.store.TaskClosureSealForOperation(tx, a.OperationId)
		if e != nil {
			return e
		}
		if taskSeal != nil {
			if !proto.Equal(taskSeal.AdmissionRef, a.Ref) {
				return reject("INVALID_CLOSURE")
			}
			op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, taskSeal.Ref)
			applyCompletionNoSend(op)
			job.State = "COMPLETED"
		}
		if e = s.saveOperation(tx, op); e != nil {
			return e
		}
		if e = s.store.SaveLedgerJob(tx, job); e != nil {
			return e
		}
		position, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return e
		}
		r = &v1.CommandReceipt{Identity: c.Header.Identity, FingerprintVersion: 1, Fingerprint: fingerprint, Phase: v1.ReceiptPhase_RECEIPT_PHASE_DECIDED, Decision: v1.Decision_DECISION_ACCEPTED, DecisionRef: command.NewRef(s.user, s.domain, "decision", "lerna.v1.CommandReceipt"), ResultRef: op.Ref, JobRef: job.Ref, ResponsibleDomainId: s.domain, CommitPosition: position, DecidedAtUnixMs: now, DurabilityProfile: "LOCAL"}
		return s.store.SaveLedgerReceipt(tx, r)
	})
	if e != nil {
		return nil, e
	}
	return r, nil
}
func (s *Service) QueryReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	if e := command.CheckIdentity(c, id, s.user, s.domain); e != nil {
		return nil, e
	}
	r, e := s.store.LoadReceipt(ctx, id)
	if e != nil {
		return &v1.ReceiptQuery{State: v1.ReceiptQueryState_RECEIPT_QUERY_STATE_UNAVAILABLE, ResponsibleDomainId: s.domain}, nil
	}
	q := &v1.ReceiptQuery{State: v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND, ResponsibleDomainId: s.domain}
	if r != nil {
		q.State = v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED
		q.Receipt = r
		q.Revision = r.CommitPosition
	}
	return q, nil
}
func (s *Service) QueryOperation(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.Operation, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "operation"); e != nil {
		return nil, e
	}
	return s.store.LoadOperation(ctx, id)
}
func (s *Service) QueryJobs(ctx context.Context, c *v1.Caller, id *v1.GlobalName) ([]*v1.Job, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "operation"); e != nil {
		return nil, e
	}
	return s.store.LedgerJobs(ctx, id)
}

// BlocksAdmission 查询权威效果；上一轮已开始但未收尾的动作即使效果已知也阻断补建。
func (s *Service) BlocksAdmission(ctx context.Context, c *v1.Caller, all, rejected []*v1.Ref) (bool, error) {
	for _, ref := range all {
		op, e := s.QueryOperation(ctx, c, ref.Name)
		if e != nil {
			return false, e
		}
		if op != nil && (op.Effect == nil || (op.Effect.Outcome != "APPLIED" && op.Effect.Outcome != "NOT_APPLIED") || op.Effect.LateEffect == "MAY_OCCUR" || op.Effect.EvidenceConflict) {
			return true, nil
		}
	}
	for _, ref := range rejected {
		op, e := s.QueryOperation(ctx, c, ref.Name)
		if e != nil {
			return false, e
		}
		if op == nil {
			return true, nil
		}
		if op.StartReceiptObtained && op.Lifecycle != "SETTLED" {
			return true, nil
		}
		if op.Lifecycle != "SETTLED" && op.Execution != nil {
			// P4 先在裁决域成立；尚未送到 P5 的开始回执同样阻止补建。
			if s.starts == nil {
				return false, command.Fail("DEPENDENCY_UNAVAILABLE")
			}
			actor := &v1.Caller{UserId: s.user, IssuerId: "egress"}
			a, e := s.starts.QueryAdmission(ctx, actor, op.AdmissionRef)
			if e != nil {
				return false, e
			}
			if a == nil || !proto.Equal(a.OperationId, op.Ref.Name) || a.ExecutorEndpointId != op.ExecutorEndpointId {
				return false, command.Fail("INVARIANT_VIOLATION")
			}
			for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
				id := &v1.CommandIdentity{UserId: s.user, IssuerId: actor.IssuerId, TargetDomainId: s.sourceDomain, CommandId: "start:" + send.Ref.Name.LocalId}
				q, e := s.starts.QueryStartReceipt(ctx, actor, id)
				if e != nil {
					return false, e
				}
				if q == nil || q.ResponsibleDomainId != s.sourceDomain {
					return false, command.Fail("DEPENDENCY_UNAVAILABLE")
				}
				if q.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
					continue
				}
				if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || q.Receipt == nil || q.Receipt.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || q.Receipt.FingerprintVersion != 1 || !proto.Equal(q.Receipt.Identity, id) || q.Receipt.ResponsibleDomainId != s.sourceDomain {
					return false, command.Fail("DEPENDENCY_UNAVAILABLE")
				}
				if q.Receipt.Decision == v1.Decision_DECISION_ACCEPTED {
					start, e := s.starts.QueryStart(ctx, actor, q.Receipt.ResultRef)
					if e != nil {
						return false, e
					}
					binding := start.GetBinding()
					expected := &v1.ExitCredentialBinding{UserId: s.user, TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: op.Execution.Attempt.Ref.Name, SendSeq: send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: send.ProcessInstance, DescriptorDigest: op.Execution.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
					if start == nil || !proto.Equal(start.Ref, q.Receipt.ResultRef) || start.CredentialRef == nil || !proto.Equal(binding, expected) || !proto.Equal(send.AttemptId, op.Execution.Attempt.Ref.Name) || !proto.Equal(start.GetIdentity(), id) || !proto.Equal(start.GetSendRef().GetName(), send.Ref.Name) || !proto.Equal(binding.GetOperationId(), op.Ref.Name) || !proto.Equal(binding.GetAdmissionRef(), op.AdmissionRef) || !proto.Equal(binding.GetAttemptId(), op.Execution.Attempt.Ref.Name) || binding.GetSendSeq() != send.SendSeq || binding.GetExecutorEndpointId() != op.ExecutorEndpointId || binding.GetDescriptorDigest() != op.Execution.CallDescriptor.Digest || binding.GetUserId() != s.user {
						return false, command.Fail("INVARIANT_VIOLATION")
					}
					return true, nil
				}
			}
		}
	}
	return false, nil
}
