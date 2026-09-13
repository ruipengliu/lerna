package sqlitememory_test

import (
	"context"
	"lerna/adapters/sqlitememory"
	"lerna/memory"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverySnapshotIncludesExactErasureAndInterleavedOperations(t *testing.T) {
	ctx := context.Background()
	s, err := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ref := memory.Ref{Namespace: "local", Collection: "personal", Key: "recovery"}
	for _, in := range []memory.Change{sourceRevision(ref, 1, "restricted"), sourceRevision(ref, 2, "independent")} {
		if _, err = s.Commit(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.EraseSource(ctx, memory.SourceErasure{Namespace: "local", Kind: "note", Key: "restricted", ThroughRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(ctx, sourceRevision(ref, 3, "independent")); err != nil {
		t.Fatal(err)
	}
	scope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	snapshot, err := s.RecoverySnapshot(ctx, scope, 0)
	if err != nil || snapshot.Position != 4 || len(snapshot.Operations) != 3 || len(snapshot.Erased) != 1 || len(snapshot.SourceFences) != 1 || !memory.ValidRecoverySnapshot(snapshot) {
		t.Fatalf("complete exact state: %+v %v", snapshot, err)
	}
	if len(snapshot.Records) != 2 || snapshot.Erased[0].Revision != 1 || snapshot.Erased[0].Position != 3 || snapshot.Operations[0].SemanticSHA256 != "" {
		t.Fatal("erased state confused with retained history")
	}
	partial, err := s.RecoverySnapshot(ctx, scope, 2)
	if err != nil || len(partial.Operations) != 1 || partial.Operations[0].Position != 4 || !memory.ValidRecoverySnapshot(partial) {
		t.Fatalf("interleaved page: %+v %v", partial, err)
	}
	for _, kind := range []string{"omitted-erasure", "resurrected-body", "retained-comparison", "position-collision", "foreign-fence", "duplicate-fence"} {
		t.Run(kind, func(t *testing.T) {
			bad := snapshot
			bad.Operations = append([]memory.Receipt(nil), snapshot.Operations...)
			bad.Erased = append([]memory.SourceEvent(nil), snapshot.Erased...)
			bad.SourceFences = append([]memory.SourceErasure(nil), snapshot.SourceFences...)
			switch kind {
			case "omitted-erasure":
				bad.Erased = nil
			case "resurrected-body":
				bad.Records = append(append([]memory.RecoveryRecord(nil), snapshot.Records...), memory.RecoveryRecord{Version: memory.VersionRef{Ref: ref, Revision: 1}, SHA256: strings.Repeat("a", 64)})
			case "retained-comparison":
				bad.Operations[0].SemanticSHA256 = strings.Repeat("a", 64)
			case "position-collision":
				bad.Erased[0].Position = 4
			case "foreign-fence":
				bad.SourceFences[0].Namespace = "other"
			case "duplicate-fence":
				bad.SourceFences = append(bad.SourceFences, bad.SourceFences[0])
			}
			if memory.ValidRecoverySnapshot(bad) {
				t.Fatal("invalid proof structure accepted")
			}
		})
	}
	if _, err = s.Delete(ctx, memory.Deletion{OperationID: "delete-after-erasure", Subject: "alice", SemanticSHA256: strings.Repeat("b", 64), Ref: ref, Expected: 3}); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.RecoverySnapshot(ctx, scope, 0)
	if err != nil || deleted.Position != 5 || len(deleted.Records) != 0 || len(deleted.Deleted) != 1 || len(deleted.Erased) != 1 || !memory.ValidRecoverySnapshot(deleted) {
		t.Fatalf("full delete after exact erasure: %+v %v", deleted, err)
	}
}
