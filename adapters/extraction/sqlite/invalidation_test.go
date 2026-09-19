package sqlite_test

import (
	"context"
	"fmt"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"testing"
)

func TestSourceInvalidationRetiresWholeCandidateAndFencesNewOperations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := processRecord(t)
	fragment := "paragraph:2"
	r.Candidate.Sources = append(r.Candidate.Sources, &wire.MemorySource{Ref: &wire.ContentSource{Kind: "note", Key: "two", Revision: 1}, Method: "authenticated-note", Fragment: &fragment})
	if err = s.Commit(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveSave(ctx, "local", "alice", "original", &wire.MemoryWrite{OperationId: "save-original", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "original"}, Spec: &wire.MemorySpec{About: "alice"}}); err != nil {
		t.Fatal(err)
	}
	event := extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}
	count, err := s.InvalidateSource(ctx, event)
	if err != nil || count != 1 {
		t.Fatalf("invalidate: %d %v", count, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Lookup(ctx, "local", "original"); err != memory.Missing {
		t.Fatalf("mixed candidate retained: %v", err)
	}
	state, err := s.Inspect(ctx, "local", "original")
	if err != nil || state.State != "retired" || !state.Committed {
		t.Fatalf("lost effect: %+v %v", state, err)
	}
	intent, err := s.LookupSave(ctx, "local", "alice", "original")
	if err != nil || intent.State != "retired" || intent.Request.Spec != nil || intent.Request.OperationId != "save-original" {
		t.Fatalf("save cleanup: %+v %v", intent, err)
	}
	count, err = s.InvalidateSource(ctx, event)
	if err != nil || count != 0 {
		t.Fatalf("repeat cleanup: %d %v", count, err)
	}
	r.OperationID = "replacement"
	if err = s.Commit(ctx, r); err != memory.ReplayUnavailable {
		t.Fatalf("old source rebuilt under new operation: %v", err)
	}
	r.Candidate.Sources[0].Ref.Revision = 2
	if err = s.Commit(ctx, r); err != nil {
		t.Fatalf("new revision: %v", err)
	}
	// A namespace is an independent source authority.
	r.Namespace = "other"
	r.OperationID = "other-original"
	r.Candidate.Sources[0].Ref.Revision = 1
	if err = s.Commit(ctx, r); err != nil {
		t.Fatalf("namespace isolation: %v", err)
	}
	event.ThroughRevision = 2
	count, err = s.InvalidateSource(ctx, event)
	if err != nil || count != 1 {
		t.Fatalf("advance fence: %d %v", count, err)
	}
	event.ThroughRevision = 1
	if _, err = s.InvalidateSource(ctx, event); err != nil {
		t.Fatal(err)
	}
	r.Namespace = "local"
	r.OperationID = "late-second-revision"
	r.Candidate.Sources[0].Ref.Revision = 2
	if err = s.Commit(ctx, r); err != memory.ReplayUnavailable {
		t.Fatalf("watermark regressed: %v", err)
	}
}

func TestSourceFenceSerializesAgainstConcurrentCandidateCommit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidate.db")
	writer, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	invalidator, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer invalidator.Close()
	for i := 0; i < 12; i++ {
		r := processRecord(t)
		r.Namespace = fmt.Sprintf("namespace-%d", i)
		start := make(chan struct{})
		commit := make(chan error, 1)
		fence := make(chan error, 1)
		go func() { <-start; commit <- writer.Commit(ctx, r) }()
		go func() {
			<-start
			_, e := invalidator.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: r.Namespace, Kind: "note", Key: "one", ThroughRevision: 1})
			fence <- e
		}()
		close(start)
		commitErr := <-commit
		if commitErr != nil && commitErr != memory.ReplayUnavailable {
			t.Fatal(commitErr)
		}
		if err = <-fence; err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Lookup(ctx, r.Namespace, r.OperationID); err != memory.Missing {
			t.Fatalf("concurrent invalidation left body: %v", err)
		}
		state, e := writer.Inspect(ctx, r.Namespace, r.OperationID)
		if commitErr == nil && (e != nil || state.State != "retired" || !state.Committed) {
			t.Fatalf("lost winning commit fact: %+v %v", state, e)
		}
		r.OperationID = "new-operation"
		if err = writer.Commit(ctx, r); err != memory.ReplayUnavailable {
			t.Fatalf("new operation bypassed fence: %v", err)
		}
	}
}
