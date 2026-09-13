package sqlitememory_test

import (
	"context"
	"fmt"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func sourceRevision(ref memory.Ref, revision uint64, source string) memory.Change {
	return memory.Change{OperationID: fmt.Sprintf("%s-%d", ref.Key, revision), Subject: "alice", SemanticSHA256: strings.Repeat("a", 64), Expected: revision - 1, Record: memory.Revision{Ref: ref, Revision: revision, Document: []byte(fmt.Sprintf(`{"spec":{"sources":[{"ref":{"kind":"note","key":%q,"revision":"1"}}]}}`, source))}}
}
func TestSourceErasurePreservesIndependentCorrectionAndOriginalFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "preference"}
	old := sourceRevision(ref, 1, "restricted")
	next := sourceRevision(ref, 2, "independent")
	for _, in := range []memory.Change{old, next} {
		if _, err = s.Commit(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	invalid := memory.SourceErasure{Namespace: "local", Kind: "note", Key: "restricted", ThroughRevision: 1}
	if n, e := s.EraseSource(ctx, invalid); e != nil || n != 1 {
		t.Fatalf("erase: %d %v", n, e)
	}
	if _, e := s.Read(ctx, ref, 1); e != memory.Missing {
		t.Fatalf("old body: %v", e)
	}
	if row, e := s.Read(ctx, ref, 2); e != nil || string(row.Document) != string(next.Record.Document) {
		t.Fatalf("independent body: %v", e)
	}
	original, err := s.LookupOperation(ctx, "local", old.OperationID)
	if err != nil || original.Revision != 1 || original.SemanticSHA256 != "" {
		t.Fatalf("original fact: %+v %v", original, err)
	}
	if _, e := s.Commit(ctx, old); e != memory.ReplayUnavailable {
		t.Fatalf("erased replay: %v", e)
	}
	events, err := s.ReadEvents(ctx, "local", "personal", 0, 16)
	if err != nil || len(events) != 3 || events[2].Kind != memory.SourceErased || events[2].Revision != 1 {
		t.Fatalf("exact erasure event: %+v %v", events, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitememory.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if snapshot, e := s.RecoverySnapshot(ctx, memory.RecoveryScope{Namespace: "local", Collection: "personal"}, 0); e != nil || len(snapshot.Erased) != 1 || len(snapshot.SourceFences) != 1 {
		t.Fatalf("recovery lost exact erasure: %+v %v", snapshot, e)
	}
	if n, e := s.EraseSource(ctx, invalid); e != nil || n != 0 {
		t.Fatalf("repeat: %d %v", n, e)
	}
	later := sourceRevision(ref, 3, "restricted")
	if _, e := s.Commit(ctx, later); e != memory.Denied {
		t.Fatalf("old source returned after restart: %v", e)
	}
	if rows, e := s.Scan(ctx, "local", "personal"); e != nil || len(rows) != 1 || rows[0].Revision != 2 {
		t.Fatalf("latest independent: %+v %v", rows, e)
	}
}

func TestErasingCurrentRevisionDoesNotExposeAnOlderIndependentBody(t *testing.T) {
	ctx := context.Background()
	s, err := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "head"}
	for _, in := range []memory.Change{sourceRevision(ref, 1, "independent"), sourceRevision(ref, 2, "restricted")} {
		if _, err = s.Commit(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if n, e := s.EraseSource(ctx, memory.SourceErasure{Namespace: "local", Kind: "note", Key: "restricted", ThroughRevision: 1}); e != nil || n != 1 {
		t.Fatalf("erase: %d %v", n, e)
	}
	if rows, e := s.Scan(ctx, "local", "personal"); e != nil || len(rows) != 0 {
		t.Fatalf("query fell back: %+v %v", rows, e)
	}
	if head, e := s.Head(ctx, ref); e != nil || head != 2 {
		t.Fatalf("head regressed: %d %v", head, e)
	}
	if _, e := s.Read(ctx, ref, 1); e != nil {
		t.Fatalf("unrelated historical revision erased: %v", e)
	}
	stale := memory.Deletion{OperationID: "stale-delete", Subject: "alice", SemanticSHA256: strings.Repeat("b", 64), Ref: ref, Expected: 1}
	if _, e := s.Delete(ctx, stale); e != memory.Conflict {
		t.Fatalf("stale delete used remaining body as head: %v", e)
	}
	staleWrite := sourceRevision(ref, 2, "independent")
	staleWrite.OperationID = "stale-write"
	if _, e := s.Commit(ctx, staleWrite); e != memory.Conflict {
		t.Fatalf("revision reused: %v", e)
	}
	next := sourceRevision(ref, 3, "independent")
	receipt, e := s.Commit(ctx, next)
	if e != nil || receipt.Position != 4 {
		t.Fatalf("event position reused: %+v %v", receipt, e)
	}
	binding := memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "exact", ConfigSHA256: strings.Repeat("c", 64)}
	if _, e = s.BindConsumer(ctx, binding); e != nil {
		t.Fatal(e)
	}
	for position := uint64(1); position <= 4; position++ {
		if e = s.AckEvent(ctx, binding, position-1, position); e != nil {
			t.Fatalf("ack %d: %v", position, e)
		}
	}
}

func TestSourceErasureAndCorrectionSerializeAtTheWriteBoundary(t *testing.T) {
	for iteration := 0; iteration < 4; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
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
			ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "race"}
			if _, err = first.Commit(ctx, sourceRevision(ref, 1, "restricted")); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			writes := make(chan error, 1)
			erases := make(chan error, 1)
			go func() { <-start; _, e := first.Commit(ctx, sourceRevision(ref, 2, "restricted")); writes <- e }()
			go func() {
				<-start
				_, e := second.EraseSource(ctx, memory.SourceErasure{Namespace: "local", Kind: "note", Key: "restricted", ThroughRevision: 1})
				erases <- e
			}()
			close(start)
			if e := <-writes; e != nil && e != memory.Denied {
				t.Fatalf("competing correction: %v", e)
			}
			if e := <-erases; e != nil {
				t.Fatal(e)
			}
			for revision := uint64(1); revision <= 2; revision++ {
				if _, e := first.Read(ctx, ref, revision); e != memory.Missing {
					t.Fatalf("restricted revision survived: %d %v", revision, e)
				}
			}
			if rows, e := first.Scan(ctx, "local", "personal"); e != nil || len(rows) != 0 {
				t.Fatalf("restricted query: %+v %v", rows, e)
			}
		})
	}
}
