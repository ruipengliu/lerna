package memory_test

import (
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func TestControlledViewRepeatsUnackedPageThenDeliversContinuousChanges(t *testing.T) {
	f := newFixture(t)
	id := api.NewID("memory")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, "原快照记录")})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	viewID := api.NewID("view")
	r = f.command(t, "memory.view.open", viewID, nil, memory.OpenViewInput{ViewID: viewID, ScopeRef: f.upload(t, "本人受控同步范围"), Purposes: []string{"memory.sync"}, HolderRef: f.auth.Ref(f.scope.OwnerID), Location: "local", MaxCandidates: 200})
	if r.Stage != "applied" {
		t.Fatalf("open: %+v", r)
	}
	page, err := f.service.PullView(f.ctx, f.scope, f.auth, memory.PullViewInput{ViewID: viewID, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].MemoryID != id || !page.SnapshotComplete {
		t.Fatalf("snapshot: %+v", page)
	}
	repeat, err := f.service.PullView(f.ctx, f.scope, f.auth, memory.PullViewInput{ViewID: viewID, Limit: 20})
	if err != nil || !api.Equal(page, repeat) {
		t.Fatalf("lost ack changed original page: %+v %v", repeat, err)
	}
	r = f.command(t, "memory.view.ack", viewID, nil, memory.AckViewInput{ViewID: viewID, Cursor: page.Cursor, ReceiptRef: f.scope.Ref(api.NewID("receipt"), 1)})
	if r.Stage != "applied" {
		t.Fatalf("ack: %+v", r)
	}
	second := api.NewID("memory")
	r = f.command(t, "memory.create", second, nil, memory.CreateInput{MemoryID: second, Values: f.values(t, "水位之后的新记录")})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	changes, err := f.service.PullView(f.ctx, f.scope, f.auth, memory.PullViewInput{ViewID: viewID, Cursor: page.Cursor, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || len(changes.Records) != 1 || changes.Records[0].MemoryID != second {
		t.Fatalf("continuous delta: %+v", changes)
	}
}
