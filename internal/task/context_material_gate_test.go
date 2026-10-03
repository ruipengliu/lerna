package task_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type materialVersionGate struct{ withdrawn atomic.Bool }

func (*materialVersionGate) Resolve(context.Context, runtime.Scope, runtime.Auth, task.ContextLookupRequest) (task.ContextLookupResult, error) {
	return task.ContextLookupResult{}, api.E("unsupported", "test_has_no_native_lookup")
}

type stagedMaterialReader struct {
	content     *contentBridge
	materials   map[string]api.ContentRef
	withdrawnID string
}

func (r *stagedMaterialReader) Resolve(ctx context.Context, sc runtime.Scope, auth runtime.Auth, in task.ContextLookupRequest) (task.ContextLookupResult, error) {
	ref := r.materials[in.Lookup.TargetRef.ObjectID]
	body, err := r.content.Read(ctx, sc, auth, ref)
	if err != nil {
		return task.ContextLookupResult{}, err
	}
	query, err := r.content.Read(ctx, sc, auth, in.Lookup.QueryRef)
	if err != nil {
		return task.ContextLookupResult{}, err
	}
	upper := uint64(len(body) + len(query))
	return task.ContextLookupResult{Materials: []task.ContextMaterial{{ContentRef: ref, Kind: in.Lookup.Kind, LookupRef: sc.Ref(in.LookupID, 1)}}, ReadBytesUpperBound: upper, TokensBound: upper}, nil
}
func (r *stagedMaterialReader) CheckMaterialsTx(_ context.Context, _ runtime.Tx, _ runtime.Auth, materials []task.ContextMaterial) error {
	for _, material := range materials {
		if material.ContentRef.ContentID == r.withdrawnID {
			return api.E("gone", "original_staged_material_withdrawn")
		}
	}
	return nil
}

func TestContextBatchRechecksPreviouslyReadMaterialBeforeFinalPublication(t *testing.T) {
	ctx := context.Background()
	content := &contentBridge{}
	reader := &stagedMaterialReader{content: content, materials: map[string]api.ContentRef{}}
	h := newHarness(t, task.Ports{Content: content, ContextLookup: reader})
	configureContent(t, h, content)
	original := h.submit(t)
	d := h.prepared(original, "0")
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), d); err != nil {
		t.Fatal(err)
	}
	lookups := []task.ContextLookup{}
	for _, text := range []string{"first original licensed material", "second still licensed material"} {
		ref, err := content.Publish(ctx, h.scope, api.NewID("upload"), "text/plain", []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		reader.materials[ref.ContentID] = ref
		query, err := content.Publish(ctx, h.scope, api.NewID("upload"), "application/json", api.Raw(map[string]any{"content_ref": ref}))
		if err != nil {
			t.Fatal(err)
		}
		lookups = append(lookups, task.ContextLookup{Kind: "existing_content", TargetRef: h.scope.Ref(ref.ContentID, ref.Version), QueryRef: query})
	}
	out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: d.DecisionID, Kind: "need_context", ReasonRef: original.GoalRef, Lookups: lookups}, nil)
	if err != nil || out.Outcome != "adopted" {
		t.Fatalf("original batch: %+v %v", out, err)
	}
	handler, _ := h.dispatch.Registry.Job(task.JobContextLookup)
	var originalJob string
	for i := 0; i < 2; i++ {
		works, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
		if err != nil || status != runtime.Committed || len(works) != 1 {
			t.Fatalf("original bounded lookup: %+v %v", works, err)
		}
		if i == 0 {
			originalJob = works[0].Job.JobID
		} else if originalJob != works[0].Job.JobID {
			t.Fatal("batch replaced original job")
		}
		if err = handler(ctx, h.store, h.scope, works[0]); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			facts, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, original.TaskID)
			if err != nil || !facts.ContextPending || len(facts.ContextMaterials) != 0 {
				t.Fatalf("incomplete batch escaped: %+v %v", facts, err)
			}
			reader.withdrawnID = lookups[0].TargetRef.ObjectID
		}
	}
	facts, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || !facts.ContextPending || len(facts.ContextMaterials) != 0 || facts.ContextBudget.Calls != 2 || len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, original.GoalRef) {
		t.Fatalf("final batch reused previously withdrawn material: %+v %v", facts, err)
	}
	if len(facts.Task.WaitReasons) != 1 || facts.Task.WaitReasons[0].ResumeCondition != "original_context_lookup: gone: original_staged_material_withdrawn; valid new input or goal change required" {
		t.Fatalf("blocked original batch reason: %+v", facts.Task.WaitReasons)
	}
}
func (g *materialVersionGate) CheckMaterialsTx(_ context.Context, _ runtime.Tx, _ runtime.Auth, materials []task.ContextMaterial) error {
	if len(materials) > 0 && g.withdrawn.Load() {
		return api.E("gone", "original_material_withdrawn")
	}
	return nil
}

func TestWithdrawnOrdinaryContextMaterialBlocksNewDecisionWithoutChangingGoalSource(t *testing.T) {
	ctx := context.Background()
	gate := &materialVersionGate{}
	h := newHarness(t, task.Ports{ContextLookup: gate})
	original := h.submit(t)
	d := h.prepared(original, "0")
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), d); err != nil {
		t.Fatal(err)
	}
	material := h.content("accurate ordinary material, never user goal evidence")
	if _, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: d.DecisionID, Kind: "need_context", ReasonRef: original.GoalRef, ContextRefs: []api.ContentRef{material}}, nil); err != nil {
		t.Fatal(err)
	}
	facts, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || len(facts.ContextMaterials) != 1 || len(facts.SourceRefs) != 1 {
		t.Fatalf("original ordinary/source separation lost: %+v %v", facts, err)
	}
	gate.withdrawn.Store(true) // 准确当前许可/版本边界；持久化和原Task不替换。
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(facts.Task, "0")); !api.IsCode(err, "gone") {
		t.Fatalf("new decision reused withdrawn original material: %v", err)
	}
	after, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || !api.Equal(facts.SourceRefs, after.SourceRefs) || after.ContextBudget.Calls != 1 {
		t.Fatalf("rejected preparation rewrote user evidence or lifetime lookup budget: %+v %v", after, err)
	}
}
