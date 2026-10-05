// Package ledger 持久接纳原动作身份；接纳本身不执行动作。
package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Store interface {
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
	store                      Store
	user, domain, sourceDomain string
}

func New(s Store, user, domain, sourceDomain string) *Service {
	return &Service{s, user, domain, sourceDomain}
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
		op := &v1.Operation{Ref: &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"}, AdmissionRef: a.Ref, ExecutorEndpointId: a.ExecutorEndpointId, AdapterRef: a.CapabilitySnapshot.AdapterRef, ParametersRef: a.ParametersRef, CapabilitySnapshot: a.CapabilitySnapshot, Lifecycle: "ACCEPTED", Dispatch: "OPEN", EffectRef: effect.Ref, Effect: effect}
		job := &v1.Job{Ref: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job"), Module: "ledger", JobType: "EXECUTE_OPERATION", ContractVersion: 1, Responsibility: c.Header.Identity, State: "READY", PurposeKey: "execute:" + a.OperationId.LocalId, SpecificationRef: op.Ref, ExecutorEndpointId: a.ExecutorEndpointId, LedgerDomainId: s.domain}
		if e = s.store.SaveOperation(tx, op); e != nil {
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
	}
	return false, nil
}
