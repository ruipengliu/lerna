package grants

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) QueryCurrentGrant(ctx context.Context, caller *v1.Caller, n *v1.GlobalName) (*v1.Grant, error) {
	if e := command.CheckName(caller, n, s.user, s.domain, "grant"); e != nil {
		return nil, e
	}
	return s.store.LoadGrant(ctx, &v1.Ref{Name: n})
}
func (s *Service) QueryRevocation(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.GrantRevocation, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "grant-revocation"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadGrantRevocation(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryCurrentRevocation(ctx context.Context, caller *v1.Caller, n *v1.GlobalName) (*v1.GrantRevocation, error) {
	if e := command.CheckName(caller, n, s.user, s.domain, "grant-revocation"); e != nil {
		return nil, e
	}
	return s.store.LoadCurrentGrantRevocation(ctx, n)
}

// Revoke 与开始门禁在同一裁决顺序排先后；消费过的凭据仍等待原出口封闭。
func (s *Service) Revoke(ctx context.Context, caller *v1.Caller, c *v1.RevokeGrantCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("revoke-grant", c.GrantId, c.ExpectedRevision), "grants.revoke", func(tx context.Context) (*v1.Ref, error) {
		if !s.userControl(caller) {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		g, e := s.QueryCurrentGrant(tx, caller, c.GrantId)
		if e != nil {
			return nil, e
		}
		if g == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if c.ExpectedRevision != nil && *c.ExpectedRevision != g.Ref.Revision {
			return nil, command.Fail("REVISION_CONFLICT")
		}
		if g.Status == "REVOKED" {
			return g.RevocationRef, nil
		}
		g.Ref.Revision++
		g.Status = "REVOKED"
		g.RevocationEpoch++
		g.RevocationCompletion = "PENDING"
		rev := &v1.GrantRevocation{Ref: command.NewRef(s.user, s.domain, "grant-revocation", "lerna.v1.GrantRevocation"), GrantRef: g.Ref, Status: "PENDING", AcceptedBy: c.Header.Identity}
		credentials, e := s.store.ExitCredentials(tx)
		if e != nil {
			return nil, e
		}
		for _, cred := range credentials {
			use, e := s.store.LoadGrantUse(tx, cred.Binding.OperationId)
			if e != nil {
				return nil, e
			}
			if use == nil || !proto.Equal(use.GrantRef.Name, g.Ref.Name) {
				continue
			}
			consumed, e := s.store.LoadExitCredentialUse(tx, cred.Ref)
			if e != nil {
				return nil, e
			}
			if consumed == nil {
				continue
			}
			header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: "grants-revocation", TargetDomainId: cred.Binding.OperationId.AuthorityDomainId, CommandId: rev.Ref.Name.LocalId + ":" + cred.Ref.Name.LocalId}, ContractVersion: 1, SchemaId: "lerna.v1.AdmissionCommands", FingerprintVersion: 1}
			rev.Closures = append(rev.Closures, &v1.GrantExitClosureRequest{Command: &v1.CloseGrantExitCommand{Header: header, CredentialRef: cred.Ref, Binding: cred.Binding, RevocationRef: rev.Ref}})
		}
		if len(rev.Closures) == 0 {
			rev.Status = "COMPLETE"
			g.RevocationCompletion = "COMPLETE"
		}
		g.RevocationRef = rev.Ref
		if e = s.store.SaveGrant(tx, g); e != nil {
			return nil, e
		}
		return rev.Ref, s.store.SaveGrantRevocation(tx, rev)
	})
}

type RevocationExits interface {
	CloseForGrantRevocation(context.Context, *v1.Caller, *v1.CloseGrantExitCommand) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}

func (s *Service) WithRevocationExits(exits RevocationExits) *Service {
	s.revocationExits = exits
	return s
}
func authorityUnavailable() error {
	return &command.Failure{Detail: &v1.ContractError{Code: "AUTHORITY_UNREACHABLE", Category: v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, CommandAcceptance: v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, RecoveryAction: "QUERY_OR_RETRY_ORIGINAL"}}
}

// ProcessRevocations 恢复已保存的封闭工作；失联保留原交接，不猜测出口停止。
func (s *Service) ProcessRevocations(ctx context.Context) error {
	pending, e := s.store.PendingGrantRevocations(ctx)
	if e != nil {
		return e
	}
	if len(pending) > 0 && s.revocationExits == nil {
		return authorityUnavailable()
	}
	caller := &v1.Caller{UserId: s.user, IssuerId: "grants-revocation"}
	for _, rev := range pending {
		for _, closure := range rev.Closures {
			if closure.RecipientReceipt != nil {
				continue
			}
			c := closure.Command
			q, e := s.revocationExits.QueryReceipt(ctx, caller, c.Header.Identity)
			if e != nil {
				return e
			}
			if q == nil {
				return authorityUnavailable()
			}
			var receipt *v1.CommandReceipt
			switch q.State {
			case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
				receipt = q.Receipt
			case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
				receipt, e = s.revocationExits.CloseForGrantRevocation(ctx, caller, c)
				if e != nil {
					return e
				}
			default:
				return authorityUnavailable()
			}
			if !validClosureReceipt(c, receipt) {
				return command.Fail("HANDOFF_RECEIPT_INVALID")
			}
			if e = s.recordClosure(ctx, rev.Ref.Name, c, receipt); e != nil {
				return e
			}
		}
	}
	return nil
}
func validClosureReceipt(c *v1.CloseGrantExitCommand, r *v1.CommandReceipt) bool {
	return r != nil && proto.Equal(r.Identity, c.Header.Identity) && r.Decision == v1.Decision_DECISION_ACCEPTED && r.Phase == v1.ReceiptPhase_RECEIPT_PHASE_DECIDED && r.ResponsibleDomainId == c.Header.Identity.TargetDomainId && r.FingerprintVersion == 1 && r.Fingerprint == command.SemanticFingerprint("close-grant-exit", c.CredentialRef, c.Binding, c.RevocationRef) && r.ResultRef != nil && r.ResultRef.Name != nil && r.ResultRef.Name.UserId == c.Header.Identity.UserId && r.ResultRef.Name.AuthorityDomainId == c.Header.Identity.TargetDomainId && r.ResultRef.Name.ObjectKind == "grant-exit-closure" && r.ResultRef.SchemaId == "lerna.v1.GrantExitClosure" && r.ResultRef.Revision > 0
}
func (s *Service) recordClosure(ctx context.Context, name *v1.GlobalName, c *v1.CloseGrantExitCommand, receipt *v1.CommandReceipt) error {
	caller := &v1.Caller{UserId: s.user, IssuerId: "grants-revocation"}
	header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: caller.IssuerId, TargetDomainId: s.domain, CommandId: "ack:" + c.Header.Identity.CommandId}, ContractVersion: 1, SchemaId: "lerna.v1.AdmissionCommands", FingerprintVersion: 1}
	r, e := s.decisions.Execute(ctx, caller, header, command.SemanticFingerprint("grant-revocation-receipt", name, c, receipt), "grants.revocation_receipt", func(tx context.Context) (*v1.Ref, error) {
		rev, e := s.store.LoadCurrentGrantRevocation(tx, name)
		if e != nil {
			return nil, e
		}
		if rev == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		found := false
		for _, closure := range rev.Closures {
			if proto.Equal(closure.Command, c) {
				if closure.RecipientReceipt != nil && !proto.Equal(closure.RecipientReceipt, receipt) {
					return nil, command.Fail("INVARIANT_VIOLATION")
				}
				closure.RecipientReceipt = receipt
				found = true
			}
		}
		if !found {
			return nil, command.Fail("HANDOFF_RECEIPT_INVALID")
		}
		rev.Ref.Revision++
		complete := true
		for _, closure := range rev.Closures {
			if closure.RecipientReceipt == nil {
				complete = false
			}
		}
		if complete {
			rev.Status = "COMPLETE"
			g, e := s.store.LoadGrant(tx, rev.GrantRef)
			if e != nil {
				return nil, e
			}
			if g == nil || g.Status != "REVOKED" || g.RevocationRef == nil || !proto.Equal(g.RevocationRef.Name, rev.Ref.Name) {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			g.Ref.Revision++
			g.RevocationCompletion = "COMPLETE"
			g.RevocationRef = rev.Ref
			if e = s.store.SaveGrant(tx, g); e != nil {
				return nil, e
			}
		}
		return rev.Ref, s.store.SaveGrantRevocation(tx, rev)
	})
	if e != nil {
		return e
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return &command.Failure{Detail: r.Error}
	}
	return nil
}

// ValidateGrantClosure 只认可原撤销事实中完整固定的封闭命令。
func (s *Service) ValidateGrantClosure(ctx context.Context, caller *v1.Caller, c *v1.CloseGrantExitCommand) error {
	if caller.GetUserId() != s.user || caller.GetIssuerId() != "grants-revocation" {
		return command.Fail("PERMISSION_DENIED")
	}
	if c == nil || c.RevocationRef == nil {
		return command.Fail("REVOCATION_INVALID")
	}
	rev, e := s.QueryRevocation(ctx, caller, c.RevocationRef)
	if e != nil {
		return e
	}
	if rev == nil {
		return command.Fail("REVOCATION_INVALID")
	}
	for _, closure := range rev.Closures {
		if proto.Equal(closure.Command, c) {
			return nil
		}
	}
	return command.Fail("REVOCATION_INVALID")
}
