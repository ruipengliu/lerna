package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// PrepareResend 单独裁决一次安全重发；重放只返回原决定，不能产生新的发送身份。
func (s *Service) PrepareResend(ctx context.Context, caller *v1.Caller, c *v1.PrepareResendCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if e := command.CheckIdentity(caller, c.Header.Identity, s.user, s.domain); e != nil {
		return nil, e
	}
	if caller.IssuerId != "host" && caller.IssuerId != "local-cli" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("prepare-resend", c.OperationId, c.PreviousSendRef, c.Claim), "ledger.resend", func(tx context.Context) (*v1.Ref, error) {
		job, e := s.work.CheckExecutionClaimInTransaction(tx, c.Claim)
		if e != nil {
			return nil, e
		}
		if !proto.Equal(job.SpecificationRef.Name, c.OperationId) {
			return nil, command.Fail("STALE_CLAIM")
		}
		op, e := s.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil || op.Execution == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		x := op.Execution
		if !proto.Equal(x.Send.Ref, c.PreviousSendRef) {
			return nil, command.Fail("STALE_REFERENCE")
		}
		if x.Send.Phase != "OBSERVED" && x.Send.Phase != "DISPATCH_POSSIBLE" {
			return nil, command.Fail("RESEND_NOT_NEEDED")
		}
		if x.Send.SendSeq >= op.CapabilitySnapshot.MaxSends {
			return nil, command.Fail("SEND_BUDGET_EXCEEDED")
		}
		_, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return nil, e
		}
		if e = s.validateResend(tx, caller, op, now); e != nil {
			return nil, e
		}
		if e = s.CheckRecoveryAllowed(tx); e != nil {
			return nil, e
		}
		var queryObservation *v1.Ref
		if x.Attempt.Capabilities.Queryable {
			p, err := s.QueryReconciliation(tx, caller, op.Ref.Name)
			if err != nil {
				return nil, err
			}
			queryObservation = proto.Clone(p.LastObservationRef).(*v1.Ref)
		}
		x.PreviousSends = append(x.PreviousSends, proto.Clone(x.Send).(*v1.PhysicalSend))
		x.Send = &v1.PhysicalSend{Ref: command.NewRef(s.user, s.domain, "send", "lerna.v1.PhysicalSend"), AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq + 1, Phase: "REGISTERED", ProcessInstance: job.ProcessInstance, ClaimEpoch: job.ClaimEpoch, LeaseUntilUnixMs: job.LeaseUntilUnixMs, ResendQueryObservationRef: queryObservation}
		op.Ref.Revision++
		return x.Send.Ref, s.store.SaveOperation(tx, op)
	})
}

func (s *Service) validateResend(ctx context.Context, caller *v1.Caller, op *v1.Operation, now int64) error {
	if op.Dispatch != "OPEN" {
		return command.Fail("DISPATCH_SEALED")
	}
	if op.Effect.EvidenceConflict {
		return command.Fail("EVIDENCE_CONFLICT")
	}
	x := op.Execution
	if x == nil || s.adapter == nil {
		return command.Fail("UNSUPPORTED_CAPABILITY")
	}
	registered, e := s.starts.QueryCurrentCapability(ctx, caller, op.CapabilitySnapshot.Ref)
	if e != nil {
		return e
	}
	if registered == nil {
		return command.Fail("CAPABILITY_INVALID")
	}
	registered = proto.Clone(registered).(*v1.Capability)
	registered.Ref, registered.ApprovedBy = op.CapabilitySnapshot.Ref, op.CapabilitySnapshot.ApprovedBy
	if !proto.Equal(registered, op.CapabilitySnapshot) {
		return command.Fail("RESEND_BINDING_MISMATCH")
	}
	descriptor, current, e := s.adapter.Compile(op, x.Attempt)
	if e != nil {
		return e
	}
	original := x.Attempt.Capabilities
	if !proto.Equal(descriptor, x.CallDescriptor) || !proto.Equal(original, current) {
		return command.Fail("RESEND_BINDING_MISMATCH")
	}
	if original == nil || !original.Idempotent || original.IdempotencyMechanism != "NATIVE_KEY" || original.ConcurrencyGuarantee != "SAME_KEY_ALL_SENDS" || original.ParameterBinding != "EXACT_REQUEST" || original.AccountScope != s.user || original.VerificationBasis != "reference-target-v1" || original.IdempotencyScope != x.Attempt.ExternalKeyScope || descriptor.ExternalKey != x.Attempt.ExternalKey {
		return command.Fail("RESEND_UNSAFE")
	}
	if original.Queryable {
		p, e := s.QueryReconciliation(ctx, caller, op.Ref.Name)
		if e != nil {
			return e
		}
		if p == nil || !proto.Equal(p.OriginalAttemptRef.Name, x.Attempt.Ref.Name) || p.LastObservationRef == nil {
			return command.Fail("RESEND_QUERY_FIRST")
		}
		if x.Send.Phase == "REGISTERED" {
			if x.Send.ResendQueryObservationRef == nil {
				return command.Fail("RESEND_QUERY_FIRST")
			}
		} else if proto.Equal(p.LastObservationRef, x.Send.ResendQueryObservationRef) {
			return command.Fail("RESEND_QUERY_FIRST")
		}
	}
	if original.RetentionMs > 0 {
		if x.Attempt.KeyValidUntilUnixMs == nil || x.CallDescriptor.KeyValidUntilUnixMs == nil || x.Attempt.GetKeyValidUntilUnixMs() != x.CallDescriptor.GetKeyValidUntilUnixMs() {
			return command.Fail("RESEND_BINDING_MISMATCH")
		}
		until := x.Attempt.GetKeyValidUntilUnixMs()
		if now >= until {
			return command.Fail("RESEND_KEY_EXPIRED")
		}
		if now < until-original.RetentionMs || (x.Attempt.FirstPossibleSendAtUnixMs != 0 && now < x.Attempt.FirstPossibleSendAtUnixMs) {
			return command.Fail("RESEND_CLOCK_UNCERTAIN")
		}
		if !original.RejectsExpiredKeys {
			return command.Fail("RESEND_ARRIVAL_UNPROVEN")
		}
	}
	return nil
}
