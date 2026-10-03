package contract_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type testRecord struct {
	Value string `json:"value"`
}

func sqliteStore(t *testing.T) (*sqlite.Store, runtime.Scope) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "owner.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: s.ID()}
}

func TestSQLiteRecordsKeepOriginalVersionsAndRollback(t *testing.T) {
	s, scope := sqliteStore(t)
	ctx := context.Background()
	id := api.NewID("task")
	status, err := s.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
		return tx.Create(ctx, "task.tasks", id, "", testRecord{Value: "original"})
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("create: %s %v", status, err)
	}
	status, err = s.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
		return tx.Put(ctx, "task.tasks", id, 1, testRecord{Value: "current"})
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("put: %s %v", status, err)
	}
	var original, current testRecord
	if rev, err := s.Read(ctx, scope, "task.tasks", id, 1, &original); err != nil || rev != 1 || original.Value != "original" {
		t.Fatalf("original: %d %+v %v", rev, original, err)
	}
	if rev, err := s.Read(ctx, scope, "task.tasks", id, 0, &current); err != nil || rev != 2 || current.Value != "current" {
		t.Fatalf("current: %d %+v %v", rev, current, err)
	}
	stop := errors.New("discard")
	status, err = s.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
		if err := tx.Put(ctx, "task.tasks", id, 2, testRecord{Value: "discarded"}); err != nil {
			return err
		}
		return stop
	})
	if status != runtime.RolledBack || !errors.Is(err, stop) {
		t.Fatalf("rollback: %s %v", status, err)
	}
	if rev, err := s.Read(ctx, scope, "task.tasks", id, 0, &current); err != nil || rev != 2 || current.Value != "current" {
		t.Fatalf("rollback changed current: %d %+v %v", rev, current, err)
	}
}
