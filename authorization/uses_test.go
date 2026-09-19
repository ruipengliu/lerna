package authorization_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func TestUseInvalidationSurvivesPurposeRestorationAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store"}, Purposes: []string{"assist", "research"}, Locations: []string{"local"}, ExpiresUnix: clock.at.Add(time.Hour).Unix()}
	apply := func(command *wire.AuthorizationCommand) uint64 {
		t.Helper()
		view, err := service.GetPolicy(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		op, err := service.NewOperation(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		command.ExpectedRevision = view.Revision
		out, err := service.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command})
		if err != nil {
			t.Fatal(err)
		}
		return out.Revision
	}
	policy := func(s *wire.AuthorizationScope) uint64 {
		return apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "content", Scope: s}}}}})
	}
	policy(scope)
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "content", Subject: "admin", Mode: "continuous", Scope: scope}}})
	checkContextPolicyCleanup(t, service, token, scope, policy, clock.at)
	spec := authorization.UseSpec{Namespace: "local", ID: "assist-body", Consumer: "content", ConfigSHA256: strings.Repeat("a", 64), Until: clock.at.Add(30 * time.Second).Unix(), Actions: []*wire.AuthorizationAction{{Resource: "root", Action: "content.store", Purpose: "assist", Location: "local"}}}
	if err := service.RegisterUse(ctx, token, spec); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterUse(ctx, token, spec); err != nil {
		t.Fatalf("repeat use: %v", err)
	}
	other := spec
	other.ID = "research-body"
	other.Actions = []*wire.AuthorizationAction{{Resource: "root", Action: "content.store", Purpose: "research", Location: "local"}}
	if err := service.RegisterUse(ctx, token, other); err != nil {
		t.Fatal(err)
	}
	narrowed := proto.Clone(scope).(*wire.AuthorizationScope)
	narrowed.Purposes = []string{"research"}
	withdrawn := policy(narrowed)
	policy(scope)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err = authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	notices, err := service.PendingUses(ctx, "local", "content", spec.ConfigSHA256, 32)
	if err != nil || len(notices) != 1 || notices[0].ID != "assist-body" || notices[0].Revision != withdrawn {
		t.Fatalf("restoration lost original invalidation: %+v %v", notices, err)
	}
	if err := service.RegisterUse(ctx, token, spec); !authorization.Is(err, authorization.ResultOnly) {
		t.Fatalf("invalidated identity became active: %v", err)
	}
	wrong := notices[0]
	wrong.Revision++
	if err := service.ConfirmUseCleaned(ctx, "local", "content", spec.ConfigSHA256, wrong); !authorization.Is(err, authorization.Conflict) {
		t.Fatalf("wrong confirmation: %v", err)
	}
	if err := service.ConfirmUseCleaned(ctx, "local", "content", spec.ConfigSHA256, notices[0]); err != nil {
		t.Fatal(err)
	}
	if again, err := service.PendingUses(ctx, "local", "content", spec.ConfigSHA256, 32); err != nil || len(again) != 0 {
		t.Fatalf("completed use stayed pending: %+v %v", again, err)
	}
	clock.at = clock.at.Add(time.Minute)
	if expired, err := service.PendingUses(ctx, "local", "content", spec.ConfigSHA256, 32); err != nil || len(expired) != 1 || expired[0].ID != "research-body" {
		t.Fatalf("expiry did not create cleanup notice: %+v %v", expired, err)
	}
}
