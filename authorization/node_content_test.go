package authorization_test

import (
	"context"
	filecontent "lerna/adapters/content/file"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
	"time"
)

func nodeContent(t *testing.T, f *grantFixture) (*artifacts.Service, artifacts.Binding, *wire.ContentRef) {
	t.Helper()
	ctx := context.Background()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"read", "content.store", "content.process", "content.discover", "content.disclose", "content.retain"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: f.clock.now.Add(time.Hour).Unix()}
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "node-content", Subject: "admin", Mode: "continuous", Scope: scope}}},
	} {
		snap, err := f.db.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		command.ExpectedRevision = snap.State.Revision
		op, err := f.s.NewOperation(ctx, f.token)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.s.Execute(ctx, f.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); err != nil {
			t.Fatal(err)
		}
	}
	blobs, err := filecontent.Open(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	sources, err := contentpolicy.New([]contentpolicy.Rule{{Kind: "input", Key: "node-fixture", Revision: 1, Actions: []string{"store", "process", "discover", "disclose", "retain"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: scope.ExpiresUnix}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := artifacts.New(f.s, blobs, sources, artifacts.Config{Inline: 16, MaxObject: 1024, MaxTotal: 2048, MaxRecords: 8, MaxChunk: 1024, MaxFiles: 16, CleanupBatch: 8, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: f.token, Namespace: "local", Location: "local", Recipient: "local"}
	op, err := f.s.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	result, err := content.Call(ctx, binding, &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{{Kind: "input", Key: "node-fixture", Revision: 1}}, AcquiredAt: f.clock.now.Unix(), MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: scope.ExpiresUnix}, Data: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	return content, binding, result.Record.Ref
}
