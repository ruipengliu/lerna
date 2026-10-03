package task_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type lookupReader struct {
	content  *contentBridge
	material api.ContentRef
	mu       sync.Mutex
	requests []task.ContextLookupRequest
	fault    error
}

func (r *lookupReader) Resolve(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in task.ContextLookupRequest) (task.ContextLookupResult, error) {
	r.mu.Lock()
	r.requests = append(r.requests, in)
	fault := r.fault
	r.mu.Unlock()
	if fault != nil {
		return task.ContextLookupResult{}, fault
	}
	body, err := r.content.Read(ctx, scope, auth, r.material)
	if err != nil {
		return task.ContextLookupResult{}, err
	}
	query, err := r.content.Read(ctx, scope, auth, in.Lookup.QueryRef)
	if err != nil {
		return task.ContextLookupResult{}, err
	}
	return task.ContextLookupResult{Materials: []task.ContextMaterial{{ContentRef: r.material, Kind: in.Lookup.Kind, LookupRef: scope.Ref(in.LookupID, 1)}}, ReadBytesUpperBound: uint64(len(body) + len(query)), TokensBound: uint64(len(body) + len(query))}, nil
}

func TestContextLookupWaitsOnOriginalJobAndFencesNewDecisions(t *testing.T) {
	content := &contentBridge{}
	reader := &lookupReader{content: content, fault: api.E("dependency_unavailable", "original_content_store_unavailable")}
	h := newHarness(t, task.Ports{Content: content, ContextLookup: reader})
	configureContent(t, h, content)
	original := h.submit(t)
	ctx := context.Background()
	var err error
	reader.material, err = content.Publish(ctx, h.scope, api.NewID("transfer"), "text/plain", []byte("licensed ordinary context"))
	if err != nil {
		t.Fatal(err)
	}
	query, err := content.Publish(ctx, h.scope, api.NewID("transfer"), "application/json", api.Raw(map[string]any{"content_ref": reader.material}))
	if err != nil {
		t.Fatal(err)
	}
	prepared := h.prepared(original, "0")
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	p := task.Proposal{DecisionID: prepared.DecisionID, Kind: "need_context", ReasonRef: original.GoalRef, Lookups: []task.ContextLookup{{Kind: "existing_content", TargetRef: h.scope.Ref(reader.material.ContentID, reader.material.Version), QueryRef: query}}}
	consumed, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), p, nil)
	if err != nil || consumed.Outcome != "adopted" {
		t.Fatalf("original lookup intent not accepted: %+v %v", consumed, err)
	}
	current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !api.IsCode(err, "invalid_state") {
		t.Fatalf("new model responsibility admitted while original material lookup waits: %v", err)
	}
	work, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("missing original lookup Job: %+v %+v %v", work, status, err)
	}
	fn, ok := h.dispatch.Registry.Job(task.JobContextLookup)
	if !ok {
		t.Fatal("context lookup handler missing")
	}
	if err = fn(ctx, h.store, h.scope, work[0]); err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	reader.fault = nil
	reader.mu.Unlock()
	var again []runtime.Work
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		again, status, err = h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if len(again) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(again) != 1 || again[0].Job.JobID != work[0].Job.JobID {
		t.Fatal("dependency wait replaced or lost original lookup Job")
	}
	if err = fn(ctx, h.store, h.scope, again[0]); err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	requests := append([]task.ContextLookupRequest{}, reader.requests...)
	reader.mu.Unlock()
	if len(requests) != 2 || !api.Equal(requests[0], requests[1]) {
		t.Fatalf("lookup retry changed original identity, deadline or bound: %+v", requests)
	}
	facts, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || len(facts.ContextMaterials) != 1 || !api.Equal(facts.ContextMaterials[0].ContentRef, reader.material) {
		t.Fatalf("actual material was not saved for next Snapshot: %+v %v", facts.ContextMaterials, err)
	}
	if facts.ContextBudget.Calls != 1 || facts.ContextBudget.BytesBound != requests[0].MaxBytes || len(facts.SourceRefs) != 1 {
		t.Fatal("retry charged twice or ordinary context changed original source evidence")
	}
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(facts.Task, "0")); err != nil {
		t.Fatalf("next decision remained blocked after actual lookup completion: %v", err)
	}
	if replay, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), p, nil); err != nil || !api.Equal(replay, consumed) {
		t.Fatalf("original need_context consumption lost on replay: %+v %v", replay, err)
	}
}

func TestNeedContextPreservesTrustedExistingMaterialWithoutClaimingUserSource(t *testing.T) {
	h := newHarness(t, task.Ports{})
	original := h.submit(t)
	prepared := h.prepared(original, "0")
	if _, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	material := h.content("exact already granted existing material")
	proposal := task.Proposal{DecisionID: prepared.DecisionID, Kind: "need_context", ReasonRef: original.GoalRef, ContextRefs: []api.ContentRef{material}}
	consumed, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), proposal, nil)
	if err != nil || consumed.Outcome != "adopted" {
		t.Fatalf("original context consumption %+v %v", consumed, err)
	}
	facts, err := h.service.ContextFacts(context.Background(), h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ContextMaterials []struct {
			ContentRef api.ContentRef `json:"content_ref"`
		} `json:"ContextMaterials"`
	}
	if err = json.Unmarshal(api.Raw(facts), &out); err != nil || len(out.ContextMaterials) != 1 || !api.Equal(out.ContextMaterials[0].ContentRef, material) {
		t.Fatalf("need_context lost material before next Snapshot: %+v %v", out, err)
	}
	if len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, original.GoalRef) || facts.SourceRefs[0].SourceKind != "user_input" {
		t.Fatal("derived context silently became original user goal evidence")
	}
	replayed, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), proposal, nil)
	if err != nil || !api.Equal(replayed, consumed) {
		t.Fatalf("same context decision consumed twice %+v %v", replayed, err)
	}
}
