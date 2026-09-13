package contextassembly_test

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"

	"lerna/adapters/memorycleanup"
	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
	"lerna/memory"
)

func TestSourceCleanupErasesCheckpointIntegrity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	store, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	assembly, err := contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
	if err != nil {
		t.Fatal(err)
	}
	req := processRequest()
	if _, err = assembly.Assemble(ctx, req); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Read(ctx, req.Key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(snapshot.Document)
	if err = store.BindCheckpoint(ctx, req.Key, digest); err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewContexts(store)
	if err != nil {
		t.Fatal(err)
	}
	event := memory.SourceEvent{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}, Kind: memory.SourceDeleted, Revision: 2, Position: 2}
	if applied, e := sink.Apply(ctx, event); e != nil || !applied {
		t.Fatalf("source cleanup: %v %v", applied, e)
	}
	if err = store.VerifyCheckpoint(ctx, req.Key); err != contextassembly.Invalidated {
		t.Fatalf("source cleanup retained comparison: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.VerifyCheckpoint(ctx, req.Key); err != contextassembly.Invalidated {
		t.Fatalf("comparison recovered from erased snapshot: %v", err)
	}
	if err = store.BindCheckpoint(ctx, req.Key, digest); err != contextassembly.Invalidated {
		t.Fatalf("late checkpoint restored old comparison: %v", err)
	}
}
