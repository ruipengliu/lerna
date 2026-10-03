package contract_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestTxSnapshotRoutingStillRequiresCurrentLockedRevision(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			id := api.NewID("object")
			ctx := context.Background()
			status, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
				return tx.Create(ctx, "test.records", id, "", map[string]any{"name": "original parent"})
			})
			if e != nil || status != runtime.Committed {
				t.Fatalf("create %s %v", status, e)
			}
			status, e = f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
				reader, ok := tx.(runtime.TxSnapshotReader)
				if !ok {
					t.Fatal("adapter lacks nonlocking route snapshot")
				}
				var routed map[string]any
				revision, e := reader.Peek(ctx, "test.records", id, &routed)
				if e != nil || revision != 1 || routed["name"] != "original parent" {
					t.Fatalf("route snapshot %d %+v %v", revision, routed, e)
				}
				if _, e = reader.Peek(ctx, "other.records", id, &routed); !api.IsCode(e, "forbidden") {
					t.Fatalf("snapshot escaped participants %v", e)
				}
				return nil
			})
			if e != nil || status != runtime.Committed {
				t.Fatalf("snapshot transaction %s %v", status, e)
			}
		})
	}
}

func TestPostgresRouteSnapshotDoesNotHoldLowerObjectLock(t *testing.T) {
	f := fixture(t, "postgres", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	id := api.NewID("object")
	status, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
		return tx.Create(ctx, "test.records", id, "", map[string]any{"name": "original parent"})
	})
	if e != nil || status != runtime.Committed {
		t.Fatalf("create %s %v", status, e)
	}
	peeked, updated := make(chan struct{}), make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		_, e := f.store.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
			reader, ok := tx.(runtime.TxSnapshotReader)
			if !ok {
				close(peeked)
				return api.E("unsupported", "snapshot_unconfigured")
			}
			var routed map[string]any
			if _, e := reader.Peek(ctx, "test.records", id, &routed); e != nil {
				close(peeked)
				return e
			}
			close(peeked)
			select {
			case <-updated:
			case <-ctx.Done():
				return ctx.Err()
			}
			var current map[string]any
			revision, e := tx.Get(ctx, "test.records", id, &current)
			if e != nil || revision != 2 || current["name"] != "new parent" {
				return api.E("invalid_state", "route_snapshot_was_authoritative")
			}
			return nil
		})
		readDone <- e
	}()
	<-peeked
	status, e = f.open(nil).Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
		return tx.Put(ctx, "test.records", id, 1, map[string]any{"name": "new parent"})
	})
	close(updated)
	if e != nil || status != runtime.Committed {
		t.Fatalf("route snapshot blocked object write %s %v", status, e)
	}
	if e = <-readDone; e != nil {
		t.Fatal(e)
	}
}
