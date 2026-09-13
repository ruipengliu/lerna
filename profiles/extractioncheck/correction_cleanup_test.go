package extractioncheck

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/executionlocal"
	"lerna/adapters/extractioncleanup"
	"lerna/adapters/memoryauth"
	"lerna/adapters/memorycleanup"
	"lerna/authorization"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/sdk"
	"strings"
	"testing"
	"time"
)

func TestCorrectedDerivedMemoryCanBeCleanedAfterSourceInvalidation(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	saved, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || saved.State != "committed" || saved.Receipt == nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	intent, err := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	scope.Actions = append(scope.Actions, "memory.correct")
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "correct-control", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "correct-control", Subject: "operator", Scope: scope, Mode: "continuous"}}},
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
	d := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{d})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{d.Type: schema.Digest(d.Document)}}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := memory.New(store, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	correction := proto.Clone(intent.Request).(*wire.MemoryWrite)
	correction.OperationId, err = h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	correction.ExpectedRevision = saved.Receipt.Revision
	corrected, err := service.Correct(ctx, b, correction)
	if err != nil || corrected.Revision != 2 {
		t.Fatalf("real correction: %+v %v", corrected, err)
	}
	if n, e := h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); e != nil || n != 1 {
		t.Fatalf("source invalidation: %d %v", n, e)
	}
	sink, err := extractioncleanup.NewMemory(store)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := extraction.NewSourceCleanupWorker(h.candidates, sink, extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "memory-source-erasure", ConfigSHA256: strings.Repeat("f", 64)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	pass, err := worker.Run(ctx)
	if err != nil || len(pass.Attempts) != 1 || !pass.Attempts[0].Complete {
		t.Fatalf("source cleanup: %+v %v", pass, err)
	}
	for revision := uint64(1); revision <= 2; revision++ {
		if _, e := store.Read(ctx, saved.Receipt.Ref, revision); e != memory.Missing {
			t.Fatalf("derived revision remains: %d %v", revision, e)
		}
	}
	original, err := service.InspectOperation(ctx, b, intent.Request.OperationId)
	if err != nil || original.State != "committed" || original.Receipt == nil || original.Receipt.Revision != 1 {
		t.Fatalf("original fact lost: %+v %v", original, err)
	}
	events, err := store.ReadEvents(ctx, "local", "personal", 0, 16)
	if err != nil || len(events) != 4 || events[2].Kind != memory.SourceErased || events[3].Kind != memory.SourceErased {
		t.Fatalf("durable exact events: %+v %v", events, err)
	}
	admissions, err := memorycleanup.NewAdmissions(store, h.auth)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[uint64]string{1: intent.Request.OperationId, 2: correction.OperationId}
	for _, id := range ids {
		status, e := h.auth.InspectMemoryOperation(ctx, h.token, id)
		if e != nil || status.Admission.SemanticSHA256 == "" {
			t.Fatalf("comparison fixture: %+v %v", status, e)
		}
	}
	first := events[2]
	forged := first
	forged.Position++
	if done, e := admissions.Apply(ctx, forged); done || e != memory.Conflict {
		t.Fatalf("unproven erasure accepted: %v %v", done, e)
	}
	for _, id := range ids {
		status, e := h.auth.InspectMemoryOperation(ctx, h.token, id)
		if e != nil || status.Admission.SemanticSHA256 == "" {
			t.Fatalf("forged event erased comparison: %+v %v", status, e)
		}
	}
	if done, e := admissions.Apply(ctx, first); e != nil || !done {
		t.Fatalf("exact admission cleanup: %v %v", done, e)
	}
	for revision, id := range ids {
		status, e := h.auth.InspectMemoryOperation(ctx, h.token, id)
		if e != nil || status.State != "reserved" || (status.Admission.SemanticSHA256 == "") != (revision == first.Revision) {
			t.Fatalf("wrong comparison erased: %d %+v %v", revision, status, e)
		}
	}
	consumer, err := memory.NewSourceConsumer(store, store, admissions, memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "exact-admissions", ConfigSHA256: strings.Repeat("b", 64)}, Batch: 4, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := consumer.Run(ctx)
	if err != nil || completed.Position != 4 || completed.Applied != 4 {
		t.Fatalf("admission event acknowledgment: %+v %v", completed, err)
	}
	for _, id := range ids {
		status, e := h.auth.InspectMemoryOperation(ctx, h.token, id)
		if e != nil || status.Admission.SemanticSHA256 != "" {
			t.Fatalf("remaining comparison: %+v %v", status, e)
		}
	}
	replay, err := consumer.Run(ctx)
	if err != nil || replay.Position != 4 || replay.Applied != 0 {
		t.Fatalf("admission replay: %+v %v", replay, err)
	}

}
