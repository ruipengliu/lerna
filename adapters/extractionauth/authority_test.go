package extractionauth_test

import (
	"context"
	"lerna/adapters/extractionauth"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

func TestReadGrantDoesNotAuthorizeExtractionOrScanning(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth, err := authorization.New(db, clock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Bootstrap(ctx, "local", "alice")
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"memory.source.read"}, Purposes: []string{"assist"}, Locations: []string{"device-a"}, ExpiresUnix: 1900003600}
	change := func(c *wire.AuthorizationCommand) {
		t.Helper()
		op, e := auth.NewOperation(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		snap, e := db.Load(ctx)
		if e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision = snap.State.Revision
		if _, e = auth.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c}); e != nil {
			t.Fatal(e)
		}
	}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "source", Scope: scope}}}}})
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "source", Subject: "alice", Scope: scope, Mode: "continuous"}}})
	a, err := extractionauth.New(auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "assist", Location: "device-a"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: token, Subject: "alice", Namespace: "local", Location: "device-a", Recipient: "device-a"}
	if err = a.Check(ctx, b, "read"); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"extract", "retain", "save", "scan", "disclose", "unknown"} {
		if err = a.Check(ctx, b, phase); err != memory.Denied {
			t.Fatalf("phase %s: %v", phase, err)
		}
	}
	scope.Actions = append(scope.Actions, "memory.extract")
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "source", Scope: scope}}}}})
	if err = a.Check(ctx, b, "extract"); err != memory.Denied {
		t.Fatalf("policy without grant authorized extraction: %v", err)
	}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "extract", Subject: "alice", Scope: scope, Mode: "continuous"}}})
	if err = a.Check(ctx, b, "extract"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"namespace", "subject", "location", "recipient"} {
		altered := b
		switch field {
		case "namespace":
			altered.Namespace = "other"
		case "subject":
			altered.Subject = "bob"
		case "location":
			altered.Location = "cloud"
		case "recipient":
			altered.Recipient = "cloud"
		}
		if err = a.Check(ctx, altered, "extract"); err != memory.Denied {
			t.Fatalf("binding %s: %v", field, err)
		}
	}
	if err = a.Check(ctx, b, "scan"); err != memory.Denied {
		t.Fatalf("one-shot extract became scan: %v", err)
	}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}})
	if err = a.Check(ctx, b, "extract"); err != memory.Denied {
		t.Fatalf("revoked extract: %v", err)
	}
}
