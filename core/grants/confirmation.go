package grants

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) CheckConfirmationMatter(ctx context.Context, c *v1.Confirmation) error {
	if c.MatterType == "GRANT_ISSUANCE" {
		m := c.GetGrantIssuance()
		if m == nil || m.IssuanceRef == nil {
			return command.Fail("CONFIRMATION_INVALID")
		}
		if command.CheckName(&v1.Caller{UserId: s.user, IssuerId: s.trustedIssuer}, m.IssuanceRef.Name, s.user, s.domain, "grant-issuance") != nil {
			return command.Fail("CONFIRMATION_INVALID")
		}
		d, e := s.store.LoadCurrentGrantIssuance(ctx, m.IssuanceRef.Name)
		if e != nil {
			return e
		}
		_, now, e := s.store.Position(ctx)
		if e != nil {
			return e
		}
		if d == nil || !proto.Equal(d.Ref, m.IssuanceRef) || d.State != "DRAFT" || !proto.Equal(d.Grant, m.Grant) || now >= d.Grant.ValidUntilUnixMs {
			return command.Fail("CONFIRMATION_INVALID")
		}
		return nil
	}
	m := c.GetOperationAdmission()
	if c.MatterType != "OPERATION_ADMISSION" || m == nil || m.GrantRef == nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	g, e := s.store.LoadGrant(ctx, m.GrantRef)
	if e != nil {
		return e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	if g == nil || !proto.Equal(g.Ref, m.GrantRef) || g.Status != "ACTIVE" || now < g.ValidFromUnixMs || now >= g.ValidUntilUnixMs || !proto.Equal(g.Subject, m.TaskId) || m.Capability == nil {
		return command.Fail("GRANT_INVALID")
	}
	for _, p := range g.Permissions {
		if p.Action == m.Capability.Action && p.Resource == m.Capability.Resource && p.UseRight == m.Capability.UseRight && p.ProcessingPurpose == m.Capability.ProcessingPurpose && p.ExecutorEndpointId == m.Capability.ExecutorEndpointId && (p.ParameterMode == "ANY" || (p.ParameterMode == "EXACT" && proto.Equal(p.ParametersRef, m.ParametersRef))) {
			return nil
		}
	}
	return command.Fail("GRANT_SCOPE_MISMATCH")
}
