// Package grants 是授权与使用记录的唯一写入方。
package grants

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Store interface {
	ReadAuthorityTime(context.Context) (int64, error)
	PendingGrantRevocations(context.Context) ([]*v1.GrantRevocation, error)
	LoadGrantVersion(context.Context, *v1.Ref) (*v1.Grant, error)
	SaveGrantRevocation(context.Context, *v1.GrantRevocation) error
	LoadGrantRevocation(context.Context, *v1.Ref) (*v1.GrantRevocation, error)
	LoadCurrentGrantRevocation(context.Context, *v1.GlobalName) (*v1.GrantRevocation, error)
	ExitCredentials(context.Context) ([]*v1.ExitCredential, error)
	SaveGrantIssuance(context.Context, *v1.GrantIssuance) error
	LoadGrantIssuance(context.Context, *v1.Ref) (*v1.GrantIssuance, error)
	LoadCurrentGrantIssuance(context.Context, *v1.GlobalName) (*v1.GrantIssuance, error)
	GrantUseCount(context.Context, string) (uint64, error)
	SaveExitCredentialUse(context.Context, *v1.ExitCredentialUse) error
	LoadExitCredentialUse(context.Context, *v1.Ref) (*v1.ExitCredentialUse, error)
	SaveExitCredential(context.Context, *v1.ExitCredential) error
	LoadExitCredential(context.Context, *v1.Ref) (*v1.ExitCredential, error)
	GrantUses(context.Context, *v1.GlobalName) ([]*v1.GrantUse, error)
	SaveGrant(context.Context, *v1.Grant) error
	LoadGrant(context.Context, *v1.Ref) (*v1.Grant, error)
	SaveGrantUse(context.Context, *v1.GrantUse) error
	LoadGrantUse(context.Context, *v1.GlobalName) (*v1.GrantUse, error)
	Position(context.Context) (uint64, int64, error)
}
type Decisions interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}
type Service struct {
	confirmationContent         ConfirmationContent
	revocationExits             RevocationExits
	confirmations               ConfirmationSessions
	admissions                  Admissions
	store                       Store
	decisions                   Decisions
	user, domain, trustedIssuer string
}

func New(s Store, d Decisions, user, domain, trustedIssuer string) *Service {
	return &Service{store: s, decisions: d, user: user, domain: domain, trustedIssuer: trustedIssuer}
}
func (s *Service) Configure(ctx context.Context, caller *v1.Caller, c *v1.ConfigureGrantCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("configure-grant", c.Grant), "grants.configure", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != s.trustedIssuer {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		g := c.Grant
		if unsupportedGrant(g) {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if g == nil || g.Ref != nil || g.Issuer != nil || g.Status != "" || g.RevocationEpoch != 0 || g.RevocationCompletion != "" || g.RevocationRef != nil || g.SemanticVersion > 1 || (g.UseMode != "CONTINUOUS" && g.UseMode != "SINGLE") || (g.UseMode == "SINGLE" && g.MaxAdmissions != 1) || g.ValidFromUnixMs <= 0 || g.ValidUntilUnixMs <= g.ValidFromUnixMs || len(g.Permissions) == 0 {
			return nil, command.Fail("INVALID_GRANT")
		}
		if e := command.CheckName(caller, g.Subject, s.user, s.domain, "task"); e != nil {
			return nil, e
		}
		for _, p := range g.Permissions {
			if p == nil || p.Action == "" || p.Resource == "" || p.ExecutorEndpointId == "" || !supportedPermission(p) || p.ProcessingPurpose != "CURRENT_TASK" {
				return nil, command.Fail("UNSUPPORTED_FEATURE")
			}
		}
		g = proto.Clone(g).(*v1.Grant)
		g.Ref = command.NewRef(s.user, s.domain, "grant", "lerna.v1.Grant")
		g.UsePoolId = g.Ref.Name.LocalId
		for _, p := range g.Permissions {
			if p.ParametersRef != nil {
				if command.CheckName(caller, p.ParametersRef.Name, s.user, s.domain+"/content", "content") != nil || p.ParametersRef.Revision == 0 || p.ParametersRef.SchemaId != "lerna.v1.Content" {
					return nil, command.Fail("INVALID_GRANT")
				}
			}
			if p.ParameterMode == "" && g.UseMode == "CONTINUOUS" {
				p.ParameterMode = "ANY"
			}
			if (p.ParameterMode != "ANY" && p.ParameterMode != "EXACT") || (p.ParameterMode == "EXACT" && p.ParametersRef == nil) || (p.ParameterMode == "ANY" && p.ParametersRef != nil) || (g.UseMode == "SINGLE" && p.ParameterMode != "EXACT") {
				return nil, command.Fail("INVALID_GRANT")
			}
		}
		g.Issuer = c.Header.Identity
		g.Status = "ACTIVE"
		g.SemanticVersion = 1
		return g.Ref, s.store.SaveGrant(tx, g)
	})
}

// OccupyInTransaction 在调用者的裁决事务中建立 operation 绑定的使用事实。
func (s *Service) OccupyInTransaction(ctx context.Context, grant *v1.Ref, task, operation *v1.GlobalName, admission *v1.Ref, cap *v1.Capability, parameters *v1.Ref) (*v1.GrantUse, bool, error) {
	caller := &v1.Caller{UserId: s.user, IssuerId: s.trustedIssuer}
	if grant == nil || command.CheckName(caller, grant.Name, s.user, s.domain, "grant") != nil {
		return nil, false, command.Fail("GRANT_INVALID")
	}
	g, e := s.store.LoadGrant(ctx, grant)
	if e != nil {
		return nil, false, e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return nil, false, e
	}
	if g == nil || !proto.Equal(g.Ref, grant) || g.Status != "ACTIVE" || g.SemanticVersion != 1 || g.Issuer == nil || (g.Issuer.IssuerId != s.trustedIssuer && g.Issuer.IssuerId != "local-cli") || (g.UseMode != "CONTINUOUS" && g.UseMode != "SINGLE") || !proto.Equal(g.Subject, task) || now < g.ValidFromUnixMs || now >= g.ValidUntilUnixMs {
		return nil, false, command.Fail("GRANT_INVALID")
	}
	if !coversCapability(g, cap, parameters) {
		return nil, false, command.Fail("GRANT_SCOPE_MISMATCH")
	}
	count, e := s.store.GrantUseCount(ctx, g.UsePoolId)
	if e != nil {
		return nil, false, e
	}
	if g.MaxAdmissions > 0 && count >= g.MaxAdmissions {
		return nil, false, command.Fail("GRANT_EXHAUSTED")
	}
	use := &v1.GrantUse{Ref: command.NewRef(s.user, s.domain, "grant-use", "lerna.v1.GrantUse"), GrantRef: g.Ref, UsePoolId: g.UsePoolId, OperationId: operation, AdmissionRef: admission, TaskId: task}
	return use, g.ConfirmationRequired, s.store.SaveGrantUse(ctx, use)
}
func (s *Service) QueryGrant(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Grant, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "grant"); e != nil {
		return nil, e
	}
	g, e := s.store.LoadGrantVersion(ctx, r)
	if e == nil && g != nil && !proto.Equal(g.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return g, e
}
func (s *Service) QueryUse(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.GrantUse, error) {
	if e := command.CheckName(c, id, s.user, s.domain+"/ledger", "operation"); e != nil {
		return nil, e
	}
	return s.store.LoadGrantUse(ctx, id)
}

func (s *Service) QueryUses(ctx context.Context, c *v1.Caller, id *v1.GlobalName) ([]*v1.GrantUse, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.GrantUses(ctx, id)
}
