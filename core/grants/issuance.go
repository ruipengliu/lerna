package grants

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ConfirmationContent interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
}

func (s *Service) WithConfirmationContent(c ConfirmationContent) *Service {
	s.confirmationContent = c
	return s
}

type ConfirmationSessions interface {
	CreateConfirmationInTransaction(context.Context, *v1.Confirmation) (*v1.Confirmation, error)
	QueryCurrentConfirmation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Confirmation, error)
	ConsumeGrantConfirmationInTransaction(context.Context, *v1.Ref, *v1.GrantConfirmationMatter, *v1.Ref) error
}

func (s *Service) WithConfirmations(c ConfirmationSessions) *Service { s.confirmations = c; return s }
func (s *Service) RequestGrantConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RequestGrantConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("request-grant-confirmation", c.Grant, c.SessionId), "grants.confirmation", func(tx context.Context) (*v1.Ref, error) {
		if !s.userControl(caller) {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		g, e := s.confirmableGrant(caller, c.Grant)
		if e != nil {
			return nil, e
		}
		draft := &v1.GrantIssuance{Ref: command.NewRef(s.user, s.domain, "grant-issuance", "lerna.v1.GrantIssuance"), Grant: g, State: "DRAFT"}
		if e = s.store.SaveGrantIssuance(tx, draft); e != nil {
			return nil, e
		}
		parameters := make([]command.ParameterDescription, len(g.Permissions))
		for i, p := range g.Permissions {
			if p.ParameterMode != "EXACT" {
				continue
			}
			if e = s.confirmationContent.CheckUsable(tx, caller, p.ParametersRef); e != nil {
				return nil, e
			}
			body, readErr := s.confirmationContent.Read(tx, caller, p.ParametersRef)
			if readErr != nil {
				return nil, readErr
			}
			if body == nil {
				return nil, command.Fail("CONTENT_UNUSABLE")
			}
			parameters[i] = command.DescribeParameters(body)
		}
		description, e := json.Marshal(struct {
			Matter     string
			Scope      *v1.Grant
			Parameters []command.ParameterDescription
		}{"GRANT_ISSUANCE", g, parameters})
		if e != nil {
			return nil, e
		}
		confirmation, e := s.confirmations.CreateConfirmationInTransaction(tx, &v1.Confirmation{SessionId: c.SessionId, MatterType: "GRANT_ISSUANCE", Description: string(description), ExpiresAtUnixMs: g.ValidUntilUnixMs, Matter: &v1.Confirmation_GrantIssuance{GrantIssuance: &v1.GrantConfirmationMatter{IssuanceRef: draft.Ref, Grant: g}}})
		if e != nil {
			return nil, e
		}
		return confirmation.Ref, nil
	})
}
func (s *Service) confirmableGrant(caller *v1.Caller, g *v1.Grant) (*v1.Grant, error) {
	if unsupportedGrant(g) {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if g == nil || g.Ref != nil || g.Issuer != nil || g.Status != "" || g.RevocationEpoch != 0 || g.RevocationCompletion != "" || g.RevocationRef != nil || g.UsePoolId != "" || g.SemanticVersion > 1 || g.ValidFromUnixMs <= 0 || g.ValidUntilUnixMs <= g.ValidFromUnixMs || len(g.Permissions) == 0 || (g.UseMode != "SINGLE" && g.UseMode != "CONTINUOUS") || (g.UseMode == "SINGLE" && g.MaxAdmissions != 1) {
		return nil, command.Fail("INVALID_GRANT")
	}
	if e := command.CheckName(caller, g.Subject, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	for _, p := range g.Permissions {
		if p == nil || p.Action == "" || p.Resource == "" || p.ExecutorEndpointId == "" || !supportedPermission(p) || p.ProcessingPurpose != "CURRENT_TASK" {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if (p.ParameterMode != "ANY" && p.ParameterMode != "EXACT") || (p.ParameterMode == "EXACT" && p.ParametersRef == nil) || (p.ParameterMode == "ANY" && p.ParametersRef != nil) || (g.UseMode == "SINGLE" && p.ParameterMode != "EXACT") {
			return nil, command.Fail("INVALID_GRANT")
		}
		if p.ParametersRef != nil && (command.CheckName(caller, p.ParametersRef.Name, s.user, s.domain+"/content", "content") != nil || p.ParametersRef.Revision == 0 || p.ParametersRef.SchemaId != "lerna.v1.Content") {
			return nil, command.Fail("INVALID_GRANT")
		}
	}
	g = proto.Clone(g).(*v1.Grant)
	g.SemanticVersion = 1
	return g, nil
}
func (s *Service) userControl(c *v1.Caller) bool {
	return c.GetUserId() == s.user && (c.GetIssuerId() == s.trustedIssuer || c.GetIssuerId() == "local-cli")
}
func (s *Service) IssueGrant(ctx context.Context, caller *v1.Caller, c *v1.IssueGrantCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("issue-grant", c.ConfirmationRef), "grants.configure", func(tx context.Context) (*v1.Ref, error) {
		if !s.userControl(caller) {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.ConfirmationRef == nil {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		confirmation, e := s.confirmations.QueryCurrentConfirmation(tx, caller, c.ConfirmationRef.Name)
		if e != nil {
			return nil, e
		}
		if confirmation == nil || !proto.Equal(confirmation.Ref, c.ConfirmationRef) || confirmation.State != "APPROVED" || confirmation.MatterType != "GRANT_ISSUANCE" || confirmation.GetGrantIssuance() == nil {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		if e = s.CheckConfirmationMatter(tx, confirmation); e != nil {
			return nil, e
		}
		matter := confirmation.GetGrantIssuance()
		draft, e := s.store.LoadCurrentGrantIssuance(tx, matter.IssuanceRef.Name)
		if e != nil {
			return nil, e
		}
		g := proto.Clone(draft.Grant).(*v1.Grant)
		g.Ref = command.NewRef(s.user, s.domain, "grant", "lerna.v1.Grant")
		g.UsePoolId = g.Ref.Name.LocalId
		g.Issuer = confirmation.RespondedBy
		g.Status = "ACTIVE"
		draft.Ref.Revision++
		draft.State = "ISSUED"
		draft.GrantRef = g.Ref
		if e = s.confirmations.ConsumeGrantConfirmationInTransaction(tx, c.ConfirmationRef, matter, draft.Ref); e != nil {
			return nil, e
		}
		if e = s.store.SaveGrant(tx, g); e != nil {
			return nil, e
		}
		return g.Ref, s.store.SaveGrantIssuance(tx, draft)
	})
}
func (s *Service) QueryGrantIssuance(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.GrantIssuance, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "grant-issuance"); e != nil {
		return nil, e
	}
	d, e := s.store.LoadGrantIssuance(ctx, r)
	if e == nil && d != nil && !proto.Equal(d.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return d, e
}

func unsupportedGrant(g *v1.Grant) bool {
	return g != nil && (g.ParentGrantRef != nil || len(g.SourceGrantRefs) > 0 || (g.DelegationMode != "" && g.DelegationMode != "NONE"))
}
