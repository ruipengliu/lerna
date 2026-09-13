package memoryauth_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/contextmemory"
	"lerna/adapters/filecontent"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqliteauth"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"testing"
	"time"
)

func checkDerivedContent(t *testing.T, service *memory.Service, adapter *memoryauth.Authority, auth *authorization.Service, db *sqliteauth.Store, b memory.Binding, scope *wire.AuthorizationScope, ref *wire.MemoryRef) {
	t.Helper()
	ctx := context.Background()
	dependency := contextassembly.Reference{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, Revision: 1}
	resolver, e := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return adapter.ReadPolicy(view) }, source{}, "device-a", []contextassembly.Reference{dependency})
	if e != nil {
		t.Fatal(e)
	}
	blobs, e := filecontent.Open(filepath.Join(t.TempDir(), "content"))
	if e != nil {
		t.Fatal(e)
	}
	defer blobs.Close()
	content, e := artifacts.New(auth, blobs, resolver, artifacts.Config{Inline: 16, MaxObject: 1024, MaxTotal: 4096, MaxRecords: 16, MaxChunk: 1024, MaxFiles: 32, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	op, e := auth.NewOperation(ctx, b.Token)
	if e != nil {
		t.Fatal(e)
	}
	binding := artifacts.Binding{Token: b.Token, Namespace: b.Namespace, Location: "device-a", Recipient: "device-a"}
	put := &wire.ContentRequest{Method: "PUT", OperationId: op, Data: []byte("hello"), Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "assist", Sources: []*wire.ContentSource{contextmemory.SourceReference(dependency)}, AcquiredAt: 1900000000, MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: 1900000500}}
	out, e := content.Call(ctx, binding, put)
	if e != nil {
		t.Fatalf("derived content write under same authority %v", e)
	}
	read := &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "assist", Limit: 5}
	result, e := content.Call(ctx, binding, read)
	if e != nil || string(result.Data) != "hello" {
		t.Fatalf("derived read %v", e)
	}
	change := func(scope *wire.AuthorizationScope) {
		t.Helper()
		op, e := auth.NewOperation(ctx, b.Token)
		if e != nil {
			t.Fatal(e)
		}
		state, e := db.Load(ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = auth.Execute(ctx, b.Token, authorization.Mutation{Namespace: b.Namespace, OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: state.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: scope}}}}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	denied := proto.Clone(scope).(*wire.AuthorizationScope)
	denied.Actions = nil
	for _, action := range scope.Actions {
		if action != "memory.disclose" {
			denied.Actions = append(denied.Actions, action)
		}
	}
	change(denied)
	if _, e = content.Call(ctx, binding, read); e == nil {
		t.Fatal("content authority bypassed revoked memory permission")
	}
	op, e = auth.NewOperation(ctx, b.Token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = content.Call(ctx, binding, &wire.ContentRequest{Method: "DELETE", OperationId: op, Ref: out.Record.Ref, ExpectedRevision: 1, Purpose: "assist"}); e != artifacts.Error("VERSION_CONFLICT") {
		t.Fatalf("old lifecycle revision bypassed automatic invalidation: %v", e)
	}
	status, e := content.Call(ctx, binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: put.OperationId, Purpose: "assist"})
	if e != nil || status.Record.State != "cleaning" || status.Record.LifecycleRevision != 2 {
		t.Fatalf("revoked use not scheduled for cleanup: %+v %v", status, e)
	}
	if e = content.Clean(ctx); e != nil {
		t.Fatal(e)
	}
	status, e = content.Call(ctx, binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: put.OperationId, Purpose: "assist"})
	if e != nil || status.Record.State != "cleaned" {
		t.Fatalf("revoked Memory disclosure blocked cleanup: %+v %v", status, e)
	}
	change(scope)
}
