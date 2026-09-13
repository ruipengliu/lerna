package grpcbinding_test

import (
	"context"
	"lerna/adapters/grpcbinding"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
)

func TestDurableDelivery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "delivery.db")
	j, e := grpcbinding.OpenJournal(path, 2)
	if e != nil {
		t.Fatal(e)
	}
	u := &wire.InvocationUpdate{OperationId: "op", Seq: 2, FullSnapshot: true, Reliable: true, Snapshot: &wire.InvocationSnapshot{Revision: 2}}
	fresh, e := j.Save(ctx, "peer", u)
	if e != nil || !fresh {
		t.Fatalf("first %v %v", fresh, e)
	}
	fresh, e = j.Save(ctx, "peer", u)
	if e != nil || fresh {
		t.Fatalf("duplicate %v %v", fresh, e)
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	j, e = grpcbinding.OpenJournal(path, 2)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	rows, e := j.Pending(ctx, "peer", "op")
	if e != nil || len(rows) != 1 || rows[0].Seq != 2 {
		t.Fatalf("recovery %v %v", rows, e)
	}
	if e = j.Ack(ctx, "peer", "op", 3); e == nil {
		t.Fatal("unsent acknowledgement accepted")
	}
	if e = j.Ack(ctx, "peer", "op", 2); e != nil {
		t.Fatal(e)
	}
	rows, e = j.Pending(ctx, "peer", "op")
	if e != nil || len(rows) != 0 {
		t.Fatal("ack lost", e)
	}
	u.Snapshot.Revision = 3
	if _, e = j.Save(ctx, "peer", u); e == nil {
		t.Fatal("same identity changed")
	}
}
