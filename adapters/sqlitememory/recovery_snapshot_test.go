package sqlitememory_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"lerna/adapters/sqlitememory"
	"lerna/memory"
)

func TestRecoverySnapshotUsesCurrentDeletionAndRetainedRevisions(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitememory.Open(filepath.Join(t.TempDir(), "current.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	deleted := memory.Ref{Namespace: "local", Collection: "personal", Key: "removed"}
	retained := memory.Ref{Namespace: "local", Collection: "personal", Key: "kept"}
	for i, ref := range []memory.Ref{deleted, retained} {
		_, err = store.Commit(ctx, memory.Change{OperationID: fmt.Sprintf("put-%d", i), Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(ref.Key))), Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(fmt.Sprintf(`{"preference":%q}`, ref.Key))}})
		if err != nil {
			t.Fatal(err)
		}
	}
	unrelated := memory.Ref{Namespace: "local", Collection: "other", Key: "unrelated"}
	if _, err = store.Commit(ctx, memory.Change{OperationID: "put-other", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("other"))), Record: memory.Revision{Ref: unrelated, Revision: 1, Document: []byte(`{"preference":"other"}`)}}); err != nil {
		t.Fatal(err)
	}
	before, err := store.RecoverySnapshot(ctx, scope, 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.Position != 2 || len(before.Records) != 2 || len(before.Deleted) != 0 {
		t.Fatalf("initial authority inventory: %+v", before)
	}
	_, err = store.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("delete metadata"))), Ref: deleted, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.RecoverySnapshot(ctx, scope, 0)
	if err != nil {
		t.Fatal(err)
	}
	if current.Scope != scope || current.Position != 3 || len(current.Operations) != 3 || len(current.Records) != 1 || len(current.Deleted) != 1 {
		t.Fatalf("current authority inventory: %+v", current)
	}
	if current.Records[0].Version != (memory.VersionRef{Ref: retained, Revision: 1}) || current.Records[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(`{"preference":"kept"}`))) {
		t.Fatal("retained revision comparison lost")
	}
	if current.Deleted[0] != (memory.VersionRef{Ref: deleted, Revision: 2}) || current.Operations[0].SemanticSHA256 != "" {
		t.Fatal("deletion proof retained an old payload comparison")
	}
	if before.Position != 2 || len(before.Records) != 2 {
		t.Fatal("new read mutated previous snapshot")
	}
}

func TestRecoverySnapshotDoesNotMixConcurrentDeletionStates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "current.db")
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
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	for i := 0; i < 8; i++ {
		ref := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection, Key: fmt.Sprintf("record-%d", i)}
		_, err = a.Commit(ctx, memory.Change{OperationID: fmt.Sprintf("put-%d", i), Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("put"))), Record: memory.Revision{Ref: ref, Revision: 1, Document: []byte(`{"preference":"synthetic"}`)}})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		type result struct {
			state memory.RecoverySnapshot
			err   error
		}
		read := make(chan result, 1)
		write := make(chan error, 1)
		go func() { <-start; state, e := a.RecoverySnapshot(ctx, scope, 0); read <- result{state, e} }()
		go func() {
			<-start
			_, e := b.Delete(ctx, memory.Deletion{OperationID: fmt.Sprintf("delete-%d", i), Subject: "operator", SemanticSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("delete"))), Ref: ref, Expected: 1})
			write <- e
		}()
		close(start)
		got, writeErr := <-read, <-write
		if got.err != nil || writeErr != nil {
			t.Fatalf("snapshot/delete: %v %v", got.err, writeErr)
		}
		before := got.state.Position == uint64(2*i+1) && len(got.state.Records) == 1 && len(got.state.Deleted) == i
		after := got.state.Position == uint64(2*i+2) && len(got.state.Records) == 0 && len(got.state.Deleted) == i+1
		if !before && !after {
			t.Fatal("proof mixed old comparisons with a new deletion position")
		}
	}
}
