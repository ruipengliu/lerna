package sqlitememory_test

import (
	"context"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
	"lerna/memory/storecontract"
	"path/filepath"
	"testing"
)

func TestCommitSurvivesReopenWithOriginalReceiptAndChange(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preferred-format"}
	in := memory.Change{OperationID: "window-1:save", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"format":"concise"}`)}}
	receipt, err := s.Commit(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Ref != ref || receipt.Revision != 1 || receipt.Position != 1 {
		t.Fatalf("receipt: %+v", receipt)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	record, err := s.Read(ctx, ref, 1)
	if err != nil || string(record.Document) != `{"format":"concise"}` {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	original, err := s.LookupOperation(ctx, "local", "window-1:save")
	if err != nil || original != receipt {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	repeated, err := s.Commit(ctx, in)
	if err != nil || repeated != receipt {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	changes, err := s.ReadChanges(ctx, "local", "personal", 0, 10)
	if err != nil || len(changes) != 1 || changes[0] != receipt {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
}

func TestCompetingCorrectionsPreserveHistoryAndOnlyOneChange(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	create := memory.Change{OperationID: "save", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"format":"concise"}`)}}
	if _, err = first.Commit(ctx, create); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, s := range []*sqlitememory.Store{first, second} {
		in := create
		in.Expected = 1
		in.Record.Revision = 2
		if i == 0 {
			in.OperationID = "correct-a"
			in.Record.Document = []byte(`{"format":"detailed"}`)
		} else {
			in.OperationID = "correct-b"
			in.Record.Document = []byte(`{"format":"bullets"}`)
		}
		go func(s *sqlitememory.Store, in memory.Change) { <-start; _, e := s.Commit(ctx, in); results <- e }(s, in)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		switch e := <-results; e {
		case nil:
			success++
		case memory.Conflict:
			conflict++
		default:
			t.Fatalf("unexpected %v", e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	history, err := first.Read(ctx, ref, 1)
	if err != nil || string(history.Document) != `{"format":"concise"}` {
		t.Fatalf("history %+v err %v", history, err)
	}
	if _, err = first.Read(ctx, ref, 3); err != memory.Missing {
		t.Fatalf("missing revision: %v", err)
	}
	changes, err := first.ReadChanges(ctx, "local", "personal", 0, 10)
	if err != nil || len(changes) != 2 || changes[1].Position != 2 {
		t.Fatalf("changes %+v err %v", changes, err)
	}
	original, err := first.Commit(ctx, create)
	if err != nil || original.Revision != 1 {
		t.Fatalf("replay after correction %+v %v", original, err)
	}
	create.Record.Document = []byte(`{"format":"changed"}`)
	if _, err = first.Commit(ctx, create); err != memory.IdentityConflict {
		t.Fatalf("same identity changed document %v", err)
	}
	rejected := create
	rejected.OperationID = "stale"
	rejected.Expected = 1
	rejected.Record.Revision = 2
	if _, err = first.Commit(ctx, rejected); err != memory.Conflict {
		t.Fatalf("stale %v", err)
	}
	if _, err = first.LookupOperation(ctx, "local", "stale"); err != memory.Missing {
		t.Fatalf("failed correction retained receipt %v", err)
	}
}

func TestReadBindingKeepsOriginalRevisionsAfterCorrectionAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, e := sqlitememory.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	change := memory.Change{OperationID: "save", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"text":"concise"}`)}}
	if _, e = store.Commit(ctx, change); e != nil {
		t.Fatal(e)
	}
	selection := memory.ReadBinding{Namespace: "local", ID: "single-read", Subject: "alice", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PermitID: "grant-1", Results: []memory.VersionRef{{Ref: ref, Revision: 1}}, Coverage: "complete"}
	first, e := store.BindRead(ctx, selection)
	if e != nil || len(first.Results) != 1 || first.Results[0].Revision != 1 {
		t.Fatalf("first %+v %v", first, e)
	}
	change.OperationID = "correct"
	change.Expected = 1
	change.Record.Revision = 2
	change.Record.Document = []byte(`{"text":"detailed"}`)
	if _, e = store.Commit(ctx, change); e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	store, e = sqlitememory.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	selection.Results[0].Revision = 2
	retry, e := store.BindRead(ctx, selection)
	if e != nil || len(retry.Results) != 1 || retry.Results[0].Revision != 1 {
		t.Fatalf("expanded retry %+v %v", retry, e)
	}
	original, e := store.LookupRead(ctx, "local", "single-read")
	if e != nil || len(original.Results) != 1 || original.Results[0].Revision != 1 {
		t.Fatalf("original %+v %v", original, e)
	}
	selection.SemanticSHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if _, e = store.BindRead(ctx, selection); e != memory.IdentityConflict {
		t.Fatalf("semantic change %v", e)
	}
	empty := memory.ReadBinding{Namespace: "local", ID: "empty-read", Subject: "alice", SemanticSHA256: selection.SemanticSHA256, PermitID: "grant-2", Coverage: "complete"}
	if _, e = store.BindRead(ctx, empty); e != nil {
		t.Fatal(e)
	}
	empty.Results = selection.Results
	result, e := store.BindRead(ctx, empty)
	if e != nil || len(result.Results) != 0 {
		t.Fatalf("empty changed %+v %v", result, e)
	}
}

func TestScanReturnsOnlyCurrentCollectionRevisions(t *testing.T) {
	ctx := context.Background()
	store, e := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	change := memory.Change{OperationID: "save", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "format"}, Revision: 1, Document: []byte(`{"text":"concise"}`)}}
	if _, e = store.Commit(ctx, change); e != nil {
		t.Fatal(e)
	}
	change.Expected = 1
	change.Record.Revision = 2
	change.OperationID = "correct"
	change.Record.Document = []byte(`{"text":"detailed"}`)
	if _, e = store.Commit(ctx, change); e != nil {
		t.Fatal(e)
	}
	change.Expected = 0
	change.Record.Revision = 1
	change.OperationID = "other"
	change.Record.Ref.Collection = "other"
	if _, e = store.Commit(ctx, change); e != nil {
		t.Fatal(e)
	}
	rows, e := store.Scan(ctx, "local", "personal")
	if e != nil || len(rows) != 1 || rows[0].Revision != 2 || string(rows[0].Document) != `{"text":"detailed"}` {
		t.Fatalf("scan %+v %v", rows, e)
	}
}

func TestSharedMemoryStoreContract(t *testing.T) {
	for _, name := range storecontract.Cases() {
		t.Run(name, func(t *testing.T) {
			open := func(ctx context.Context) (memory.QueryStore, func() error, error) {
				s, e := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
				if e != nil {
					return nil, nil, e
				}
				return s, s.Close, nil
			}
			if e := storecontract.Check(context.Background(), open, name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
