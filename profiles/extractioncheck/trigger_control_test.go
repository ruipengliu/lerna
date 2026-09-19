package extractioncheck

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	extractionauth "lerna/adapters/extraction/auth"
	"lerna/authorization"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func TestTriggerOwnerCanCancelAfterScanRevocationWithSeparatePermission(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	enableScan(t, ctx, h)
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	registry, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	spec := extraction.TriggerSpec{ID: "owner-stop", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 2, MaxSteps: 3, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}
	if err = registry.Register(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err = registry.Cancel(ctx, spec.ID); err != memory.Denied {
		t.Fatalf("scan grant implicitly granted cancellation: %v", err)
	}
	state, err := h.candidates.GetTrigger(ctx, "local", "operator", spec.ID)
	if err != nil || state.State != "active" {
		t.Fatalf("denied cancel mutated registration: %+v %v", state, err)
	}
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	scope.Actions = append(scope.Actions, "memory.scan.cancel")
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "scan-control", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "scan-control", Subject: "operator", Scope: scope, Mode: "continuous"}}},
	} {
		snapshot, err = h.db.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		command.ExpectedRevision = snapshot.State.Revision
		op, e := h.operation(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); e != nil {
			t.Fatal(e)
		}
	}
	if err = revokeSaveAction(ctx, h, "memory.scan"); err != nil {
		t.Fatal(err)
	}
	if err = auth.Check(ctx, b, "scan"); err != memory.Denied {
		t.Fatalf("scan still authorized: %v", err)
	}
	for _, action := range []string{"memory.source.read", "memory.extract"} {
		if err = revokeSaveAction(ctx, h, action); err != nil {
			t.Fatal(err)
		}
	}
	h.clock.advance(10 * time.Minute)
	wrong := b
	wrong.Subject = "someone-else"
	other, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, wrong, "task")
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Cancel(ctx, spec.ID); err != memory.Denied {
		t.Fatalf("claimed owner bypassed identity: %v", err)
	}
	if err = registry.Cancel(ctx, spec.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.Cancel(ctx, spec.ID); err != nil {
		t.Fatalf("same cancellation failed replay: %v", err)
	}
	state, err = h.candidates.GetTrigger(ctx, "local", "operator", spec.ID)
	if err != nil || state.State != "cancelled" {
		t.Fatalf("cancellation not durable: %+v %v", state, err)
	}
	if err = revokeSaveAction(ctx, h, "memory.scan.cancel"); err != nil {
		t.Fatal(err)
	}
	if err = registry.Cancel(ctx, spec.ID); err != memory.Denied {
		t.Fatalf("replay bypassed current control permission: %v", err)
	}
}
