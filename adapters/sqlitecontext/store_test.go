package sqlitecontext_test

import (
	"context"
	"fmt"
	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
	"path/filepath"
	"testing"
)

func TestFirstSnapshotSurvivesCompetingSelectionAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.db")
	first, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	second, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	key := contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}
	original := contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Document: []byte(`{"sources":[{"revision":1}]}`)}
	a, e := first.Bind(ctx, original)
	if e != nil {
		t.Fatal(e)
	}
	newer := original
	newer.Document = []byte(`{"sources":[{"revision":2}]}`)
	b, e := second.Bind(ctx, newer)
	if e != nil || string(b.Document) != string(a.Document) {
		t.Fatalf("replaced snapshot %s %v", b.Document, e)
	}
	newer.SemanticSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, e = second.Bind(ctx, newer); e != contextassembly.IdentityConflict {
		t.Fatalf("changed intent %v", e)
	}
	if e = first.Close(); e != nil {
		t.Fatal(e)
	}
	if e = second.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := reopened.Read(ctx, key)
	if e != nil || string(got.Document) != string(original.Document) {
		t.Fatalf("reopen %s %v", got.Document, e)
	}
}

func TestConcurrentBindingsHaveOneDurableWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.db")
	a, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := sqlitecontext.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	key := contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 2}
	start := make(chan struct{})
	results := make(chan contextassembly.Snapshot, 2)
	errors := make(chan error, 2)
	for i, store := range []*sqlitecontext.Store{a, b} {
		go func() {
			<-start
			s, e := store.Bind(context.Background(), contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Document: []byte(fmt.Sprintf(`{"revision":%d}`, i+1))})
			results <- s
			errors <- e
		}()
	}
	close(start)
	first, second := <-results, <-results
	for range 2 {
		if e := <-errors; e != nil {
			t.Fatal(e)
		}
	}
	if string(first.Document) != string(second.Document) {
		t.Fatalf("two winners %s %s", first.Document, second.Document)
	}
	got, e := a.Read(context.Background(), key)
	if e != nil || string(got.Document) != string(first.Document) {
		t.Fatalf("durable winner %s %v", got.Document, e)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	key.Decision = 3
	if _, e = a.Bind(cancelled, contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: first.SemanticSHA256, Document: first.Document}); e == nil {
		t.Fatal("cancelled commit accepted")
	}
	if _, e = a.Read(context.Background(), key); e != contextassembly.Missing {
		t.Fatalf("cancelled snapshot persisted %v", e)
	}
}
