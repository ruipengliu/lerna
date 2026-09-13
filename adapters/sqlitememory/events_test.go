package sqlitememory_test

import (
	"context"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
	"path/filepath"
	"testing"
)

func TestSourceEventsRemainReadableAfterBodyDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "event-source"}
	first := memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"secret":"first"}`)}}
	if _, err = s.Commit(ctx, first); err != nil {
		t.Fatal(err)
	}
	next := first
	next.OperationID = "correct"
	next.Expected = 1
	next.Record.Revision = 2
	next.Record.Document = []byte(`{"secret":"second"}`)
	if _, err = s.Commit(ctx, next); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ErasedOperations(ctx, ref, 3); err != memory.Conflict {
		t.Fatalf("uncommitted deletion treated as proof: %v", err)
	}
	if _, err = s.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "alice", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 2}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.ReadEvents(ctx, "local", "personal", 0, 2)
	if err != nil || len(events) != 2 {
		t.Fatalf("event page: %+v %v", events, err)
	}
	if events[0] != (memory.SourceEvent{Ref: ref, Kind: "created", Revision: 1, Position: 1}) || events[1] != (memory.SourceEvent{Ref: ref, Kind: "corrected", Revision: 2, Position: 2}) {
		t.Fatalf("lost original source changes: %+v", events)
	}
	last, err := s.ReadEvents(ctx, "local", "personal", events[1].Position, 2)
	if err != nil || len(last) != 1 || last[0] != (memory.SourceEvent{Ref: ref, Kind: "deleted", Revision: 3, Position: 3}) {
		t.Fatalf("lost deletion event: %+v %v", last, err)
	}
	other, err := s.ReadEvents(ctx, "other", "personal", 0, 2)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-namespace events: %+v %v", other, err)
	}
	erased, err := s.ErasedOperations(ctx, ref, 3)
	if err != nil || len(erased) != 2 || erased[0].OperationID != "put" || erased[1].OperationID != "correct" || erased[0].SemanticSHA256 != "" || erased[1].SemanticSHA256 != "" {
		t.Fatalf("erased operation metadata: %+v %v", erased, err)
	}
	if _, err = s.ErasedOperations(ctx, ref, 2); err != memory.Conflict {
		t.Fatalf("wrong deletion revision accepted: %v", err)
	}
}

func TestConsumerProgressBindsConfigurationAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "artifact-cleaner", ConfigSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	position, err := s.BindConsumer(ctx, binding)
	if err != nil || position != 0 {
		t.Fatalf("initial cursor: %d %v", position, err)
	}
	altered := binding
	altered.ConfigSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = s.BindConsumer(ctx, altered); err != memory.IdentityConflict {
		t.Fatalf("replaced consumer config: %v", err)
	}
	if err = s.AckEvent(ctx, binding, 0, 1); err != memory.Conflict {
		t.Fatalf("acknowledged nonexistent event: %v", err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "source"}
	if _, err = s.Commit(ctx, memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: binding.ConfigSHA256, Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"text":"value"}`)}}); err != nil {
		t.Fatal(err)
	}
	if err = s.AckEvent(ctx, binding, 0, 2); err == nil {
		t.Fatal("skipped event")
	}
	if err = s.AckEvent(ctx, binding, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	position, err = s.BindConsumer(ctx, binding)
	if err != nil || position != 1 {
		t.Fatalf("lost applied position: %d %v", position, err)
	}
	if err = s.AckEvent(ctx, binding, 0, 1); err != nil {
		t.Fatalf("ack replay: %v", err)
	}
	if err = s.AckEvent(ctx, altered, 0, 1); err != memory.IdentityConflict {
		t.Fatalf("replayed wrong config: %v", err)
	}
}
