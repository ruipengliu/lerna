package ledger

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type GrantClosures interface {
	ValidateGrantClosure(context.Context, *v1.Caller, *v1.CloseGrantExitCommand) error
	QueryCredential(context.Context, *v1.Caller, *v1.Ref) (*v1.ExitCredential, error)
	QueryCredentialUse(context.Context, *v1.Caller, *v1.Ref) (*v1.ExitCredentialUse, error)
}
type grantClosureStore interface {
	SaveGrantExitClosure(context.Context, *v1.GrantExitClosure) error
	LoadGrantExitClosure(context.Context, *v1.Ref) (*v1.GrantExitClosure, error)
}

func (s *Service) WithGrantClosures(g GrantClosures) *Service { s.grantClosures = g; return s }

// CloseForGrantRevocation 只在出口使用临界区退出后调用；封闭推进权不能删除既有外部效果。
func (s *Service) CloseForGrantRevocation(ctx context.Context, caller *v1.Caller, c *v1.CloseGrantExitCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "grants-revocation" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if s.grantClosures == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("close-grant-exit", c.CredentialRef, c.Binding, c.RevocationRef), "ledger.grant_closure", func(tx context.Context) (*v1.Ref, error) {
		if e := s.grantClosures.ValidateGrantClosure(tx, caller, c); e != nil {
			return nil, e
		}
		credential, e := s.grantClosures.QueryCredential(tx, caller, c.CredentialRef)
		if e != nil {
			return nil, e
		}
		use, e := s.grantClosures.QueryCredentialUse(tx, caller, c.CredentialRef)
		if e != nil {
			return nil, e
		}
		b := c.Binding
		if credential == nil || use == nil || b == nil || !proto.Equal(credential.Binding, b) || !proto.Equal(use.CredentialRef, c.CredentialRef) || use.ConsumedSendIdentity != fmt.Sprintf("%s/%d", b.GetAttemptId().GetLocalId(), b.SendSeq) {
			return nil, command.Fail("CREDENTIAL_INVALID")
		}
		op, e := s.QueryOperation(tx, caller, b.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil || op.Execution == nil || !proto.Equal(op.Execution.Attempt.Ref.Name, b.AttemptId) || !proto.Equal(op.AdmissionRef, b.AdmissionRef) || op.Execution.CallDescriptor.Digest != b.DescriptorDigest {
			return nil, command.Fail("CREDENTIAL_BINDING_MISMATCH")
		}
		x := op.Execution
		var send *v1.PhysicalSend
		for _, candidate := range append([]*v1.PhysicalSend{x.Send}, x.PreviousSends...) {
			if candidate.SendSeq == b.SendSeq {
				send = candidate
			}
		}
		if send == nil {
			return nil, command.Fail("CREDENTIAL_BINDING_MISMATCH")
		}
		possible := send.Phase != "REGISTERED" && send.Phase != "CLOSED"
		closure := &v1.GrantExitClosure{Ref: command.NewRef(s.user, s.domain, "grant-exit-closure", "lerna.v1.GrantExitClosure"), CredentialRef: c.CredentialRef, RevocationRef: c.RevocationRef, OperationId: b.OperationId, SendRef: proto.Clone(send.Ref).(*v1.Ref), PhysicalSendWasPossible: possible}
		op.Ref.Revision++
		op.Dispatch = "SEALED"
		op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, closure.Ref)
		if !possible {
			send.Ref.Revision++
			send.Phase = "CLOSED"
		}
		if !executionMayHaveSent(x) {
			x.Attempt.Ref.Revision++
			x.Attempt.Phase = "CLOSED"
			op.AttemptRefs = []*v1.Ref{x.Attempt.Ref}
			op.Effect.Ref.Revision++
			op.Effect.Outcome = "NOT_APPLIED"
			op.Effect.LateEffect = "RULED_OUT"
			op.EffectRef = op.Effect.Ref
			op.Lifecycle = "SETTLED"
		}
		jobs, e := s.store.LedgerJobs(tx, op.Ref.Name)
		if e != nil {
			return nil, e
		}
		for _, j := range jobs {
			if j.JobType != "EXECUTE_OPERATION" {
				continue
			}
			j.Ref.Revision++
			j.ProcessInstance = ""
			j.LeaseUntilUnixMs = 0
			if op.Lifecycle == "SETTLED" {
				j.State = "COMPLETED"
			} else {
				j.State = "WAITING"
			}
			if e = s.store.SaveLedgerJob(tx, j); e != nil {
				return nil, e
			}
		}
		if e = s.store.SaveOperation(tx, op); e != nil {
			return nil, e
		}
		return closure.Ref, s.store.(grantClosureStore).SaveGrantExitClosure(tx, closure)
	})
}
func (s *Service) QueryGrantExitClosure(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.GrantExitClosure, error) {
	if e := s.checkHistory(caller, r, "grant-exit-closure", "lerna.v1.GrantExitClosure"); e != nil {
		return nil, e
	}
	v, e := s.store.(grantClosureStore).LoadGrantExitClosure(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
