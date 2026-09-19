package sqlite_test

import (
	"context"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/memory"
	"path/filepath"
	"testing"
)

func TestDeletePersistsTombstoneAndPreventsHistoricalReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "private"}
	write := memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"private":"original"}`)}}
	if _, err = s.Commit(ctx, write); err != nil {
		t.Fatal(err)
	}
	deletion := memory.Deletion{OperationID: "delete", Subject: "alice", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 1}
	got, err := s.Delete(ctx, deletion)
	if err != nil || got.Revision != 2 || got.Position != 2 {
		t.Fatalf("delete: %+v %v", got, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, ref, 1); err != memory.Missing {
		t.Fatalf("old body readable: %v", err)
	}
	records, err := s.Scan(ctx, ref.Namespace, ref.Collection)
	if err != nil || len(records) != 0 {
		t.Fatalf("deleted query: %+v %v", records, err)
	}
	head, err := s.Head(ctx, ref)
	if err != nil || head != 2 {
		t.Fatalf("lost tombstone: %d %v", head, err)
	}
	repeat, err := s.Delete(ctx, deletion)
	if err != nil || repeat != got {
		t.Fatalf("delete replay: %+v %v", repeat, err)
	}
	old, err := s.LookupOperation(ctx, ref.Namespace, "put")
	if err != nil || old.SemanticSHA256 != "" || old.Revision != 1 {
		t.Fatalf("old result: %+v %v", old, err)
	}
	if _, err = s.Commit(ctx, write); err != memory.ReplayUnavailable {
		t.Fatalf("old payload resubmission: %v", err)
	}
	write.OperationID = "recreate"
	if _, err = s.Commit(ctx, write); err != memory.Conflict {
		t.Fatalf("reused identity: %v", err)
	}
	changes, err := s.ReadChanges(ctx, ref.Namespace, ref.Collection, 1, 10)
	if err != nil || len(changes) != 1 || changes[0] != got {
		t.Fatalf("deletion change: %+v %v", changes, err)
	}
}

func TestDeleteAndCorrectionHaveOneDurableWinner(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	a, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "race"}
	in := memory.Change{OperationID: "put", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"v":1}`)}}
	if _, err = a.Commit(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.OperationID = "correct"
	in.Expected = 1
	in.Record.Revision = 2
	in.Record.Document = []byte(`{"v":2}`)
	type outcome struct {
		deleting bool
		err      error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, e := a.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "alice", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 1})
		results <- outcome{true, e}
	}()
	go func() { <-start; _, e := b.Commit(ctx, in); results <- outcome{false, e} }()
	close(start)
	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	if first.err != nil || second.err != memory.Conflict {
		t.Fatalf("competing writes: %+v %+v", first, second)
	}
	changes, err := a.ReadChanges(ctx, ref.Namespace, ref.Collection, 0, 10)
	if err != nil || len(changes) != 2 || changes[1].Revision != 2 || changes[1].Position != 2 {
		t.Fatalf("non-atomic changes: %+v %v", changes, err)
	}
	if first.deleting {
		if _, err = a.Read(ctx, ref, 1); err != memory.Missing {
			t.Fatalf("deleted body: %v", err)
		}
	} else {
		row, err := a.Read(ctx, ref, 2)
		if err != nil || string(row.Document) != `{"v":2}` {
			t.Fatalf("lost correction: %+v %v", row, err)
		}
	}
}
