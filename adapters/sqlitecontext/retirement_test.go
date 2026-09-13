package sqlitecontext_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
)

func TestRetirementBeforeSnapshotBindingSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	s, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key := contextassembly.Key{Namespace: "local", TaskID: "late-context", Decision: 1}
	if err := s.Retire(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Read(ctx, key); err != contextassembly.Invalidated || len(got.Document) != 0 {
		t.Fatalf("lost pre-binding retirement: %+v %v", got, err)
	}
	candidate := contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Document: []byte(`{"body":"public test"}`)}
	if _, err := s.Bind(ctx, candidate); err != contextassembly.Invalidated {
		t.Fatalf("late body accepted: %v", err)
	}
	if err := s.Retire(ctx, key); err != nil {
		t.Fatalf("retirement replay: %v", err)
	}
	candidate.Key.Decision = 2
	if _, err := s.Bind(ctx, candidate); err != nil {
		t.Fatalf("unrelated decision blocked: %v", err)
	}
	if err := s.Retire(ctx, candidate.Key); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read(ctx, candidate.Key); err != contextassembly.Invalidated || got.SemanticSHA256 != "" || len(got.Document) != 0 {
		t.Fatalf("retained body survived retirement: %+v %v", got, err)
	}
}

func TestRetiredUnboundIdentitiesConsumeSnapshotCapacity(t *testing.T) {
	ctx := context.Background()
	s, err := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 512; i++ {
		if err := s.Retire(ctx, contextassembly.Key{Namespace: "local", TaskID: fmt.Sprintf("retired-%d", i), Decision: 1}); err != nil {
			t.Fatal(err)
		}
	}
	key := contextassembly.Key{Namespace: "local", TaskID: "new", Decision: 1}
	if err := s.Retire(ctx, key); err != contextassembly.Capacity {
		t.Fatalf("retirement exceeded capacity: %v", err)
	}
	if _, err := s.Bind(ctx, contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Document: []byte(`{}`)}); err != contextassembly.Capacity {
		t.Fatalf("binding ignored reserved identities: %v", err)
	}
	key.TaskID = "retired-0"
	if err := s.Retire(ctx, key); err != nil {
		t.Fatalf("full capacity rejected original retirement: %v", err)
	}
	if _, err := s.Read(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("old identity evicted: %v", err)
	}
}

func TestConcurrentRetirementAlwaysFencesSnapshotBinding(t *testing.T) {
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
	key := contextassembly.Key{Namespace: "local", TaskID: "competing-context", Decision: 1}
	start := make(chan struct{})
	bound := make(chan error, 1)
	retired := make(chan error, 1)
	go func() {
		<-start
		_, err := a.Bind(ctx, contextassembly.Snapshot{Key: key, Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Document: []byte(`{"body":"public test"}`)})
		bound <- err
	}()
	go func() { <-start; retired <- b.Retire(ctx, key) }()
	close(start)
	if err := <-bound; err != nil && err != contextassembly.Invalidated {
		t.Fatalf("binding outcome: %v", err)
	}
	if err := <-retired; err != nil {
		t.Fatal(err)
	}
	if _, err := a.Read(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("race resurrected snapshot: %v", err)
	}
}
