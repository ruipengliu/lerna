package sdk_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	authlocal "lerna/adapters/authorization/local"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
)

func TestAuthorizationSDKUsesRealCommitAndRejectsNamespaceForgery(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := authorization.Config{CredentialTTL: time.Hour, GrantTTL: 30 * time.Minute, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 16, MaxResources: 64, MaxDepth: 8, MaxWork: 1024, EvaluationTimeout: time.Second}
	service, err := authorization.New(store, authorization.SystemClock{}, config)
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewAuthorizationClient(authlocal.Bind(service, token), "local")
	op, err := client.NewOperation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Execute(ctx, op, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "doc", Parent: "root"}}})
	if err != nil || receipt.Evidence != "local_authorization_commit" {
		t.Fatalf("commit: %+v %v", receipt, err)
	}
	if _, err := sdk.NewAuthorizationClient(authlocal.Bind(service, token), "forged").GetPolicy(ctx); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("namespace forgery: %v", err)
	}
	if _, err := sdk.NewAuthorizationClient(authlocal.Bind(service, "admin"), "local").LookupOperation(ctx, op); !authorization.Is(err, authorization.Unauthenticated) {
		t.Fatalf("subject forgery: %v", err)
	}
}
