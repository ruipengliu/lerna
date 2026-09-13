package sqlitecontext_test

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"strings"
	"testing"

	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
)

func TestCheckpointIntegrityIsBoundAndErasedWithSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	s, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	checkpoint, ok := any(s).(contextassembly.CheckpointStore)
	if !ok {
		t.Fatal("store cannot own checkpoint integrity")
	}
	key := contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}
	original := contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Document: []byte(`{"body":"public test"}`)}
	if _, err = s.Bind(ctx, original); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original.Document)
	if err = checkpoint.VerifyCheckpoint(ctx, key); err != contextassembly.Missing {
		t.Fatalf("unbound checkpoint: %v", err)
	}
	wrong := sha256.Sum256([]byte(`{"body":"other"}`))
	if err = checkpoint.BindCheckpoint(ctx, key, wrong); err != contextassembly.IdentityConflict {
		t.Fatalf("wrong body accepted: %v", err)
	}
	if err = checkpoint.BindCheckpoint(ctx, key, digest); err != nil {
		t.Fatal(err)
	}
	if err = checkpoint.BindCheckpoint(ctx, key, digest); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint = any(s).(contextassembly.CheckpointStore)
	if err = checkpoint.VerifyCheckpoint(ctx, key); err != nil {
		t.Fatalf("lost original comparison: %v", err)
	}
	if err = s.Retire(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = checkpoint.VerifyCheckpoint(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("comparison survived retirement: %v", err)
	}
	if err = checkpoint.BindCheckpoint(ctx, key, digest); err != contextassembly.Invalidated {
		t.Fatalf("late checkpoint revived comparison: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = any(s).(contextassembly.CheckpointStore).VerifyCheckpoint(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("retirement lost after reopen: %v", err)
	}
}

func TestConcurrentCheckpointBindingCannotOutliveRetirement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	a, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	key := contextassembly.Key{Namespace: "local", TaskID: "competing-checkpoint", Decision: 1}
	snapshot := contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Document: []byte(`{"body":"public test"}`)}
	if _, err = a.Bind(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	bound := make(chan error, 1)
	retired := make(chan error, 1)
	go func() { <-start; bound <- a.BindCheckpoint(ctx, key, sha256.Sum256(snapshot.Document)) }()
	go func() { <-start; retired <- b.Retire(ctx, key) }()
	close(start)
	if err = <-bound; err != nil && err != contextassembly.Invalidated {
		t.Fatalf("binding: %v", err)
	}
	if err = <-retired; err != nil {
		t.Fatal(err)
	}
	if err = a.VerifyCheckpoint(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("late integrity escaped cleanup: %v", err)
	}
}
