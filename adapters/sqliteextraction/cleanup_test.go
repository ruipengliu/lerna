package sqliteextraction_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/sqliteextraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"testing"
)

func TestCleanupRequestRemainsBoundToRetiredCandidateAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Commit(ctx, processRecord(t)); err != nil {
		t.Fatal(err)
	}
	ref := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "original"}
	if err = s.ReserveSave(ctx, "local", "alice", "original", &wire.MemoryWrite{OperationId: "save-original", Ref: ref, Spec: &wire.MemorySpec{About: "alice"}}); err != nil {
		t.Fatal(err)
	}
	request := memory.DeleteRequest{OperationID: "delete-original", Ref: ref, ExpectedRevision: 1, Purpose: "assist"}
	if err = s.ReserveCleanup(ctx, "local", "alice", "original", request); err != memory.Denied {
		t.Fatalf("active candidate cleaned: %v", err)
	}
	if err = s.Retire(ctx, "local", "alice", "original"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveCleanup(ctx, "local", "alice", "original", request); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LookupCleanup(ctx, "local", "alice", "original")
	if err != nil || got.OperationID != request.OperationID || got.ExpectedRevision != request.ExpectedRevision || got.Purpose != request.Purpose || !proto.Equal(got.Ref, request.Ref) {
		t.Fatalf("lost request: %+v %v", got, err)
	}
	if err = s.ReserveCleanup(ctx, "local", "alice", "original", request); err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.OperationID = "replacement"
	if err = s.ReserveCleanup(ctx, "local", "alice", "original", changed); err != memory.IdentityConflict {
		t.Fatalf("replacement deletion: %v", err)
	}
	changed = request
	changed.ExpectedRevision = 2
	if err = s.ReserveCleanup(ctx, "local", "alice", "original", changed); err != memory.IdentityConflict {
		t.Fatalf("changed revision: %v", err)
	}
	if _, err = s.LookupCleanup(ctx, "local", "mallory", "original"); err != memory.Denied {
		t.Fatalf("wrong subject: %v", err)
	}
}
