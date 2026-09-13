// Package localauth validates the real local authorization implementation.
// Its controllable clock and crash probes are verification machinery only.
package localauth

import (
	"context"
	"fmt"
	"lerna/adapters/authlocal"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"os"
	"path/filepath"
	"time"
)

type clock struct{ at time.Time }

func (c *clock) Now() (time.Time, error) { return c.at, nil }
func Config() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}

type harness struct {
	root        string
	store       *sqliteauth.Store
	service     *authorization.Service
	clock       *clock
	admin, user string
	config      authorization.Config
}

func newHarness(ctx context.Context, config authorization.Config) (*harness, error) {
	root, err := os.MkdirTemp("", "lerna-auth-")
	if err != nil {
		return nil, err
	}
	h := &harness{root: root, config: config, clock: &clock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}}
	if err := h.reopen(); err != nil {
		h.close()
		return nil, err
	}
	h.admin, err = h.service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		h.close()
		return nil, err
	}
	h.user, err = authorization.NewCredential()
	if err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}
func (h *harness) close() {
	if h.store != nil {
		h.store.Close()
	}
	os.RemoveAll(h.root)
}
func (h *harness) reopen() error {
	if h.store != nil {
		if err := h.store.Close(); err != nil {
			return err
		}
	}
	var err error
	h.store, err = sqliteauth.Open(filepath.Join(h.root, "auth.db"))
	if err != nil {
		return err
	}
	h.service, err = authorization.New(h.store, h.clock, h.config)
	return err
}
func (h *harness) client(token string) *sdk.AuthorizationClient {
	return sdk.NewAuthorizationClient(authlocal.Bind(h.service, token), "local")
}
func (h *harness) apply(ctx context.Context, cmd *wire.AuthorizationCommand) (*wire.AuthorizationReceipt, error) {
	client := h.client(h.admin)
	view, err := client.GetPolicy(ctx)
	if err != nil {
		return nil, err
	}
	cmd.ExpectedRevision = view.Revision
	id, err := client.NewOperation(ctx)
	if err != nil {
		return nil, err
	}
	return client.Execute(ctx, id, cmd)
}
func (h *harness) scope(selector *wire.ResourceSelector, minutes int) *wire.AuthorizationScope {
	return &wire.AuthorizationScope{Resources: selector, Actions: []string{"read"}, Purposes: []string{"answer"}, Locations: []string{"local"}, ExpiresUnix: h.clock.at.Add(time.Duration(minutes) * time.Minute).Unix()}
}
func subtree(id string) *wire.ResourceSelector {
	return &wire.ResourceSelector{Selection: &wire.ResourceSelector_Subtree{Subtree: id}}
}
func exact(id string) *wire.ResourceSelector {
	return &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: id}}
}
func (h *harness) setup(ctx context.Context, mayIssue bool) error {
	commands := []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "user", CredentialSha256: authorization.CredentialDigest(h.user), ExpiresUnix: h.clock.at.Add(time.Hour).Unix()}}},
		{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "vault", Parent: "root"}}},
		{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "doc", Parent: "vault"}}},
		{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "public", Parent: "root"}}},
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: h.scope(subtree("root"), 60)}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "g1", Subject: "user", Scope: h.scope(subtree("vault"), 30), Mode: "continuous", MayIssue: mayIssue}}},
	}
	for _, cmd := range commands {
		if _, err := h.apply(ctx, cmd); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
	}
	return nil
}
func action() *wire.AuthorizationAction {
	return &wire.AuthorizationAction{Resource: "doc", Action: "read", Purpose: "answer", Location: "local"}
}
func requireCode(err error, code authorization.Code) error {
	if !authorization.Is(err, code) {
		return fmt.Errorf("expected %s, observed %v", code, err)
	}
	return nil
}
func expectDecision(ctx context.Context, h *harness, a *wire.AuthorizationAction, allowed bool) error {
	decision, err := h.client(h.user).Evaluate(ctx, a)
	if err != nil {
		return err
	}
	if decision.Allowed != allowed {
		return fmt.Errorf("expected allowed=%v, observed %v", allowed, decision.Allowed)
	}
	return nil
}
