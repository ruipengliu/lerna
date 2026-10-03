package memory_test

import (
	"testing"

	"github.com/ruipengliu/lerna/internal/memory"
)

func TestMemoryListKeepsOriginalFiniteSnapshotWhenNewRecordAppears(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"memory_00000000000000000000000000000001", "memory_00000000000000000000000000000002"} {
		r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, id)})
		if r.Stage != "applied" {
			t.Fatalf("create: %+v", r)
		}
	}
	page, err := f.service.ListMemory(f.ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	third := "memory_00000000000000000000000000000003"
	r := f.command(t, "memory.create", third, nil, memory.CreateInput{MemoryID: third, Values: f.values(t, third)})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	next, err := f.service.ListMemory(f.ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].MemoryID == third || !next.Exhausted || next.CollectionRevision != page.CollectionRevision {
		t.Fatalf("list changed original snapshot: first %+v second %+v", page, next)
	}
}
