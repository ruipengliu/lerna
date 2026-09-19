package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/memory"
)

func TestRecoverySupportsHistoryBeyondRetainedBodyCapacity(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitememory.Open(filepath.Join(t.TempDir(), "current.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	ref := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection, Key: "old"}
	for revision := uint64(1); revision <= 512; revision++ {
		_, err = store.Commit(ctx, memory.Change{OperationID: fmt.Sprintf("put-%d", revision), Subject: "operator", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Expected: revision - 1, Record: memory.Revision{Ref: ref, Revision: revision, Document: []byte(`{"preference":"synthetic"}`)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = store.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 512})
	if err != nil {
		t.Fatal(err)
	}
	kept := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection, Key: "kept"}
	_, err = store.Commit(ctx, memory.Change{OperationID: "put-kept", Subject: "operator", SemanticSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Record: memory.Revision{Ref: kept, Revision: 1, Document: []byte(`{"preference":"kept"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.RecoverySnapshot(ctx, scope, 0)
	if err != nil {
		t.Fatalf("valid accumulated history prevented recovery: %v", err)
	}
	if state.Position != 514 || len(state.Records) != 1 || len(state.Deleted) != 1 {
		t.Fatal("current retained state lost")
	}
	if state.After != 0 || len(state.Operations) != 512 || state.Operations[511].OperationID != "put-512" || state.Operations[511].SemanticSHA256 != "" {
		t.Fatal("first history page did not preserve erased original identities")
	}
	next, err := store.RecoverySnapshot(ctx, scope, 512)
	if err != nil {
		t.Fatal(err)
	}
	if next.After != 512 || next.Position != 514 || len(next.Operations) != 2 || next.Operations[0].OperationID != "delete" || next.Operations[0].Position != 513 || next.Operations[1].OperationID != "put-kept" || next.Operations[1].Position != 514 {
		t.Fatal("remaining operations disappeared across the page boundary")
	}
	end, err := store.RecoverySnapshot(ctx, scope, 514)
	if err != nil || len(end.Operations) != 0 || end.Position != 514 || len(end.Records) != 1 || len(end.Deleted) != 1 {
		t.Fatalf("head-only current state: %+v %v", end, err)
	}
	if _, err = store.RecoverySnapshot(ctx, scope, 515); err != memory.Conflict {
		t.Fatalf("future cursor accepted: %v", err)
	}
}
