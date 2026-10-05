package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type StartFacts interface {
	QueryAdmission(context.Context, *v1.Caller, *v1.Ref) (*v1.Admission, error)
	QueryStart(context.Context, *v1.Caller, *v1.Ref) (*v1.StartRecord, error)
	QueryStartReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

func (s *Service) WithStarts(facts StartFacts) *Service { s.starts = facts; return s }

// RecordDispatch 只有本次新提交成功才返回新的物理发送权；原回执的重放不能再次放行 I/O。
func (s *Service) RecordDispatch(ctx context.Context, caller *v1.Caller, c *v1.DispatchCommand) (*v1.CommandReceipt, bool, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, false, e
	}
	if caller.GetIssuerId() != "egress" {
		return nil, false, command.Fail("PERMISSION_DENIED")
	}
	fresh := false
	r, e := s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("dispatch", c.OperationId, c.StartReceipt), "ledger.dispatch", func(tx context.Context) (*v1.Ref, error) {
		job, e := s.work.CheckExecutionClaimInTransaction(tx, c.Claim)
		if e != nil {
			return nil, e
		}
		op, e := s.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil || op.Execution == nil || !proto.Equal(job.SpecificationRef.Name, c.OperationId) {
			return nil, command.Fail("INVALID_OPERATION")
		}
		x := op.Execution
		if op.Dispatch != "OPEN" || x.Send.Phase != "REGISTERED" {
			return nil, command.Fail("DISPATCH_CLOSED")
		}
		if c.Header.Identity.CommandId != "dispatch:"+x.Send.Ref.Name.LocalId {
			return nil, command.Fail("INVALID_SEND_IDENTITY")
		}
		if c.StartReceipt == nil || c.StartReceipt.Decision != v1.Decision_DECISION_ACCEPTED || c.StartReceipt.ResultRef == nil {
			return nil, command.Fail("START_RECEIPT_INVALID")
		}
		q, e := s.starts.QueryStartReceipt(tx, caller, c.StartReceipt.Identity)
		if e != nil {
			return nil, e
		}
		if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt, c.StartReceipt) {
			return nil, command.Fail("START_RECEIPT_INVALID")
		}
		start, e := s.starts.QueryStart(tx, caller, c.StartReceipt.ResultRef)
		if e != nil {
			return nil, e
		}
		if start == nil || !proto.Equal(start.Binding.OperationId, op.Ref.Name) || !proto.Equal(start.Binding.AttemptId, x.Attempt.Ref.Name) || start.Binding.SendSeq != x.Send.SendSeq || start.Binding.DescriptorDigest != x.CallDescriptor.Digest || !proto.Equal(start.SendRef.Name, x.Send.Ref.Name) {
			return nil, command.Fail("START_RECEIPT_INVALID")
		}
		if e = s.CheckRecoveryAllowed(tx); e != nil {
			return nil, e
		}
		_, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return nil, e
		}
		x.Send.Ref.Revision++
		x.Send.Phase = "DISPATCH_POSSIBLE"
		x.Send.StartReceipt = c.StartReceipt
		x.Send.CredentialRef = start.CredentialRef
		x.Send.ObservationRef = command.NewRef(s.user, s.domain, "observation", "lerna.v1.RawObservation")
		x.Send.ProcessInstance = job.ProcessInstance
		x.Send.ClaimEpoch = job.ClaimEpoch
		x.Send.LeaseUntilUnixMs = job.LeaseUntilUnixMs
		x.Attempt.Ref.Revision++
		x.Attempt.Phase = "DISPATCH_POSSIBLE"
		x.Attempt.FirstPossibleSendAtUnixMs = now
		op.AttemptRefs = []*v1.Ref{x.Attempt.Ref}
		op.StartReceiptObtained = true
		op.Ref.Revision++
		op.Effect.Ref.Revision++
		op.Effect.Outcome = "UNKNOWN"
		op.Effect.LateEffect = "MAY_OCCUR"
		op.EffectRef = op.Effect.Ref
		if e = s.store.SaveOperation(tx, op); e != nil {
			return nil, e
		}
		fresh = true
		return x.Send.Ref, nil
	})
	if e != nil {
		return nil, false, e
	}
	return r, fresh && r.Decision == v1.Decision_DECISION_ACCEPTED, nil
}
