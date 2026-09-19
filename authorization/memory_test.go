package authorization_test

import (
	"context"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryAdmissionRetainsIdentityWithoutClaimingBusinessCommit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	store, e := sqliteauth.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, e := authorization.New(store, clock, config())
	if e != nil {
		t.Fatal(e)
	}
	token, e := service.Bootstrap(ctx, "local", "admin")
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"memory.put"}, Purposes: []string{"assist"}, Locations: []string{"local"}, ExpiresUnix: clock.at.Add(time.Hour).Unix()}
	apply := func(command *wire.AuthorizationCommand) {
		t.Helper()
		view, e := service.GetPolicy(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		command.ExpectedRevision = view.Revision
		op, e := service.NewOperation(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = service.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); e != nil {
			t.Fatal(e)
		}
	}
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: scope}}}}})
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "memory", Subject: "admin", Scope: scope, Mode: "continuous"}}})
	op, e := service.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	action := &wire.AuthorizationAction{Resource: "root", Action: "memory.put", Purpose: "assist", Location: "local"}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first, e := service.ReserveMemoryOperation(ctx, token, op, digest, action)
	if e != nil || first.ExpiresUnixNano <= clock.at.UnixNano() {
		t.Fatalf("first %+v %v", first, e)
	}
	again, e := service.ReserveMemoryOperation(ctx, token, op, digest, action)
	if e != nil || again.SemanticSHA256 != first.SemanticSHA256 || again.ExpiresUnixNano != first.ExpiresUnixNano {
		t.Fatalf("repeat %+v %v", again, e)
	}
	if _, e = service.ReserveMemoryOperation(ctx, token, op, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"[:64], action); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("changed identity %v", e)
	}
	if e = service.UpdateContent(ctx, func(tx authorization.ContentTransaction) error { return tx.Operation(op, "admin", true) }); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("content identity reuse %v", e)
	}
	if e = service.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return tx.Operation(op, "admin", true) }); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("runtime identity reuse %v", e)
	}
	view, e := service.GetPolicy(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: view.Revision, Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "other", Parent: "root"}}}}); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("cross-domain reuse %v", e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	store, e = sqliteauth.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	service, e = authorization.New(store, clock, config())
	if e != nil {
		t.Fatal(e)
	}
	status, e := service.InspectMemoryOperation(ctx, token, op)
	if e != nil || status.State != "reserved" || status.Admission.Subject != "admin" {
		t.Fatalf("reopen %+v %v", status, e)
	}
	clock.at = clock.at.Add(2 * time.Minute)
	if _, e = service.ReserveMemoryOperation(ctx, token, op, digest, action); !authorization.Is(e, authorization.Expired) {
		t.Fatalf("expired reserved operation %v", e)
	}
	status, e = service.InspectMemoryOperation(ctx, token, op)
	if e != nil || status.State != "expired" {
		t.Fatalf("expired status %+v %v", status, e)
	}
	fresh, e := service.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	status, e = service.InspectMemoryOperation(ctx, token, fresh)
	if e != nil || status.State != "not_admitted" {
		t.Fatalf("unused %+v %v", status, e)
	}
	if _, e = service.ReserveMemoryOperation(ctx, token, fresh, digest, action); e != nil {
		t.Fatal(e)
	}
	if e = service.EraseMemoryComparisons(ctx, "local", []authorization.MemoryComparison{{OperationID: fresh, Subject: "admin"}}); e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sqliteauth.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	service, e = authorization.New(reopened, clock, config())
	if e != nil {
		t.Fatal(e)
	}
	status, e = service.InspectMemoryOperation(ctx, token, fresh)
	if e != nil || status.State != "reserved" || status.Admission.SemanticSHA256 != "" || status.Admission.Subject != "admin" {
		t.Fatalf("erased comparison restored: %+v %v", status, e)
	}
	if _, e = service.ReserveMemoryOperation(ctx, token, fresh, digest, action); !authorization.Is(e, authorization.ResultOnly) {
		t.Fatalf("reopened admission reused erased comparison: %v", e)
	}
	if e = service.EraseMemoryComparisons(ctx, "local", []authorization.MemoryComparison{{OperationID: fresh, Subject: "admin"}}); e != nil {
		t.Fatalf("repeat erasure: %v", e)
	}
}
