package grants

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Admissions interface {
	QueryAdmission(context.Context, *v1.Caller, *v1.Ref) (*v1.Admission, error)
}

func (s *Service) WithAdmissions(a Admissions) *Service { s.admissions = a; return s }

// IssueCredential 从已持久准入签发不透明引用，不能据此跳过开始门禁。
func (s *Service) IssueCredential(ctx context.Context, caller *v1.Caller, c *v1.IssueExitCredentialCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("issue-exit-credential", c.AdmissionRef, c.Binding, c.ExpiresAtUnixMs), "grants.credential", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != s.trustedIssuer {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if s.admissions == nil {
			return nil, command.Fail("AUTHORITY_UNREACHABLE")
		}
		a, e := s.admissions.QueryAdmission(tx, caller, c.AdmissionRef)
		if e != nil {
			return nil, e
		}
		g, e := s.checkExit(tx, c.Binding, a)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		if c.ExpiresAtUnixMs <= now || c.ExpiresAtUnixMs > g.ValidUntilUnixMs {
			return nil, command.Fail("CREDENTIAL_INVALID")
		}
		cred := &v1.ExitCredential{Ref: command.NewRef(s.user, s.domain, "credential", "lerna.v1.ExitCredential"), Binding: proto.Clone(c.Binding).(*v1.ExitCredentialBinding), IssuedAtUnixMs: now, ExpiresAtUnixMs: c.ExpiresAtUnixMs, RevocationEpoch: g.RevocationEpoch, State: "ISSUED"}
		if e = s.store.SaveExitCredential(tx, cred); e != nil {
			return nil, e
		}
		return cred.Ref, s.store.SaveTraceSource(tx, "grants", &v1.TraceEvent{EventType: "CREDENTIAL_ISSUED", SourceRecordRef: cred.Ref, TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: c.Binding.AttemptId, RelatedRefs: []*v1.Ref{a.Ref, a.GrantUseRef}})
	})
}
func (s *Service) QueryCredential(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.ExitCredential, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "credential"); e != nil {
		return nil, e
	}
	c, e := s.store.LoadExitCredential(ctx, r)
	if e == nil && c != nil && !proto.Equal(c.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return c, e
}
func (s *Service) checkExit(ctx context.Context, b *v1.ExitCredentialBinding, a *v1.Admission) (*v1.Grant, error) {
	if b == nil || a == nil || b.UserId != s.user || b.Audience != "egress" || b.CallerIssuerId != "egress" || b.ExecutorInstance == "" || b.DescriptorDigest == "" || b.SendSeq == 0 || b.AttemptId == nil || b.AttemptId.UserId != s.user || b.AttemptId.AuthorityDomainId != a.LedgerDomainId || b.AttemptId.ObjectKind != "attempt" || b.AttemptId.LocalId == "" || !proto.Equal(b.TaskId, a.TaskId) || !proto.Equal(b.SubjectId, a.TaskId) || !proto.Equal(b.OperationId, a.OperationId) || !proto.Equal(b.AdmissionRef, a.Ref) || !proto.Equal(b.GrantUseRef, a.GrantUseRef) || b.ExecutorEndpointId != a.ExecutorEndpointId || b.RequirementsVersion != a.RequirementsVersion || b.InputVersion != a.InputVersion || b.ControlGeneration != a.ControlGeneration || a.BudgetBasis == nil || !proto.Equal(b.BudgetReservationRef, a.BudgetBasis.ReservationRef) || a.CapabilitySnapshot == nil || b.UseRight != a.CapabilitySnapshot.UseRight || b.ProcessingPurpose != a.CapabilitySnapshot.ProcessingPurpose || (b.UseRight != "INVOKE" && (a.CapabilitySnapshot.Action != "QUERY" || b.UseRight != "READ")) || b.ProcessingPurpose != "CURRENT_TASK" {
		return nil, command.Fail("CREDENTIAL_BINDING_MISMATCH")
	}
	u, e := s.store.LoadGrantUse(ctx, a.OperationId)
	if e != nil {
		return nil, e
	}
	if u == nil || !proto.Equal(u.Ref, a.GrantUseRef) || !proto.Equal(u.AdmissionRef, a.Ref) || !proto.Equal(u.OperationId, a.OperationId) || !proto.Equal(u.TaskId, a.TaskId) || len(a.GrantRefs) != 1 || !proto.Equal(u.GrantRef, a.GrantRefs[0]) {
		return nil, command.Fail("GRANT_INVALID")
	}
	g, e := s.store.LoadGrant(ctx, u.GrantRef)
	if e != nil {
		return nil, e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return nil, e
	}
	if g == nil || g.Status != "ACTIVE" || now < g.ValidFromUnixMs || now >= g.ValidUntilUnixMs || g.SemanticVersion != 1 || !proto.Equal(g.Subject, a.TaskId) || g.UsePoolId != u.UsePoolId {
		return nil, command.Fail("GRANT_INVALID")
	}
	if !coversCapability(g, a.CapabilitySnapshot, a.ParametersRef) {
		return nil, command.Fail("GRANT_SCOPE_MISMATCH")
	}
	return g, nil
}

// ConsumeCredentialInTransaction 仅由受信开始门禁在同一裁决事务消费；不重新占用授权次数。
func (s *Service) ConsumeCredentialInTransaction(ctx context.Context, caller *v1.Caller, r *v1.Ref, b *v1.ExitCredentialBinding, a *v1.Admission) error {
	if caller.GetUserId() != s.user || caller.GetIssuerId() != "egress" {
		return command.Fail("PERMISSION_DENIED")
	}
	c, e := s.QueryCredential(ctx, caller, r)
	if e != nil {
		return e
	}
	if c == nil || c.State != "ISSUED" || !proto.Equal(c.Binding, b) {
		return command.Fail("CREDENTIAL_INVALID")
	}
	g, e := s.checkExit(ctx, b, a)
	if e != nil {
		return e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	if now >= c.ExpiresAtUnixMs || c.RevocationEpoch != g.RevocationEpoch {
		return command.Fail("CREDENTIAL_INVALID")
	}
	u, e := s.store.LoadExitCredentialUse(ctx, r)
	if e != nil {
		return e
	}
	if u != nil {
		return command.Fail("CREDENTIAL_CONSUMED")
	}
	use := &v1.ExitCredentialUse{Ref: command.NewRef(s.user, s.domain, "credential-use", "lerna.v1.ExitCredentialUse"), CredentialRef: c.Ref, ConsumedSendIdentity: fmt.Sprintf("%s/%d", b.AttemptId.LocalId, b.SendSeq), ConsumedAtUnixMs: now}
	if e = s.store.SaveExitCredentialUse(ctx, use); e != nil {
		return e
	}
	return s.store.SaveTraceSource(ctx, "grants", &v1.TraceEvent{EventType: "CREDENTIAL_CONSUMED", SourceRecordRef: use.Ref, TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: b.AttemptId, RelatedRefs: []*v1.Ref{c.Ref, a.Ref, a.GrantUseRef}})
}

// QueryCredentialUse 返回单独的不可变消费事实，不改写签发引用。
func (s *Service) QueryCredentialUse(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.ExitCredentialUse, error) {
	c, e := s.QueryCredential(ctx, caller, r)
	if e != nil || c == nil {
		return nil, e
	}
	return s.store.LoadExitCredentialUse(ctx, r)
}
