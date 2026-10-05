package budget

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ClosureEvidence interface {
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	QueryGrantExitClosure(context.Context, *v1.Caller, *v1.Ref) (*v1.GrantExitClosure, error)
}
type releaseStore interface {
	SaveReservationRelease(context.Context, *v1.ReservationRelease) error
	LoadReservationRelease(context.Context, *v1.Ref) (*v1.ReservationRelease, error)
}

// ReleaseUnused 只消费原负责方的不可变未发送证明，不伪造零用量。
func (s *Service) ReleaseUnused(ctx context.Context, caller *v1.Caller, c *v1.ReleaseReservationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("release-unused", c.ReservationRef, c.ClosureRef), "budget.release", func(tx context.Context) (*v1.Ref, error) {
		if !s.trustedCaller(caller) && caller.IssuerId != "budget-closure" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		original, e := s.QueryReservation(tx, caller, c.ReservationRef)
		if e != nil {
			return nil, e
		}
		if original == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		old, e := s.store.(releaseStore).LoadReservationRelease(tx, c.ReservationRef)
		if e != nil {
			return nil, e
		}
		if old != nil {
			if !proto.Equal(old.ClosureRef, c.ClosureRef) {
				return nil, command.Fail("RELEASE_PROOF_CONFLICT")
			}
			return old.Ref, nil
		}
		r, e := s.store.(sendStore).LoadCurrentReservation(tx, c.ReservationRef)
		if e != nil {
			return nil, e
		}
		if r.Status != "RESERVED" {
			return nil, command.Fail("RESERVATION_NOT_PENDING")
		}
		send, e := s.validateNoSend(tx, caller, r, c.ClosureRef)
		if e != nil {
			return nil, e
		}
		for _, id := range []*v1.GlobalName{nil, r.TaskId} {
			b, e := s.store.LoadBudget(tx, id)
			if e != nil {
				return nil, e
			}
			if b == nil || b.Reserved < r.Ceiling {
				return nil, command.Fail("INVALID_RESERVATION")
			}
			b.Reserved -= r.Ceiling
			b.Ref.Revision++
			if e = projectBudget(b); e != nil {
				return nil, e
			}
			if e = s.store.SaveBudget(tx, b); e != nil {
				return nil, e
			}
		}
		if send != nil {
			source, e := s.store.(billingStore).LoadBillingSource(tx, send)
			if e != nil {
				return nil, e
			}
			if source != nil {
				source.Ref.Revision++
				source.Status = "UNUSED_CLOSED"
				if e = s.store.(billingStore).SaveBillingSource(tx, source); e != nil {
					return nil, e
				}
			}
		}
		r.Ref.Revision++
		r.Status = "UNUSED_CLOSED"
		if e = s.store.SaveReservation(tx, r); e != nil {
			return nil, e
		}
		release := &v1.ReservationRelease{Ref: command.NewRef(s.user, s.domain, "reservation-release", "lerna.v1.ReservationRelease"), ReservationRef: c.ReservationRef, ClosureRef: c.ClosureRef, Released: r.Ceiling}
		return release.Ref, s.store.(releaseStore).SaveReservationRelease(tx, release)
	})
}

// ProcessClosures 恢复已有封闭证明；缺少证明的预留继续占用。
func (s *Service) ProcessClosures(ctx context.Context) error {
	all, e := s.store.(interface {
		AllReservations(context.Context) ([]*v1.Reservation, error)
	}).AllReservations(ctx)
	if e != nil {
		return e
	}
	caller := &v1.Caller{UserId: s.user, IssuerId: "budget-closure"}
	for _, r := range all {
		if r.Status != "RESERVED" {
			continue
		}
		op, e := s.usageSource.(ClosureEvidence).QueryOperation(ctx, caller, r.OperationId)
		if e != nil {
			return e
		}
		if op == nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.GetEffect().GetOutcome() != "NOT_APPLIED" || op.GetEffect().GetLateEffect() != "RULED_OUT" {
			continue
		}
		for _, ref := range op.ClosureEvidenceRefs {
			if kind := ref.GetName().GetObjectKind(); kind != "grant-exit-closure" && kind != "completion-seal" {
				continue
			}
			if _, e = s.validateNoSend(ctx, caller, r, ref); e != nil {
				var failure *command.Failure
				if errors.As(e, &failure) && failure.Detail.Code == "NO_SEND_UNPROVEN" {
					continue
				}
				return e
			}
			header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: caller.IssuerId, TargetDomainId: s.domain, CommandId: "release:" + r.Ref.Name.LocalId + ":" + ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
			receipt, e := s.ReleaseUnused(ctx, caller, &v1.ReleaseReservationCommand{Header: header, ReservationRef: r.Ref, ClosureRef: ref})
			if e != nil {
				return e
			}
			if receipt.Decision != v1.Decision_DECISION_ACCEPTED {
				return command.Fail("BUDGET_RELEASE_REJECTED")
			}
			break
		}
	}
	return nil
}
func (s *Service) QueryReservationRelease(ctx context.Context, c *v1.Caller, reservation *v1.Ref) (*v1.ReservationRelease, error) {
	if _, e := s.QueryReservation(ctx, c, reservation); e != nil {
		return nil, e
	}
	return s.store.(releaseStore).LoadReservationRelease(ctx, reservation)
}

type CompletionAuthority interface {
	QueryCompletionIntent(context.Context, *v1.Caller, *v1.Ref) (*v1.CompletionClosureIntent, error)
	QueryVerification(context.Context, *v1.Caller, *v1.Ref) (*v1.Verification, error)
	QueryAdmission(context.Context, *v1.Caller, *v1.Ref) (*v1.Admission, error)
}
type completionProofs interface {
	QueryCompletionSeal(context.Context, *v1.Caller, *v1.Ref) (*v1.CompletionSeal, error)
}

func (s *Service) WithCompletionAuthority(a CompletionAuthority) *Service {
	s.completionAuthority = a
	return s
}
func (s *Service) validateNoSend(ctx context.Context, caller *v1.Caller, r *v1.Reservation, ref *v1.Ref) (*v1.Ref, error) {
	proofs := s.usageSource.(ClosureEvidence)
	op, e := proofs.QueryOperation(ctx, caller, r.OperationId)
	if e != nil {
		return nil, e
	}
	if op != nil {
		if !proto.Equal(op.AdmissionRef, r.AdmissionRef) || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.GetEffect().GetOutcome() != "NOT_APPLIED" || op.GetEffect().GetLateEffect() != "RULED_OUT" {
			return nil, command.Fail("NO_SEND_UNPROVEN")
		}
		found := false
		for _, p := range op.ClosureEvidenceRefs {
			if proto.Equal(p, ref) {
				found = true
			}
		}
		if !found {
			return nil, command.Fail("INVALID_CLOSURE_PROOF")
		}
	}
	switch ref.GetName().GetObjectKind() {
	case "grant-exit-closure":
		proof, e := proofs.QueryGrantExitClosure(ctx, caller, ref)
		if e != nil {
			return nil, e
		}
		if proof == nil || proof.PhysicalSendWasPossible || !proto.Equal(proof.OperationId, r.OperationId) || op == nil || op.Execution == nil || !proto.Equal(proof.SendRef.Name, op.Execution.Send.Ref.Name) || op.Execution.Send.Phase != "CLOSED" {
			return nil, command.Fail("NO_SEND_UNPROVEN")
		}
		return proof.SendRef, nil
	case "completion-seal":
		seal, e := s.usageSource.(completionProofs).QueryCompletionSeal(ctx, caller, ref)
		if e != nil {
			return nil, e
		}
		if seal == nil || !seal.NoSendProven || seal.PhysicalSendWasPossible || !proto.Equal(seal.OperationId, r.OperationId) || !proto.Equal(seal.AdmissionRef, r.AdmissionRef) {
			return nil, command.Fail("NO_SEND_UNPROVEN")
		}
		intent, e := s.completionAuthority.QueryCompletionIntent(ctx, caller, seal.IntentRef)
		if e != nil {
			return nil, e
		}
		if intent == nil || !proto.Equal(intent.TaskId, r.TaskId) {
			return nil, command.Fail("INVALID_CLOSURE_PROOF")
		}
		c := intent.Command
		if c == nil || !proto.Equal(c.IntentRef, seal.IntentRef) || !proto.Equal(c.VerificationRef, seal.VerificationRef) || !proto.Equal(c.AdmissionRef, seal.AdmissionRef) || !proto.Equal(c.OperationId, seal.OperationId) || c.ExecutorEndpointId != seal.ExecutorEndpointId {
			return nil, command.Fail("INVALID_CLOSURE_PROOF")
		}
		verification, e := s.completionAuthority.QueryVerification(ctx, caller, seal.VerificationRef)
		if e != nil {
			return nil, e
		}
		listed := false
		if verification != nil && proto.Equal(verification.TaskId, r.TaskId) {
			for _, p := range verification.ClosureIntentRefs {
				if proto.Equal(p, seal.IntentRef) {
					listed = true
				}
			}
		}
		a, e := s.completionAuthority.QueryAdmission(ctx, caller, seal.AdmissionRef)
		if e != nil {
			return nil, e
		}
		if !listed || a == nil || !proto.Equal(a.TaskId, r.TaskId) || !proto.Equal(a.OperationId, r.OperationId) || a.ExecutorEndpointId != seal.ExecutorEndpointId || !proto.Equal(a.BudgetBasis.ReservationRef.Name, r.Ref.Name) {
			return nil, command.Fail("INVALID_CLOSURE_PROOF")
		}
		if op != nil && op.Execution != nil {
			if op.Execution.Send.Phase != "CLOSED" {
				return nil, command.Fail("NO_SEND_UNPROVEN")
			}
			return op.Execution.Send.Ref, nil
		}
		return nil, nil
	default:
		return nil, command.Fail("UNSUPPORTED_CLOSURE_PROOF")
	}
}
