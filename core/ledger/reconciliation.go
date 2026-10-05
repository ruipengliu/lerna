package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type reconciliationStore interface {
	SaveReconciliation(context.Context, *v1.Reconciliation) error
	LoadReconciliation(context.Context, *v1.GlobalName) (*v1.Reconciliation, error)
	SaveReconciliationQuery(context.Context, *v1.ReconciliationQuery) error
	LoadReconciliationQuery(context.Context, *v1.Ref) (*v1.ReconciliationQuery, error)
	LoadClosureQuery(context.Context, *v1.Ref) (*v1.ReconciliationQuery, error)
	SaveReconciliationFinding(context.Context, *v1.ReconciliationFinding) error
	LoadReconciliationFinding(context.Context, *v1.Ref) (*v1.ReconciliationFinding, error)
}
type ReconciliationTasks interface {
	QueryCapability(context.Context, *v1.Caller, *v1.Ref) (*v1.Capability, error)
	AdmitClosure(context.Context, *v1.Caller, *v1.AdmitClosureCommand) (*v1.CommandReceipt, error)
	QueryClosureReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	ProcessHandoffs(context.Context, *v1.Caller) error
	ValidateClosureConfirmation(context.Context, *v1.Caller, *v1.AdmitClosureCommand, bool) error
}
type ReconciliationGrants interface {
	QueryGrant(context.Context, *v1.Caller, *v1.Ref) (*v1.Grant, error)
	IssueCredential(context.Context, *v1.Caller, *v1.IssueExitCredentialCommand) (*v1.CommandReceipt, error)
}
type ReconciliationEgress interface {
	Invoke(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
}
type reconciliationJobs interface {
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
	CheckReconciliationClaimInTransaction(context.Context, *v1.Job) (*v1.Job, error)
}

func (s *Service) WithReconciliation(t ReconciliationTasks, g ReconciliationGrants, e ReconciliationEgress) *Service {
	s.reconciliationTasks = t
	s.reconciliationGrants = g
	s.reconciliationEgress = e
	return s
}
func reconcileHeader(user, issuer, domain, id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: user, IssuerId: issuer, TargetDomainId: domain, CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func (s *Service) QueryReconciliation(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.Reconciliation, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "operation"); e != nil {
		return nil, e
	}
	return s.store.(reconciliationStore).LoadReconciliation(ctx, id)
}
func (s *Service) QueryReconciliationQuery(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ReconciliationQuery, error) {
	if r == nil || r.SchemaId != "lerna.v1.ReconciliationQuery" || r.Revision == 0 {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "reconciliation-query"); e != nil {
		return nil, e
	}
	q, e := s.store.(reconciliationStore).LoadReconciliationQuery(ctx, r)
	if q != nil && !proto.Equal(q.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return q, e
}
func (s *Service) QueryReconciliationFinding(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ReconciliationFinding, error) {
	if r == nil || r.SchemaId != "lerna.v1.ReconciliationFinding" || r.Revision != 1 {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "reconciliation-finding"); e != nil {
		return nil, e
	}
	return s.store.(reconciliationStore).LoadReconciliationFinding(ctx, r)
}
func (s *Service) RequestReconciliation(ctx context.Context, caller *v1.Caller, c *v1.RequestReconciliationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request-reconciliation", c.OperationId, c.QueryCapabilityRef, c.ParametersRef, c.GrantRef, c.ConfirmationRef, c.PolicyVersion, c.Limits), "ledger.reconcile_request", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" && caller.IssuerId != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		op, e := s.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil || op.Execution == nil || op.QuerySubject != nil || !op.Execution.Attempt.Capabilities.Queryable || op.Execution.Attempt.FirstPossibleSendAtUnixMs == 0 {
			return nil, command.Fail("TARGET_NOT_QUERYABLE")
		}
		existing, e := s.QueryReconciliation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if existing != nil {
			if !proto.Equal(existing.QueryCapabilityRef, c.QueryCapabilityRef) || !proto.Equal(existing.ParametersRef, c.ParametersRef) || !proto.Equal(existing.GrantRef, c.GrantRef) || !proto.Equal(existing.ConfirmationRef, c.ConfirmationRef) || existing.PolicyVersion != c.PolicyVersion || !proto.Equal(existing.Limits, c.Limits) {
				return nil, command.Fail("RECONCILIATION_EXISTS")
			}
			return existing.Ref, nil
		}
		if op.Lifecycle == "SETTLED" {
			return nil, command.Fail("ALREADY_SETTLED")
		}
		_, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return nil, e
		}
		l := c.Limits
		if c.PolicyVersion != 1 || l == nil || l.MaxChecks == 0 || l.MaxChecks > 1000 || l.DeadlineUnixMs <= now || l.MaxFee < 0 || l.InitialDelayMs <= 0 || l.MaxDelayMs < l.InitialDelayMs || l.MaxDelayMs > 86400000 {
			return nil, command.Fail("INVALID_RECONCILIATION_POLICY")
		}
		if s.reconciliationTasks == nil {
			return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		cap, e := s.reconciliationTasks.QueryCapability(tx, caller, c.QueryCapabilityRef)
		if e != nil {
			return nil, e
		}
		if !queryCapability(op, cap) {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		a, e := s.starts.QueryAdmission(tx, caller, op.AdmissionRef)
		if e != nil {
			return nil, e
		}
		if a == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		p := &v1.Reconciliation{Ref: command.NewRef(s.user, s.domain, "reconciliation", "lerna.v1.Reconciliation"), OperationId: op.Ref.Name, TaskId: a.TaskId, OriginalAttemptRef: op.Execution.Attempt.Ref, QueryCapabilityRef: c.QueryCapabilityRef, ParametersRef: c.ParametersRef, GrantRef: c.GrantRef, ConfirmationRef: c.ConfirmationRef, PolicyVersion: c.PolicyVersion, Limits: proto.Clone(l).(*v1.ReconciliationLimits), State: "READY", NextReconcileAtUnixMs: now, RequestedBy: c.Header.Identity}
		j := &v1.Job{Ref: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job"), Module: "ledger", JobType: "RECONCILE_OPERATION", ContractVersion: 1, Responsibility: c.Header.Identity, State: "READY", PurposeKey: "reconcile:" + op.Ref.Name.LocalId, SpecificationRef: op.Ref, ExecutorEndpointId: op.ExecutorEndpointId, LedgerDomainId: s.domain, ReadyAtUnixMs: now}
		p.JobRef = j.Ref
		if e = s.store.SaveLedgerJob(tx, j); e != nil {
			return nil, e
		}
		return p.Ref, s.saveReconciliation(tx, p)
	})
}
func queryCapability(op *v1.Operation, cap *v1.Capability) bool {
	return cap != nil && cap.Action == "QUERY" && cap.UseRight == "READ" && cap.ProcessingPurpose == "CURRENT_TASK" && cap.Resource == op.CapabilitySnapshot.Resource && cap.ExecutorEndpointId == op.ExecutorEndpointId && cap.AdapterRef != nil && cap.AdapterRef.Name != nil && cap.AdapterRef.Name.LocalId == "simulator-queryable" && cap.AdapterRef.Revision == 1
}

// ValidateClosureWork 读取核心保存的精确意图；原任务版本并非查询的新开始依据。
func (s *Service) QueryClosureWork(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.ClosureWorkRequest, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.ClosureWorkRequest" {
		return nil, command.Fail("INVALID_CLOSURE_ORIGIN")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "closure-work"); e != nil {
		return nil, e
	}
	q, e := s.store.(reconciliationStore).LoadClosureQuery(ctx, r)
	if e != nil {
		return nil, e
	}
	if q == nil || !proto.Equal(q.Work.Ref, r) {
		return nil, command.Fail("INVALID_CLOSURE_ORIGIN")
	}
	p, e := s.QueryReconciliation(ctx, caller, q.Work.QuerySubject.OperationId)
	if e != nil {
		return nil, e
	}
	if p == nil || p.ActiveQueryRef == nil || !proto.Equal(p.ActiveQueryRef.Name, q.Ref.Name) {
		return nil, command.Fail("CLOSURE_WORK_INACTIVE")
	}
	op, e := s.QueryOperation(ctx, caller, p.OperationId)
	if e != nil {
		return nil, e
	}
	w := q.Work
	if op == nil || op.Execution == nil || !proto.Equal(w.OwnerRef.Name, p.Ref.Name) || !proto.Equal(w.SourceRef.Name, q.Ref.Name) || !proto.Equal(w.SourceIdentity, p.RequestedBy) || !proto.Equal(w.TaskId, p.TaskId) || !proto.Equal(w.CapabilityRef, p.QueryCapabilityRef) || !proto.Equal(w.ParametersRef, p.ParametersRef) || !proto.Equal(w.GrantRef, p.GrantRef) || !proto.Equal(w.QuerySubject.AttemptId, op.Execution.Attempt.Ref.Name) || w.QuerySubject.ExternalKey != op.Execution.Attempt.ExternalKey || w.QuerySubject.TargetScope != op.CapabilitySnapshot.Resource || w.QuerySubject.ExecutorEndpointId != op.ExecutorEndpointId {
		return nil, command.Fail("INVALID_CLOSURE_ORIGIN")
	}
	return w, nil
}

func (s *Service) ValidateClosureWork(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ClosureWorkRequest, error) {
	w, e := s.QueryClosureWork(ctx, c, r)
	if e != nil {
		return nil, e
	}
	p, e := s.QueryReconciliation(ctx, c, w.QuerySubject.OperationId)
	if e != nil {
		return nil, e
	}
	if p.State != "IN_PROGRESS" {
		return nil, command.Fail("CLOSURE_WORK_INACTIVE")
	}
	return w, nil
}

func (s *Service) ProcessReconciliations(ctx context.Context, caller *v1.Caller) error {
	if e := command.CheckCaller(caller, s.user); e != nil {
		return e
	}
	jobs := s.work.(reconciliationJobs)
	pending, e := jobs.Pending(ctx, caller)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "host"}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	// 一次扫描每项责任最多推进一次；零抖动也不能在本次恢复里自旋。
	for _, j := range pending {
		if j.JobType != "RECONCILE_OPERATION" {
			continue
		}
		h := reconcileHeader(s.user, actor.IssuerId, s.domain, command.NewRef(s.user, s.domain, "command", "command").Name.LocalId)
		r, e := jobs.ExecuteJob(ctx, actor, &v1.JobCommand{Identity: h.Identity, ContractVersion: 1, Action: "CLAIM", Module: "ledger", AllowedTypes: []string{"RECONCILE_OPERATION"}, JobRef: j.Ref, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			continue
		}
		if e = s.ProcessReconciliationClaim(ctx, actor, r.Jobs[0]); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) ProcessReconciliationClaim(ctx context.Context, caller *v1.Caller, claim *v1.Job) error {
	if caller.GetUserId() != s.user || caller.GetIssuerId() != "host" {
		return command.Fail("PERMISSION_DENIED")
	}
	if claim == nil || claim.Ref == nil {
		return command.Fail("STALE_CLAIM")
	}
	h := reconcileHeader(s.user, caller.IssuerId, s.domain, fmt.Sprintf("reconcile-query:%s:%d:%d", claim.Ref.Name.LocalId, claim.ClaimEpoch, claim.Ref.Revision))
	r, e := s.work.Execute(ctx, caller, h, command.SemanticFingerprint("prepare-reconciliation-query", claim), "ledger.reconcile_prepare", func(tx context.Context) (*v1.Ref, error) {
		current, e := s.work.(reconciliationJobs).CheckReconciliationClaimInTransaction(tx, claim)
		if e != nil {
			return nil, e
		}
		p, e := s.QueryReconciliation(tx, caller, current.SpecificationRef.Name)
		if e != nil {
			return nil, e
		}
		if p == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		if p.ActiveQueryRef != nil {
			return p.ActiveQueryRef, nil
		}
		op, e := s.QueryOperation(tx, caller, p.OperationId)
		if e != nil {
			return nil, e
		}
		cap, e := s.reconciliationTasks.QueryCapability(tx, caller, p.QueryCapabilityRef)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return nil, e
		}
		reason := reconciliationLimitReason(p, now, cap)
		if !queryCapability(op, cap) {
			reason = "CAPABILITY_UNAVAILABLE"
		}
		if reason != "" {
			return p.Ref, s.pauseReconciliation(tx, p, reason)
		}
		q := &v1.ReconciliationQuery{Ref: command.NewRef(s.user, s.domain, "reconciliation-query", "lerna.v1.ReconciliationQuery"), State: "PENDING_ADMISSION"}
		p.Ref.Revision++
		p.CheckCount++
		p.State = "IN_PROGRESS"
		p.ActiveQueryRef = q.Ref
		p.QueryRefs = append(p.QueryRefs, q.Ref)
		w := &v1.ClosureWorkRequest{Ref: command.NewRef(s.user, s.domain, "closure-work", "lerna.v1.ClosureWorkRequest"), OwnerRef: proto.Clone(p.Ref).(*v1.Ref), SourceRef: proto.Clone(q.Ref).(*v1.Ref), SourceIdentity: p.RequestedBy, TaskId: p.TaskId, Purpose: "RECONCILE", CapabilityRef: p.QueryCapabilityRef, ParametersRef: p.ParametersRef, GrantRef: p.GrantRef, QuerySubject: &v1.QuerySubject{OperationId: p.OperationId, AttemptId: op.Execution.Attempt.Ref.Name, ExternalKey: op.Execution.Attempt.ExternalKey, TargetScope: op.CapabilitySnapshot.Resource, ExecutorEndpointId: op.ExecutorEndpointId, CapabilityRef: op.CapabilitySnapshot.Ref}}
		w.AdmissionIdentity = &v1.CommandIdentity{UserId: s.user, IssuerId: "ledger-reconciliation", TargetDomainId: s.sourceDomain, CommandId: "admit:" + w.Ref.Name.LocalId}
		q.Work = w
		if cap.FeeCeiling != nil {
			p.ReservedFee += *cap.FeeCeiling
		}
		if e = s.store.(reconciliationStore).SaveReconciliationQuery(tx, q); e != nil {
			return nil, e
		}
		return q.Ref, s.saveReconciliation(tx, p)
	})
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	if r.ResultRef.SchemaId != "lerna.v1.ReconciliationQuery" {
		return nil
	}
	q, e := s.QueryReconciliationQuery(ctx, caller, r.ResultRef)
	if e != nil {
		return e
	}
	// 不复用初次准备回执里的旧视图；交接可能已经完成。
	q, e = s.store.(reconciliationStore).LoadClosureQuery(ctx, q.Work.Ref)
	if e != nil {
		return e
	}
	if q.AdmissionCommand == nil {
		prepared, e := s.prepareClosureAdmission(ctx, caller, claim, q)
		if e != nil {
			return e
		}
		if prepared == nil {
			return nil
		}
		q = prepared
	}
	if q.AdmissionReceipt == nil {
		source := &v1.Caller{UserId: s.user, IssuerId: "ledger-reconciliation"}
		receipt, e := s.reconciliationTasks.QueryClosureReceipt(ctx, source, q.Work.AdmissionIdentity)
		if e != nil {
			return e
		}
		var ar *v1.CommandReceipt
		switch receipt.State {
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
			ar = receipt.Receipt
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
			ar, e = s.reconciliationTasks.AdmitClosure(ctx, source, q.AdmissionCommand)
		default:
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		if e != nil {
			return e
		}
		if ar == nil || !proto.Equal(ar.Identity, q.Work.AdmissionIdentity) || ar.Fingerprint != command.SemanticFingerprint("admit-closure", q.Work.Ref, q.AdmissionCommand.ConfirmationRef) || ar.ResponsibleDomainId != s.sourceDomain {
			return command.Fail("HANDOFF_RECEIPT_INVALID")
		}
		ack := reconcileHeader(s.user, caller.IssuerId, s.domain, "query-admission-ack:"+q.Ref.Name.LocalId)
		saved, e := s.work.Execute(ctx, caller, ack, command.SemanticFingerprint("reconciliation-admission-ack", q.Work.Ref, ar), "ledger.reconcile_admission_ack", func(tx context.Context) (*v1.Ref, error) {
			if _, e := s.work.(reconciliationJobs).CheckReconciliationClaimInTransaction(tx, claim); e != nil {
				return nil, e
			}
			fresh, e := s.store.(reconciliationStore).LoadClosureQuery(tx, q.Work.Ref)
			if e != nil {
				return nil, e
			}
			p, e := s.QueryReconciliation(tx, caller, q.Work.QuerySubject.OperationId)
			if e != nil {
				return nil, e
			}
			fresh.Ref.Revision++
			fresh.AdmissionReceipt = ar
			fresh.State = "ADMITTED"
			if ar.Decision != v1.Decision_DECISION_ACCEPTED {
				fresh.State = "REJECTED"
				fresh.Reason = ar.GetError().GetCode()
				p.State = "PAUSED"
				p.PauseReason = fresh.Reason
			} else {
				a, e := s.starts.QueryAdmission(tx, caller, ar.ResultRef)
				if e != nil {
					return nil, e
				}
				if a == nil || !proto.Equal(a.Origin, q.Work.Ref) {
					return nil, command.Fail("HANDOFF_RECEIPT_INVALID")
				}
				fresh.QueryOperationRef = &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"}
			}
			if p.State == "PAUSED" {
				if e = s.saveReconciliationJob(tx, p); e != nil {
					return nil, e
				}
			}
			if e = s.saveQueryAndPlan(tx, p, fresh); e != nil {
				return nil, e
			}
			return fresh.Ref, nil
		})
		if e != nil {
			return e
		}
		if saved.Error != nil {
			return &command.Failure{Detail: saved.Error}
		}
		q, e = s.QueryReconciliationQuery(ctx, caller, saved.ResultRef)
		if e != nil {
			return e
		}
	}
	if q.AdmissionReceipt.Decision != v1.Decision_DECISION_ACCEPTED {
		return nil
	}
	if e = s.reconciliationTasks.ProcessHandoffs(ctx, caller); e != nil {
		return e
	}
	e = s.executeReconciliationQuery(ctx, caller, q)
	var failure *command.Failure
	if errors.As(e, &failure) {
		switch failure.Detail.Code {
		case "QUERY_RESULT_UNKNOWN", "GRANT_INVALID", "GRANT_SCOPE_MISMATCH", "STALE_GENERATION", "CREDENTIAL_INVALID", "CLOSURE_WORK_INACTIVE", "CONTENT_UNUSABLE", "DISPATCH_SEALED":
			return s.pauseReconciliationClaim(ctx, caller, claim, q, failure.Detail.Code)
		}
	}
	return e
}
func (s *Service) saveQueryAndPlan(ctx context.Context, p *v1.Reconciliation, q *v1.ReconciliationQuery) error {
	p.Ref.Revision++
	for i, r := range p.QueryRefs {
		if proto.Equal(r.Name, q.Ref.Name) {
			p.QueryRefs[i] = q.Ref
		}
	}
	if p.ActiveQueryRef != nil && proto.Equal(p.ActiveQueryRef.Name, q.Ref.Name) {
		p.ActiveQueryRef = q.Ref
	}
	if e := s.store.(reconciliationStore).SaveReconciliationQuery(ctx, q); e != nil {
		return e
	}
	return s.saveReconciliation(ctx, p)
}
func (s *Service) executeReconciliationQuery(ctx context.Context, caller *v1.Caller, q *v1.ReconciliationQuery) error {
	a, e := s.starts.QueryAdmission(ctx, caller, q.AdmissionReceipt.ResultRef)
	if e != nil {
		return e
	}
	op, e := s.QueryOperation(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	if op == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if op.Execution != nil && op.Execution.Send.Phase != "REGISTERED" {
		if e = s.ProcessInterpretations(ctx, caller); e != nil {
			return e
		}
		current, e := s.QueryOperation(ctx, caller, a.OperationId)
		if e != nil {
			return e
		}
		if current.Execution.Send.Phase == "CLOSED" {
			return command.Fail("DISPATCH_SEALED")
		}
		if current.Lifecycle != "SETTLED" {
			return command.Fail("QUERY_RESULT_UNKNOWN")
		}
		return nil
	}
	jobs, e := s.QueryJobs(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	var pending *v1.Job
	for _, j := range jobs {
		if j.JobType == "EXECUTE_OPERATION" {
			pending = j
		}
	}
	if pending == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	id := reconcileHeader(s.user, "host", s.domain, "query-execute-claim:"+process)
	r, e := s.work.(reconciliationJobs).ExecuteJob(ctx, caller, &v1.JobCommand{Identity: id.Identity, ContractVersion: 1, Action: "CLAIM", Module: "ledger", AllowedTypes: []string{"EXECUTE_OPERATION"}, JobRef: pending.Ref, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	if len(r.Jobs) == 0 {
		return nil
	}
	claim := r.Jobs[0]
	r, e = s.Prepare(ctx, caller, &v1.PrepareExecutionCommand{Header: reconcileHeader(s.user, "host", s.domain, "query-prepare:"+process), OperationId: a.OperationId, ProcessInstance: process, Claim: claim})
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	x, e := s.QueryExecution(ctx, caller, a.OperationId)
	if e != nil {
		return e
	}
	startHeader := reconcileHeader(s.user, "egress", s.sourceDomain, "start:"+x.Send.Ref.Name.LocalId)
	prior, e := s.starts.QueryStartReceipt(ctx, &v1.Caller{UserId: s.user, IssuerId: "egress"}, startHeader.Identity)
	if e != nil {
		return e
	}
	if prior.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
		if prior.Receipt.Error != nil {
			return &command.Failure{Detail: prior.Receipt.Error}
		}
		saved, e := s.starts.QueryStart(ctx, caller, prior.Receipt.ResultRef)
		if e != nil {
			return e
		}
		if saved == nil {
			return command.Fail("START_RECEIPT_INVALID")
		}
		// P4 是不可变的原开始决定；新领取只接替 P5 推进权，不重新消耗凭证。
		r, e = s.reconciliationEgress.Invoke(ctx, &v1.Caller{UserId: s.user, IssuerId: "egress"}, &v1.StartExecutionCommand{Header: startHeader, AdmissionRef: a.Ref, CredentialRef: saved.CredentialRef, Binding: saved.Binding, Claim: claim, CallDescriptor: x.CallDescriptor})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		return nil
	}
	if prior.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	b := &v1.ExitCredentialBinding{UserId: s.user, TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	g, e := s.reconciliationGrants.QueryGrant(ctx, caller, q.Work.GrantRef)
	if e != nil {
		return e
	}
	if g == nil {
		return command.Fail("GRANT_INVALID")
	}
	r, e = s.reconciliationGrants.IssueCredential(ctx, caller, &v1.IssueExitCredentialCommand{Header: reconcileHeader(s.user, "host", s.sourceDomain, "query-credential:"+process), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: min(time.Now().Add(time.Minute).UnixMilli(), g.ValidUntilUnixMs)})
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	start := &v1.StartExecutionCommand{Header: reconcileHeader(s.user, "egress", s.sourceDomain, "start:"+x.Send.Ref.Name.LocalId), AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, Claim: claim, CallDescriptor: x.CallDescriptor}
	r, e = s.reconciliationEgress.Invoke(ctx, &v1.Caller{UserId: s.user, IssuerId: "egress"}, start)
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	return nil
}

// RecoverReconciliations 接替旧领取；未来的退避时间保留在工作记录中，不阻塞启动。
func (s *Service) RecoverReconciliations(ctx context.Context, c *v1.Caller) error {
	for {
		if e := s.ProcessReconciliations(ctx, c); e != nil {
			return e
		}
		pending, e := s.work.(reconciliationJobs).Pending(ctx, c)
		if e != nil {
			return e
		}
		claimed := false
		for _, j := range pending {
			if j.JobType == "RECONCILE_OPERATION" && j.State == "CLAIMED" {
				claimed = true
			}
		}
		if !claimed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// pauseReconciliationClaim 将确定的阻塞保存为原责任的暂停，不把读失败解释成效果终态。
func (s *Service) pauseReconciliationClaim(ctx context.Context, c *v1.Caller, claim *v1.Job, q *v1.ReconciliationQuery, reason string) error {
	h := reconcileHeader(s.user, c.IssuerId, s.domain, fmt.Sprintf("query-pause:%s:%d", q.Ref.Name.LocalId, claim.ClaimEpoch))
	r, e := s.work.Execute(ctx, c, h, command.SemanticFingerprint("pause-reconciliation-query", claim, q.Work.Ref, reason), "ledger.reconcile_pause", func(tx context.Context) (*v1.Ref, error) {
		if _, e := s.work.(reconciliationJobs).CheckReconciliationClaimInTransaction(tx, claim); e != nil {
			return nil, e
		}
		p, e := s.QueryReconciliation(tx, c, q.Work.QuerySubject.OperationId)
		if e != nil {
			return nil, e
		}
		if p == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		if p.ActiveQueryRef == nil || !proto.Equal(p.ActiveQueryRef.Name, q.Ref.Name) || p.State != "IN_PROGRESS" {
			return p.Ref, nil
		}
		return p.Ref, s.pauseReconciliation(tx, p, reason)
	})
	if e != nil {
		return e
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	return nil
}

// prepareClosureAdmission 在首次 P1 提交前核验附加确认；无效批准不占用准入命令身份。
func (s *Service) prepareClosureAdmission(ctx context.Context, c *v1.Caller, claim *v1.Job, q *v1.ReconciliationQuery) (*v1.ReconciliationQuery, error) {
	p, e := s.QueryReconciliation(ctx, c, q.Work.QuerySubject.OperationId)
	if e != nil {
		return nil, e
	}
	g, e := s.reconciliationGrants.QueryGrant(ctx, c, p.GrantRef)
	if e != nil {
		return nil, e
	}
	if g == nil {
		return nil, s.pauseReconciliationClaim(ctx, c, claim, q, "GRANT_INVALID")
	}
	cmd := &v1.AdmitClosureCommand{Header: reconcileHeader(s.user, "ledger-reconciliation", s.sourceDomain, q.Work.AdmissionIdentity.CommandId), WorkRef: q.Work.Ref, ConfirmationRef: p.ConfirmationRef}
	// 确认负责方在自己的裁决事务检查当前事项；执行账本不能写它的时钟或状态。
	if e = s.reconciliationTasks.ValidateClosureConfirmation(ctx, c, cmd, g.ConfirmationRequired); e != nil {
		var failure *command.Failure
		if !errors.As(e, &failure) {
			return nil, e
		}
		reason := failure.Detail.Code
		switch reason {
		case "CONFIRMATION_INVALID", "CONFIRMATION_EXPIRED", "GRANT_INVALID", "GRANT_SCOPE_MISMATCH":
			if p.ConfirmationRef == nil && g.ConfirmationRequired {
				reason = "CONFIRMATION_REQUIRED"
			}
			return nil, s.pauseReconciliationClaim(ctx, c, claim, q, reason)
		default:
			return nil, e
		}
	}
	h := reconcileHeader(s.user, c.IssuerId, s.domain, fmt.Sprintf("query-confirmation:%s:%d", q.Ref.Name.LocalId, claim.ClaimEpoch))
	r, e := s.work.Execute(ctx, c, h, command.SemanticFingerprint("prepare-closure-admission", claim, q.Work.Ref, cmd), "ledger.reconcile_confirmation", func(tx context.Context) (*v1.Ref, error) {
		if _, e := s.work.(reconciliationJobs).CheckReconciliationClaimInTransaction(tx, claim); e != nil {
			return nil, e
		}
		fresh, e := s.store.(reconciliationStore).LoadClosureQuery(tx, q.Work.Ref)
		if e != nil {
			return nil, e
		}
		if fresh.AdmissionCommand != nil {
			return fresh.Ref, nil
		}
		current, e := s.QueryReconciliation(tx, c, q.Work.QuerySubject.OperationId)
		if e != nil {
			return nil, e
		}
		if current.Ref.Revision != p.Ref.Revision {
			return nil, command.Fail("STALE_CLAIM")
		}
		fresh.Ref.Revision++
		fresh.AdmissionCommand = cmd
		if e = s.saveQueryAndPlan(tx, current, fresh); e != nil {
			return nil, e
		}
		return fresh.Ref, nil
	})
	if e != nil {
		return nil, e
	}
	if r.Error != nil {
		return nil, &command.Failure{Detail: r.Error}
	}
	return s.QueryReconciliationQuery(ctx, c, r.ResultRef)
}
