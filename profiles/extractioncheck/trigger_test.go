package extractioncheck

import (
	"context"
	"lerna/adapters/extractionauth"
	"lerna/adapters/sqliteextraction"
	"lerna/authorization"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"testing"
	"time"
)

func TestContinuousTriggerRequiresExplicitScanGrantAndPersistsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, "task")
	if err != nil {
		t.Fatal(err)
	}
	spec := extraction.TriggerSpec{ID: "explicit-source-scan", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 3, MaxSteps: 2, ExpiresUnix: h.now().Add(time.Hour).Unix()}
	if err = registry.Register(ctx, spec); err != memory.Denied {
		t.Fatalf("single-use rights enabled scan: %v", err)
	}
	if _, err = h.candidates.GetTrigger(ctx, "local", "operator", spec.ID); err != memory.Missing {
		t.Fatalf("denied registration persisted: %v", err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"memory.source.read", "memory.extract", "memory.scan"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: spec.ExpiresUnix}
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "explicit-scan", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "explicit-scan", Subject: "operator", Scope: scope, Mode: "continuous"}}},
	} {
		snapshot, e := h.db.Load(ctx)
		if e != nil {
			t.Fatal(e)
		}
		op, e := h.operation(ctx)
		if e != nil {
			t.Fatal(e)
		}
		command.ExpectedRevision = snapshot.State.Revision
		if _, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); e != nil {
			t.Fatal(e)
		}
	}
	if err = registry.Register(ctx, spec); err != nil {
		t.Fatal(err)
	}
	other, err := sqliteextraction.Open(filepath.Join(h.root, "candidates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err := other.GetTrigger(ctx, "local", "operator", spec.ID)
	if err != nil || got.State != "active" || got.Spec.MaxRounds != 3 || got.Spec.MaxSteps != 2 || got.Spec.Sources[0].Key != "one" {
		t.Fatalf("registration: %+v %v", got, err)
	}
	if err = registry.Register(ctx, spec); err != nil {
		t.Fatal(err)
	}
	changed := spec
	changed.MaxRounds = 4
	if err = registry.Register(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("budget reset: %v", err)
	}
	if err = other.CancelTrigger(ctx, "local", "operator", spec.ID); err != nil {
		t.Fatal(err)
	}
	if err = registry.Register(ctx, spec); err != memory.ReplayUnavailable {
		t.Fatalf("cancelled scan reactivated: %v", err)
	}
	got, err = h.candidates.GetTrigger(ctx, "local", "operator", spec.ID)
	if err != nil || got.State != "cancelled" {
		t.Fatalf("cancellation lost: %+v %v", got, err)
	}
}
