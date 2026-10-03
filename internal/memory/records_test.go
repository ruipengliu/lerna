package memory_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func (f fixture) values(t *testing.T, body string) memory.MemoryValues {
	t.Helper()
	ref := f.upload(t, body)
	scope := f.upload(t, "本人本地跨任务记忆范围")
	return memory.MemoryValues{Type: "fact", ContentRef: ref, Sources: []api.SourceEvidence{}, ScopeRef: scope, PolicyRef: f.policy.PolicyRef, ObservedAt: api.Time(time.Now().Add(-time.Hour))}
}

func TestMemoryCorrectionChangesCurrentAssertionAndDeleteRemainsInspectable(t *testing.T) {
	f := newFixture(t)
	id := api.NewID("memory")
	values := f.values(t, "用户住在杭州")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	one := uint64(1)
	values.ContentRef = f.upload(t, "用户已迁居成都")
	r = f.command(t, "memory.replace", id, &one, memory.ReplaceInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("replace: %+v", r)
	}
	record, err := f.service.ReadMemory(f.ctx, f.scope, f.auth, memory.ReadMemoryInput{MemoryID: id})
	if err != nil || record.Revision != 2 || record.Values.ContentRef != values.ContentRef {
		t.Fatalf("corrected current record: %+v %v", record, err)
	}
	two := uint64(2)
	r = f.command(t, "memory.delete", id, &two, memory.DeleteInput{MemoryID: id, Reason: "本人删除"})
	if r.Stage != "applied" {
		t.Fatalf("delete: %+v", r)
	}
	if _, err = f.service.ReadMemory(f.ctx, f.scope, f.auth, memory.ReadMemoryInput{MemoryID: id}); !api.IsCode(err, "gone") {
		t.Fatalf("deleted record is ordinary readable: %v", err)
	}
	record, err = f.service.InspectMemory(f.ctx, f.scope, f.auth, id)
	if err != nil || record.State != "deleted" || record.CleanupState != "pending" {
		t.Fatalf("delete lost management responsibility: %+v %v", record, err)
	}
	var output memory.MemoryOutput
	if err = json.Unmarshal(r.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.MemoryRef.Revision != 3 {
		t.Fatalf("unexpected delete revision: %+v", output)
	}
}
