package grants

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func managementPermissions(ctx context.Context, h *harness) error {
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	other, err := authorization.NewCredential()
	if err != nil {
		return err
	}
	id, err := h.s.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	now, _ := h.c.Now()
	_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "other", CredentialSha256: authorization.CredentialDigest(other), ExpiresUnix: now.Add(authConfig().CredentialTTL).Unix()}}}})
	if err != nil {
		return err
	}
	id, err = h.s.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "execution-only", Subject: "other", Scope: h.spec.Scope, Mode: "continuous"}}}})
	if err != nil {
		return err
	}
	req, err := h.request(ctx, "ISSUE", 4)
	if err != nil {
		return err
	}
	req.Spec.Subject = "other"
	if _, err = h.g.Mutate(ctx, other, req); !authorization.Is(err, authorization.Denied) {
		return expect(err, authorization.Denied)
	}
	req.Kind = "DERIVE"
	req.GrantId = root.GrantId
	req.ExpectedGrantRevision = 2
	req.Spec.DelegationDepth = 0
	req.Spec.Units = 1
	if _, err = h.g.Mutate(ctx, other, req); !authorization.Is(err, authorization.Denied) {
		return expect(err, authorization.Denied)
	}
	req.Kind = "REVOKE"
	req.Spec = nil
	if _, err = h.g.Mutate(ctx, other, req); !authorization.Is(err, authorization.Denied) {
		return expect(err, authorization.Denied)
	}
	if _, err = h.g.Get(ctx, other, root.GrantId); !authorization.Is(err, authorization.Denied) {
		return expect(err, authorization.Denied)
	}
	if _, err = h.g.LookupOperation(ctx, other, root.OperationId); !authorization.Is(err, authorization.Denied) {
		return expect(err, authorization.Denied)
	}
	_, err = h.g.Get(ctx, "admin", root.GrantId)
	return expect(err, authorization.Unauthenticated)
}
