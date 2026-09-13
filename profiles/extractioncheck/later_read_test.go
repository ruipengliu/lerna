package extractioncheck

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/contextmemory"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/authorization"
	"lerna/contextassembly"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/tasks"
	"testing"
	"time"
)

// Real Memory admission and a fresh signed read grant are required even though
// the extraction task had permission to save the same record.
type laterUseCase struct {
	Inapplicable                          bool
	Kind, Condition, Claim, Block, Answer string
	Sources                               int
}

func readSavedInLaterTask(t *testing.T, h *harness, store *sqlitememory.Store, receipt memory.Receipt, original tasks.Ref, cases ...laterUseCase) (tasks.Task, *wire.MemoryReadResult) {
	t.Helper()
	use := laterUseCase{Kind: "inference", Condition: "report", Claim: "tentative-report-format", Block: `{"claim":"tentative-report-format","value":"detailed"}`, Answer: "The report is ready. Its detailed format follows a tentative inference from prior report choices.", Sources: 3}
	if len(cases) > 1 {
		t.Fatal("one task use policy required")
	}
	if len(cases) == 1 {
		use = cases[0]
	}
	ctx := context.Background()
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	scope.Actions = append(scope.Actions, "memory.get", "memory.store_reference")
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "later-memory-read", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "later-memory-read", Subject: "operator", Scope: scope, Mode: "continuous"}}},
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
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := h.core.Get(ctx, h.token, original)
	if err != nil {
		t.Fatal(err)
	}
	if previous.State != "COMPLETED" {
		t.Fatal("extraction task is not complete")
	}
	later, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Prepare a report using currently authorized saved format evidence", InputRefs: []string{putReportFacts(t, h)}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: h.now().Add(5 * time.Minute).Unix(), ModelRequests: 1, ModelTokens: 4352}})
	if err != nil {
		t.Fatal(err)
	}
	d := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{d})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, ReferenceStorage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{d.Type: schema.Digest(d.Document)}}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := memory.New(store, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	peer := memoryauth.Peer{Audience: "memory", Presenter: "local", CertificateSHA256: h.binding.CertificateSHA256}
	permits, err := memoryauth.NewReadPermits(policy, h.grants, peer)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := memory.NewReader(service, permits)
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	ref := &wire.MemoryRef{Namespace: receipt.Ref.Namespace, Collection: receipt.Ref.Collection, Key: receipt.Ref.Key}
	if err = service.ValidateReferenceStorage(ctx, b, ref, receipt.Revision, "task", "local"); err != nil {
		t.Fatal(err)
	}
	id, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	get := &wire.MemoryGet{ReadId: id, Ref: ref, Revision: receipt.Revision, Purpose: "task"}
	intent, err := memory.DescribeGet(b, get)
	if err != nil {
		t.Fatal(err)
	}
	op, err = h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: snapshot.State.Revision, Kind: "ISSUE", Spec: &wire.SignedGrantSpec{Subject: "operator", Audience: peer.Audience, Presenter: peer.Presenter, CertificateSha256: peer.CertificateSHA256, Scope: scope, NotBefore: h.now().Unix(), Units: 1, Mode: "single", OperationBinding: id, SemanticSha256: intent.SemanticSHA256}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.Get(ctx, b, get, grant.Material)
	if err != nil || result == nil || result.Coverage != "complete" {
		t.Fatalf("governed later Memory read: %v", err)
	}
	allocated, err := h.grants.Get(ctx, h.token, grant.GrantId)
	if err != nil || allocated.Allocated != 1 {
		t.Fatalf("later task read allocation: %v", err)
	}
	projector, err := contextmemory.NewProjector("extracted-report-v1", []contextmemory.ProjectionRule{{SchemaID: d.ID, SchemaVersion: d.Version, Kind: use.Kind, Condition: use.Condition, Claim: use.Claim, Field: "value", Values: []string{"concise", "detailed"}}})
	if err != nil {
		t.Fatal(err)
	}
	reference := contextassembly.Reference{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, Revision: receipt.Revision}
	key := contextassembly.Key{Namespace: later.Ref.Namespace, TaskID: later.Ref.TaskID, Decision: 1}
	request := contextassembly.Request{Key: key, Subject: "operator", Purpose: "task", Location: "local", Storage: "local", PolicyVersion: "extracted-report-v1", FactsVersion: later.Version, MaxBytes: 32768, Candidates: []contextassembly.Candidate{{Reference: reference}}}
	adapter, err := contextmemory.New(service, reader, permits, projector, contextmemory.Config{Binding: b, Decision: key, FactsVersion: later.Version, PolicyVersion: request.PolicyVersion, Purpose: "task", Storage: "local", Authorizations: []contextmemory.Authorization{{Reference: reference, ReadID: id, Material: grant.Material}}})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := adapter.Load(ctx, request, reference)
	if err != nil || projected.Applicable == use.Inapplicable || projected.Block.Role != "memory" || projected.Block.Text != use.Block {
		t.Fatalf("later task context did not receive inferred report format: %v", err)
	}
	wrongTask := request
	wrongTask.Key.TaskID = original.TaskID
	if _, err = adapter.Load(ctx, wrongTask, reference); err == nil {
		t.Fatal("original extraction task borrowed later context authority")
	}
	if len(result.Records) != 1 || len(result.Records[0].GetSpec().GetSources()) != use.Sources {
		t.Fatal("missing inference lineage")
	}
	checkInvalidatedOutput := runReportDecision(t, h, later, store, service, policy, adapter, request, reference, use.Answer, !use.Inapplicable)
	source := result.Records[0].Spec.Sources[0].Ref
	if _, err = h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: source.Kind, Key: source.Key, ThroughRevision: source.Revision}); err != nil {
		t.Fatal(err)
	}
	if next, e := adapter.Load(ctx, request, reference); e == nil || next.Block.Text != "" {
		t.Fatal("invalidated inference remained available to later task")
	}
	checkInvalidatedOutput()
	allocated, err = h.grants.Get(ctx, h.token, grant.GrantId)
	if err != nil || allocated.Allocated != 1 {
		t.Fatalf("context replay allocated another read: %v", err)
	}
	return later, result
}
