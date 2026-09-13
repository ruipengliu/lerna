package sqliteextraction_test

import (
	"context"
	"lerna/adapters/sqliteextraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRetiredSaveDiscoveryIsBoundedAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "candidates.db")
	s, err := sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c", "a", "b", "active"} {
		r := processRecord(t)
		r.OperationID = id
		if err = s.Commit(ctx, r); err != nil {
			t.Fatal(err)
		}
		if err = s.ReserveSave(ctx, "local", "alice", id, &wire.MemoryWrite{OperationId: "save-" + id, Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: id}, Spec: &wire.MemorySpec{About: "alice"}}); err != nil {
			t.Fatal(err)
		}
		if id != "active" {
			if err = s.Retire(ctx, "local", "alice", id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqliteextraction.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.ListRetiredSaves(ctx, "local", "alice", "", 2)
	if err != nil || !reflect.DeepEqual(first, []string{"a", "b"}) {
		t.Fatalf("first page: %v %v", first, err)
	}
	last, err := s.ListRetiredSaves(ctx, "local", "alice", "b", 2)
	if err != nil || !reflect.DeepEqual(last, []string{"c"}) {
		t.Fatalf("last page: %v %v", last, err)
	}
	for _, scope := range [][2]string{{"other", "alice"}, {"local", "mallory"}} {
		ids, e := s.ListRetiredSaves(ctx, scope[0], scope[1], "", 2)
		if e != nil || len(ids) != 0 {
			t.Fatalf("scope: %v %v", ids, e)
		}
	}
	if _, err = s.ListRetiredSaves(ctx, "local", "alice", "", 17); err != memory.Invalid {
		t.Fatalf("unbounded page: %v", err)
	}
}
