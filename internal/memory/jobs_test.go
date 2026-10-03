package memory_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestReviewOnlyExtractionSavesExactlyOneMemoryAndCleanupIsRecoverable(t *testing.T) {
	f := newFixture(t)
	values := f.values(t, "本人偏好使用中文")
	values.Type = "preference"
	doc := memory.ExtractionDocument{Statements: []memory.ExtractionStatement{{Values: values, ExpiresAt: api.Time(time.Now().Add(5 * time.Minute))}}}
	input := f.upload(t, string(api.Raw(doc)))
	id := api.NewID("extraction")
	r := f.command(t, "memory.extract", id, nil, memory.ExtractInput{ExtractionID: id, InputRefs: []api.ContentRef{input}, ExtractorRef: memory.RuleExtractor(), Limits: memory.ExtractionLimits{MaxInputBytes: 1 << 20, MaxCandidates: 1, MaxTokens: 0, MaxCost: api.Amount{Unit: "USD", Value: "0"}}, Deadline: api.Time(time.Now().Add(time.Minute)), SavingMode: "review_only"})
	if r.Stage != "applied" {
		t.Fatalf("extract: %+v", r)
	}
	if err := runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 50); err != nil {
		t.Fatal(err)
	}
	page, err := f.service.ListCandidates(f.ctx, f.scope, f.auth, memory.ListCandidatesInput{ExtractionID: id, Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "pending" {
		t.Fatalf("candidate review page: %+v %v", page, err)
	}
	candidate := page.Items[0]
	ref := f.scope.Ref(candidate.CandidateID, candidate.Revision)
	memoryID := api.NewID("memory")
	r = f.command(t, "memory.create", memoryID, nil, memory.CreateInput{MemoryID: memoryID, Values: values, CandidateRef: &ref})
	if r.Stage != "applied" {
		t.Fatalf("save candidate: %+v", r)
	}
	secondID := api.NewID("memory")
	r = f.command(t, "memory.create", secondID, nil, memory.CreateInput{MemoryID: secondID, Values: values, CandidateRef: &ref})
	if r.Stage != "rejected" {
		t.Fatalf("second memory accepted for same candidate: %+v", r)
	}
	one := uint64(1)
	r = f.command(t, "memory.delete", memoryID, &one, memory.DeleteInput{MemoryID: memoryID, Reason: "停止长期记忆"})
	if r.Stage != "applied" {
		t.Fatalf("delete memory: %+v", r)
	}
	if err = runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 100); err != nil {
		t.Fatal(err)
	}
	record, err := f.service.InspectMemory(f.ctx, f.scope, f.auth, memoryID)
	if err != nil || record.State != "deleted" || record.CleanupState != "complete" {
		t.Fatalf("metadata cleanup: %+v %v", record, err)
	}
	var result memory.MemoryOutput
	if err = json.Unmarshal(r.Output, &result); err != nil {
		t.Fatal(err)
	}
	if result.MemoryRef.ObjectID != memoryID {
		t.Fatalf("delete changed original identity: %+v", result)
	}
}
