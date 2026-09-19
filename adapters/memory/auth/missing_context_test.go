package auth_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	sqliteauth "lerna/adapters/authorization/sqlite"
	filecontent "lerna/adapters/content/file"
	contextmemory "lerna/adapters/context/memory"
	contextpolicy "lerna/adapters/context/policy"
	sqlitecontext "lerna/adapters/context/sqlite"
	memoryauth "lerna/adapters/memory/auth"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkMissingContext(t *testing.T, service *memory.Service, reader *memory.Reader, permits *memoryauth.ReadPermits, policy *memoryauth.Authority, auth *authorization.Service, grants *authorization.GrantAuthority, db *sqliteauth.Store, b memory.Binding, scope *wire.AuthorizationScope, peer memoryauth.Peer, spec *wire.MemorySpec) {
	t.Helper()
	ctx := context.Background()
	next := func() string {
		op, e := auth.NewOperation(ctx, b.Token)
		if e != nil {
			t.Fatal(e)
		}
		return op
	}
	ref := contextassembly.Reference{Namespace: b.Namespace, Collection: "personal", Key: "not-recorded", Revision: 1}
	get := &wire.MemoryGet{ReadId: next(), Ref: &wire.MemoryRef{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key}, Revision: 1, Purpose: "assist"}
	intent, e := memory.DescribeGet(b, get)
	if e != nil {
		t.Fatal(e)
	}
	op := next()
	state, e := db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	grant, e := grants.Mutate(ctx, b.Token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "ISSUE", Spec: &wire.SignedGrantSpec{Subject: b.Subject, Audience: peer.Audience, Presenter: peer.Presenter, CertificateSha256: peer.CertificateSHA256, Scope: scope, NotBefore: 1900000000, Units: 1, Mode: "single", OperationBinding: get.ReadId, SemanticSha256: intent.SemanticSHA256}})
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: b.Namespace, TaskID: "missing-context", Decision: 1}, Subject: b.Subject, Purpose: "assist", Location: b.Recipient, Storage: "device-a", FactsVersion: 1, PolicyVersion: "text-v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: ref}}}
	projector, e := contextmemory.NewProjector(req.PolicyVersion, []contextmemory.ProjectionRule{{SchemaID: "urn:memory:text", SchemaVersion: "1", Kind: "preference", Condition: "when answering", Claim: "reply-style", Field: "text", Values: []string{"concise", "detailed"}}})
	if e != nil {
		t.Fatal(e)
	}
	adapter, e := contextmemory.New(service, reader, permits, projector, contextmemory.Config{Binding: b, Decision: req.Key, FactsVersion: 1, PolicyVersion: req.PolicyVersion, Purpose: req.Purpose, Storage: req.Storage, MissingChecker: policy, MissingRetainUntil: 1900000500, Authorizations: []contextmemory.Authorization{{Reference: ref, ReadID: get.ReadId, Material: grant.Material}}})
	if e != nil {
		t.Fatal(e)
	}
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	contextPolicy, e := contextpolicy.New(auth, store, contextpolicy.Config{Namespace: b.Namespace, Consumer: "missing-context", ConfigSHA256: strings.Repeat("f", 64), Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	bound, e := contextPolicy.Bind(b.Token, adapter)
	if e != nil {
		t.Fatal(e)
	}
	assembler, e := contextassembly.NewWithPolicy(store, contextFacts{}, adapter, bound)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		out, e := assembler.Assemble(ctx, req)
		if e != nil || len(out.Status.Missing) != 1 || len(out.Input.Blocks) != 0 {
			t.Fatalf("missing context: %+v %v", out, e)
		}
	}
	source, until, e := adapter.Derive(ctx, req, contextassembly.Dependency{Reference: ref, State: "missing"}, "device-a")
	if e != nil || source.Kind != "memory-missing" || until != 1900000500 {
		t.Fatalf("missing lineage: %v %v", source, e)
	}
	resolver, e := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return policy.ReadPolicy(view) }, sourcePolicyForMissing{}, "device-a", []contextassembly.Reference{ref})
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
	binding := artifacts.Binding{Token: b.Token, Namespace: b.Namespace, Location: "device-a", Recipient: "device-a"}
	put := &wire.ContentRequest{Method: "PUT", OperationId: next(), Data: []byte("hello"), Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "assist", Sources: []*wire.ContentSource{source}, AcquiredAt: 1900000000, MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: until}}
	out, e := content.Call(ctx, binding, put)
	if e != nil {
		t.Fatalf("missing derived write: %v", e)
	}
	read := &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "assist", Limit: 5}
	got, e := content.Call(ctx, binding, read)
	if e != nil || string(got.Data) != "hello" {
		t.Fatalf("missing derived read: %v", e)
	}
	used, e := grants.Get(ctx, b.Token, grant.GrantId)
	if e != nil || used.Allocated != 1 {
		t.Fatalf("missing read allocation: %v %v", used, e)
	}

	// Creating the previously absent revision invalidates the old absence claim;
	// replay must not silently keep it or adopt the newly available body.
	if _, e = service.Put(ctx, b, &wire.MemoryWrite{OperationId: next(), Ref: get.Ref, Spec: proto.Clone(spec).(*wire.MemorySpec)}); e != nil {
		t.Fatal(e)
	}
	if e = assembler.Validate(ctx, req); e != contextassembly.Invalidated {
		t.Fatalf("newly present source kept missing context valid: %v", e)
	}
	if _, e = assembler.Assemble(ctx, req); e != contextassembly.Invalidated {
		t.Fatalf("replay replaced original absence: %v", e)
	}

	op = next()
	state, e = db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = grants.Mutate(ctx, b.Token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "REVOKE", GrantId: grant.GrantId, ExpectedGrantRevision: used.Revision}); e != nil {
		t.Fatal(e)
	}
	if e = assembler.Validate(ctx, req); e != contextassembly.Denied {
		t.Fatalf("revoked original missing read replayed: %v", e)
	}
	change := func(scope *wire.AuthorizationScope) {
		op := next()
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
	if _, e = content.Call(ctx, binding, read); e != artifacts.Error("PERMISSION_DENIED") {
		t.Fatalf("missing derived disclosure bypass: %v", e)
	}
	change(scope)
}

// This test allows no fallback source: the artifact must use the missing policy.
type sourcePolicyForMissing struct{}

func (sourcePolicyForMissing) Check(context.Context, *wire.ContentSource, string, string, string, int64) error {
	return memory.Denied
}
