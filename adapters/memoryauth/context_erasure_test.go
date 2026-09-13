package memoryauth_test

import (
	"context"
	"testing"

	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
)

func checkContextErasure(t *testing.T, store *sqlitecontext.Store, path string, key contextassembly.Key, source contextassembly.Reference) {
	t.Helper()
	ctx := context.Background()
	original, err := store.Read(ctx, key)
	if err != nil || len(original.Document) == 0 {
		t.Fatalf("original snapshot: %+v %v", original, err)
	}
	other := original
	other.Key.TaskID = "unrelated-context"
	other.Document = []byte(`{"FactsSHA256":"","Dependencies":[],"Status":{}}`)
	if _, err = store.Bind(ctx, other); err != nil {
		t.Fatal(err)
	}
	change := contextassembly.SourceInvalidation{Namespace: source.Namespace, Collection: source.Collection, Key: source.Key, ThroughRevision: source.Revision}
	if count, err := store.InvalidateSource(ctx, change); err != nil || count != 1 {
		t.Fatalf("snapshot cleanup: %d %v", count, err)
	}
	if got, err := store.Read(ctx, key); err != contextassembly.Invalidated || len(got.Document) != 0 || got.SemanticSHA256 != "" {
		t.Fatalf("erased snapshot disclosure: %+v %v", got, err)
	}
	if _, err := store.Read(ctx, other.Key); err != nil {
		t.Fatalf("unrelated snapshot removed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.Bind(ctx, original); err != contextassembly.Invalidated {
		t.Fatalf("erased decision rebound: %v", err)
	}
	original.Key.TaskID = "late-old-source"
	if _, err = reopened.Bind(ctx, original); err != contextassembly.Invalidated {
		t.Fatalf("stale source rebound under fresh decision: %v", err)
	}
	if count, err := reopened.InvalidateSource(ctx, change); err != nil || count != 0 {
		t.Fatalf("repeat cleanup: %d %v", count, err)
	}
}
