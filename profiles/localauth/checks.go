package localauth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	authlocal "lerna/adapters/authorization/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
)

func check(ctx context.Context, name string) error {
	config := Config()
	if name == "evaluation-budget" {
		config.MaxWork = 32
	}
	h, err := newHarness(ctx, config)
	if err != nil {
		return err
	}
	defer h.close()
	if name == "bootstrap-reopen" {
		_, err = h.service.Bootstrap(ctx, "other", "intruder")
		if err = requireCode(err, authorization.Conflict); err != nil {
			return err
		}
		if err = h.reopen(); err != nil {
			return err
		}
		identity, err := h.service.Authenticate(ctx, h.admin)
		if err != nil {
			return err
		}
		if identity.Subject != "admin" || identity.Namespace != "local" {
			return fmt.Errorf("trust changed")
		}
		return nil
	}
	if name == "missing-configuration" {
		_, err := authorization.New(h.store, h.clock, authorization.Config{})
		return requireCode(err, authorization.Invalid)
	}
	if err = h.setup(ctx, name == "issuer-containment"); err != nil {
		return err
	}
	switch name {
	case "continuous-use":
		for i := 0; i < 3; i++ {
			if err := expectDecision(ctx, h, action(), true); err != nil {
				return err
			}
		}
		return nil
	case "cross-subject":
		_, err := h.client(h.user).GetPolicy(ctx)
		return requireCode(err, authorization.Denied)
	case "forged-identity":
		_, err := h.client("admin").GetPolicy(ctx)
		return requireCode(err, authorization.Unauthenticated)
	case "cross-namespace":
		_, err := sdk.NewAuthorizationClient(authlocal.Bind(h.service, h.admin), "other").GetPolicy(ctx)
		return requireCode(err, authorization.Denied)
	case "outside-resource", "outside-action", "outside-purpose", "outside-location":
		a := action()
		switch name {
		case "outside-resource":
			a.Resource = "public"
		case "outside-action":
			a.Action = "write"
		case "outside-purpose":
			a.Purpose = "training"
		case "outside-location":
			a.Location = "cloud"
		}
		return expectDecision(ctx, h, a, false)
	case "hard-deny":
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: h.scope(subtree("root"), 60)}, {Id: "deny", Scope: h.scope(exact("doc"), 60), Deny: true}}}}})
		if err != nil {
			return err
		}
		return expectDecision(ctx, h, action(), false)
	case "grant-expiry":
		h.clock.at = h.clock.at.Add(31 * time.Minute)
		return expectDecision(ctx, h, action(), false)
	case "time-rollback":
		h.clock.at = h.clock.at.Add(-time.Second)
		_, err := h.client(h.user).Evaluate(ctx, action())
		return requireCode(err, authorization.TimeUntrusted)
	case "unknown-constraint":
		a := action()
		a.RequiredConstraints = []string{"unimplemented"}
		_, err := h.client(h.user).Evaluate(ctx, a)
		return requireCode(err, authorization.Unsupported)
	case "disabled-principal":
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "user", CredentialSha256: authorization.CredentialDigest(h.user), ExpiresUnix: h.clock.at.Add(time.Hour).Unix(), Disabled: true}}})
		if err != nil {
			return err
		}
		_, err = h.client(h.user).Evaluate(ctx, action())
		return requireCode(err, authorization.Unauthenticated)
	case "unsupported-single-use":
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "single", Subject: "user", Scope: h.scope(exact("doc"), 20), Mode: "single"}}})
		return requireCode(err, authorization.Unsupported)
	case "execution-is-not-issuance", "issuer-containment":
		view, err := h.client(h.admin).GetPolicy(ctx)
		if err != nil {
			return err
		}
		id, err := h.client(h.user).NewOperation(ctx)
		if err != nil {
			return err
		}
		selector := exact("doc")
		if name == "issuer-containment" {
			selector = subtree("root")
		}
		cmd := &wire.AuthorizationCommand{ExpectedRevision: view.Revision, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "new", Subject: "user", Scope: h.scope(selector, 20), Mode: "continuous"}}}
		_, err = h.client(h.user).Execute(ctx, id, cmd)
		if err = requireCode(err, authorization.Denied); err != nil {
			return err
		}
		if name == "issuer-containment" {
			cmd.GetIssueGrant().Scope.Resources = exact("doc")
			_, err = h.client(h.user).Execute(ctx, id, cmd)
			return err
		}
		return nil
	case "selector-set":
		selector := &wire.ResourceSelector{Selection: &wire.ResourceSelector_Set{Set: &wire.ResourceSet{Ids: []string{"doc", "vault"}}}}
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "set", Subject: "user", Scope: h.scope(selector, 20), Mode: "continuous"}}})
		return err
	case "unregistered-tree":
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "fake", Subject: "user", Scope: h.scope(subtree("claimed-tree"), 20), Mode: "continuous"}}})
		return requireCode(err, authorization.Invalid)
	case "evaluation-budget":
		rules := []*wire.PolicyRule{}
		for i := 0; i < 20; i++ {
			rules = append(rules, &wire.PolicyRule{Id: fmt.Sprintf("r%d", i), Scope: h.scope(exact("doc"), 60)})
		}
		_, err := h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: rules}}})
		if err != nil {
			return err
		}
		_, err = h.client(h.user).Evaluate(ctx, action())
		return requireCode(err, authorization.Denied)
	case "idempotency-and-conflict", "current-auth-before-receipt", "tampered-window", "window-cleanup", "concurrent-revision":
		return operationCheck(ctx, h, name)
	default:
		return fmt.Errorf("unknown verification case")
	}
}
func resourceCommand(id string) *wire.AuthorizationCommand {
	return &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: id, Parent: "root"}}}
}
func operationCheck(ctx context.Context, h *harness, name string) error {
	client := h.client(h.admin)
	view, err := client.GetPolicy(ctx)
	if err != nil {
		return err
	}
	id, err := client.NewOperation(ctx)
	if err != nil {
		return err
	}
	cmd := resourceCommand("added")
	cmd.ExpectedRevision = view.Revision
	if name == "tampered-window" {
		_, err = client.Execute(ctx, id+"x", cmd)
		if err == nil {
			return fmt.Errorf("tampered operation accepted")
		}
		return nil
	}
	if name == "concurrent-revision" {
		other, err := client.NewOperation(ctx)
		if err != nil {
			return err
		}
		otherCmd := resourceCommand("other")
		otherCmd.ExpectedRevision = view.Revision
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, v := range []struct {
			id  string
			cmd *wire.AuthorizationCommand
		}{{id, cmd}, {other, otherCmd}} {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := client.Execute(ctx, v.id, v.cmd); results <- err }()
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !authorization.Is(err, authorization.Conflict) {
				return err
			}
		}
		if successes != 1 {
			return fmt.Errorf("concurrent revisions both accepted")
		}
		return nil
	}
	first, err := client.Execute(ctx, id, cmd)
	if err != nil {
		return err
	}
	if name == "current-auth-before-receipt" {
		_, err = h.client(h.user).LookupOperation(ctx, id)
		return requireCode(err, authorization.Denied)
	}
	if name == "window-cleanup" {
		unused, err := client.NewOperation(ctx)
		if err != nil {
			return err
		}
		if _, err = h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}); err != nil {
			return err
		}
		if _, err = client.Execute(ctx, unused, cmd); !authorization.Is(err, authorization.Expired) {
			return fmt.Errorf("closed window accepted: %v", err)
		}
		if err = h.reopen(); err != nil {
			return err
		}
		client = h.client(h.admin)
		if _, err = client.LookupOperation(ctx, id); err != nil {
			return err
		}
		h.clock.at = h.clock.at.Add(3 * time.Minute)
		if _, err = h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}); err != nil {
			return err
		}
		if err = h.reopen(); err != nil {
			return err
		}
		client = h.client(h.admin)
		_, err = client.Execute(ctx, id, cmd)
		return requireCode(err, authorization.Expired)
	}
	second, err := client.Execute(ctx, id, cmd)
	if err != nil || !proto.Equal(first, second) {
		return fmt.Errorf("idempotent retry: %v", err)
	}
	cmd.GetRegisterResource().Id = "different"
	_, err = client.Execute(ctx, id, cmd)
	return requireCode(err, authorization.IdentityConflict)
}
