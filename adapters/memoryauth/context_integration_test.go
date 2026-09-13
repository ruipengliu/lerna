package memoryauth_test

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/contextmemory"
	"lerna/adapters/contextpolicy"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqlitecontext"
	"lerna/adapters/taskcontext"
	"lerna/authorization"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type contextFacts struct{}

func (contextFacts) Load(context.Context, contextassembly.Request) (brain.Input, error) {
	return brain.Input{Goal: "Answer the public question", Constraints: "Use an applicable preference"}, nil
}
func (contextFacts) Validate(context.Context, contextassembly.Request) error { return nil }

func checkContextIntegration(t *testing.T, service *memory.Service, reader *memory.Reader, permits *memoryauth.ReadPermits, auth *authorization.Service, grants *authorization.GrantAuthority, db *sqliteauth.Store, b memory.Binding, scope *wire.AuthorizationScope, ref *wire.MemoryRef, peer memoryauth.Peer) {
	t.Helper()
	ctx := context.Background()
	next := func() string {
		op, e := auth.NewOperation(ctx, b.Token)
		if e != nil {
			t.Fatal(e)
		}
		return op
	}
	get := &wire.MemoryGet{ReadId: next(), Ref: ref, Revision: 1, Purpose: "assist"}
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
	source := contextassembly.Reference{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, Revision: 1}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: b.Namespace, TaskID: "context-task", Decision: 1}, Subject: b.Subject, Purpose: "assist", Location: b.Recipient, Storage: "device-a", FactsVersion: 1, PolicyVersion: "text-v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: source, Required: true}}}
	projector, e := contextmemory.NewProjector(req.PolicyVersion, []contextmemory.ProjectionRule{{SchemaID: "urn:memory:text", SchemaVersion: "1", Kind: "preference", Condition: "when answering", Claim: "reply-style", Field: "text", Values: []string{"concise", "detailed"}}})
	if e != nil {
		t.Fatal(e)
	}
	adapter, e := contextmemory.New(service, reader, permits, projector, contextmemory.Config{Binding: b, Decision: req.Key, FactsVersion: 1, PolicyVersion: req.PolicyVersion, Purpose: req.Purpose, Storage: req.Storage, Authorizations: []contextmemory.Authorization{{Reference: source, ReadID: get.ReadId, Material: grant.Material}}})
	if e != nil {
		t.Fatal(e)
	}
	storePath := filepath.Join(t.TempDir(), "contexts.db")
	store, e := sqlitecontext.Open(storePath)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	policyConfig := contextpolicy.Config{Namespace: b.Namespace, Consumer: "memory-context", ConfigSHA256: strings.Repeat("e", 64), Batch: 16, Timeout: 5 * time.Second}
	policyAdapter, e := contextpolicy.New(auth, store, policyConfig)
	if e != nil {
		t.Fatal(e)
	}
	bound, e := policyAdapter.Bind(b.Token, adapter)
	if e != nil {
		t.Fatal(e)
	}
	assembler, e := contextassembly.NewWithPolicy(store, contextFacts{}, adapter, bound)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := permits.OriginalUse(ctx, b, intent, grant.Material); e != memory.Denied {
		t.Fatalf("unallocated read described as original use: %v", e)
	}
	if unused, e := grants.Get(ctx, b.Token, grant.GrantId); e != nil || unused.Allocated != 0 {
		t.Fatalf("dependency query allocated a read: %+v %v", unused, e)
	}
	for range 2 {
		out, e := assembler.Assemble(ctx, req)
		if e != nil || len(out.Input.Blocks) != 1 {
			t.Fatalf("real context %v %v", out, e)
		}
		var preference struct{ Claim, Value string }
		if json.Unmarshal([]byte(out.Input.Blocks[0].Text), &preference) != nil || preference.Claim != "reply-style" || preference.Value != "concise" || out.Input.Blocks[0].Role != "memory" {
			t.Fatalf("registered preference did not reach context: %+v", out.Input)
		}
	}
	// A tighter host storage policy cannot release a previously retained body.
	restricted, e := contextmemory.New(service, reader, permits, projector, contextmemory.Config{ReferenceOnly: true, Binding: b, Decision: req.Key, FactsVersion: 1, PolicyVersion: req.PolicyVersion, Purpose: req.Purpose, Storage: req.Storage, Authorizations: []contextmemory.Authorization{{Reference: source, ReadID: get.ReadId, Material: grant.Material}}})
	if e != nil {
		t.Fatal(e)
	}
	restrictedAssembly, e := contextassembly.NewWithPolicy(store, contextFacts{}, restricted, bound)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restrictedAssembly.Assemble(ctx, req); e != contextassembly.Denied {
		t.Fatalf("reference-only policy released retained snapshot: %v", e)
	}
	task := tasks.Task{Ref: tasks.Ref{Namespace: b.Namespace, TaskID: req.Key.TaskID}, Subject: b.Subject, Goal: "Answer the public question"}
	session, e := taskcontext.NewSession(assembler, req, task)
	if e != nil {
		t.Fatal(e)
	}
	lineage, e := session.Lineage(adapter)
	if e != nil {
		t.Fatal(e)
	}
	derived, e := lineage.Sources(ctx, task, "device-a")
	if e != nil || len(derived.Sources) != 1 || derived.Sources[0].Kind != "memory" || derived.Sources[0].Revision != 1 || derived.RetainUntil != 1900001000 {
		t.Fatalf("original context lineage %+v %v", derived, e)
	}
	if _, e = lineage.Sources(ctx, task, "cloud"); e != contextassembly.Denied {
		t.Fatalf("lineage exported to forbidden location: %v", e)
	}
	changedTask := task
	changedTask.Goal = "different"
	if _, e = lineage.Sources(ctx, changedTask, "device-a"); e != contextassembly.Invalidated {
		t.Fatalf("changed task lineage: %v", e)
	}
	// Restore the same decision from SQLite, without registering new reads or
	// changing the original candidate set. Process-level recovery is separate.
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sqlitecontext.Open(storePath)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored, e := contextassembly.NewWithPolicy(reopened, contextFacts{}, adapter, bound)
	if e != nil {
		t.Fatal(e)
	}
	restoredSession, e := taskcontext.NewSession(restored, req, task)
	if e != nil {
		t.Fatal(e)
	}
	lineage, e = restoredSession.Lineage(adapter)
	if e != nil {
		t.Fatal(e)
	}
	replayed, e := lineage.Sources(ctx, task, "device-a")
	if e != nil || len(replayed.Sources) != 1 || replayed.Sources[0].Key != derived.Sources[0].Key || replayed.RetainUntil != derived.RetainUntil {
		t.Fatalf("restored lineage changed %+v %v", replayed, e)
	}
	used, e := grants.Get(ctx, b.Token, grant.GrantId)
	if e != nil || used.Allocated != 1 {
		t.Fatalf("read units %v %v", used, e)
	}
	proof, e := permits.OriginalUse(ctx, b, intent, grant.Material)
	if e != nil || proof.Permit.GrantID != grant.GrantId || proof.Permit.OperationID != get.ReadId || proof.Permit.Units != 1 || len(proof.Actions) != 4 || proof.Expires != used.Spec.Scope.ExpiresUnix {
		t.Fatalf("original read dependency: %+v %v", proof, e)
	}
	changedIntent := intent
	changedIntent.ID = next()
	if _, e := permits.OriginalUse(ctx, b, changedIntent, grant.Material); e != memory.Denied {
		t.Fatalf("replacement read identity accepted: %v", e)
	}
	if allocated, e := grants.Get(ctx, b.Token, grant.GrantId); e != nil || allocated.Allocated != 1 {
		t.Fatalf("describing original use allocated again: %+v %v", allocated, e)
	}
	policy := func(nextScope *wire.AuthorizationScope) {
		state, err := db.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = auth.Execute(ctx, b.Token, authorization.Mutation{Namespace: b.Namespace, OperationID: next(), Command: &wire.AuthorizationCommand{ExpectedRevision: state.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: nextScope}}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	narrow := proto.Clone(scope).(*wire.AuthorizationScope)
	narrow.Actions = nil
	for _, action := range scope.Actions {
		if action != "memory.store_reference" {
			narrow.Actions = append(narrow.Actions, action)
		}
	}
	policy(narrow)
	policy(scope)
	if e := restored.Validate(ctx, req); e != contextassembly.Invalidated {
		t.Fatalf("restored storage permission resurrected original context: %v", e)
	}
	op = next()
	state, e = db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = grants.Mutate(ctx, b.Token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "REVOKE", GrantId: grant.GrantId, ExpectedGrantRevision: used.Revision}); e != nil {
		t.Fatal(e)
	}
	if _, e = lineage.Sources(ctx, task, "device-a"); e != contextassembly.Denied {
		t.Fatalf("revoked original read allowed new lineage: %v", e)
	}
	if e = restored.Validate(ctx, req); e != contextassembly.Denied {
		t.Fatalf("revoked retained context %v", e)
	}
	if _, e := permits.OriginalUse(ctx, b, intent, grant.Material); e != memory.Denied {
		t.Fatalf("revoked read exported a live dependency: %v", e)
	}
	checkContextErasure(t, reopened, storePath, req.Key, source)
}
