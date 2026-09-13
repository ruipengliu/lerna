package authorization_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func TestRetainedUseTracksOriginalSignedAllocationAndAncestorRevocation(t *testing.T) {
	for _, mode := range []string{"revocation", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := newGrantFixture(t)
			ctx := context.Background()
			if mode == "expiry" {
				f.spec.Scope.ExpiresUnix = f.clock.now.Add(20 * time.Second).Unix()
			}
			root, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
			if err != nil {
				t.Fatal(err)
			}
			child := f.request(t, "DERIVE", 2)
			child.Spec = proto.Clone(f.spec).(*wire.SignedGrantSpec)
			child.Spec.Units = 2
			child.Spec.DelegationDepth = 0
			child.GrantId = root.GrantId
			child.ExpectedGrantRevision = root.Revision
			derived, err := f.g.Mutate(ctx, f.token, child)
			if err != nil {
				t.Fatal(err)
			}
			op, err := f.s.NewOperation(ctx, f.token)
			if err != nil {
				t.Fatal(err)
			}
			localScope := proto.Clone(f.spec.Scope).(*wire.AuthorizationScope)
			localScope.ExpiresUnix = f.clock.now.Add(time.Hour).Unix()
			if _, err = f.s.Execute(ctx, f.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: derived.Revision, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "local-read", Subject: "admin", Scope: localScope, Mode: "continuous"}}}}); err != nil {
				t.Fatal(err)
			}
			action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
			p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: "retained-read", SemanticSHA256: strings.Repeat("b", 64)}
			permit, err := f.g.ReserveUse(ctx, derived.Material, p, action, 1)
			if err != nil {
				t.Fatal(err)
			}
			use := authorization.UseSpec{Namespace: "local", ID: "retained-context", Consumer: "contexts", ConfigSHA256: strings.Repeat("a", 64), Until: f.clock.now.Add(time.Minute).Unix(), Actions: []*wire.AuthorizationAction{action}, Permits: []authorization.UsePermit{permit}}
			forged := use
			forged.ID = "forged-context"
			forged.Permits = append([]authorization.UsePermit(nil), use.Permits...)
			forged.Permits[0].Presenter = "other"
			if err := f.s.RegisterUse(ctx, f.token, forged); !authorization.Is(err, authorization.Denied) {
				t.Fatalf("forged original allocation: %v", err)
			}
			if err := f.s.RegisterUse(ctx, f.token, use); err != nil {
				t.Fatal(err)
			}
			local := use
			local.ID = "local-context"
			local.Permits = nil
			if err := f.s.RegisterUse(ctx, f.token, local); err != nil {
				t.Fatal(err)
			}
			if mode == "expiry" {
				f.clock.now = f.clock.now.Add(30 * time.Second)
				notices, err := f.s.PendingUses(ctx, "local", "contexts", use.ConfigSHA256, 32)
				if err != nil || len(notices) != 1 || notices[0].ID != use.ID {
					t.Fatalf("signed expiry not isolated from local permission: %+v %v", notices, err)
				}
				return
			}
			view, err := f.s.GetPolicy(ctx, f.token)
			if err != nil {
				t.Fatal(err)
			}
			revoke := f.request(t, "REVOKE", view.Revision)
			revoke.Spec = nil
			revoke.GrantId = root.GrantId
			revoke.ExpectedGrantRevision = root.Revision
			withdrawn, err := f.g.Mutate(ctx, f.token, revoke)
			if err != nil {
				t.Fatal(err)
			}
			notices, err := f.s.PendingUses(ctx, "local", "contexts", use.ConfigSHA256, 32)
			if err != nil || len(notices) != 1 || notices[0].ID != use.ID || notices[0].Revision != withdrawn.Revision {
				t.Fatalf("ancestor revocation not retained: %+v %v", notices, err)
			}
			if err := f.s.RegisterUse(ctx, f.token, use); !authorization.Is(err, authorization.ResultOnly) {
				t.Fatalf("revoked original allocation reused: %v", err)
			}
		})
	}
}
