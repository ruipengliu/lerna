package memory_test

import (
	"context"
	"os"
	"testing"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPostgresContentMemoryAndContinuousCommittedIndex(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN is required for PostgreSQL evidence")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, dsn, postgres.WithMaxConnections(8))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := newFixtureWithStore(t, store)
	id := api.NewID("memory")
	values := f.values(t, "PostgreSQL 保存的准确原断言")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	one := uint64(1)
	values.ContentRef = f.upload(t, "PostgreSQL 保存的纠正断言")
	r = f.command(t, "memory.replace", id, &one, memory.ReplaceInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("replace: %+v", r)
	}
	if err = runtime.Drain(ctx, store, f.scope, f.dispatcher.Registry, 100); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.IndexStatus(ctx, f.scope, f.auth)
	if err != nil {
		t.Fatal(err)
	}
	if status.ChangeHead != 2 || status.ContiguousWatermark != 2 || status.State != "ready" {
		t.Fatalf("committed index head: %+v", status)
	}
	record, err := f.service.ReadMemory(ctx, f.scope, f.auth, memory.ReadMemoryInput{MemoryID: id})
	if err != nil || record.Revision != 2 || record.Values.ContentRef != values.ContentRef {
		t.Fatalf("read correction: %+v %v", record, err)
	}
	two := uint64(2)
	r = f.command(t, "memory.delete", id, &two, memory.DeleteInput{MemoryID: id, Reason: "PG 删除墓碑"})
	if r.Stage != "applied" {
		t.Fatalf("delete: %+v", r)
	}
	if err = runtime.Drain(ctx, store, f.scope, f.dispatcher.Registry, 100); err != nil {
		t.Fatal(err)
	}
	record, err = f.service.InspectMemory(ctx, f.scope, f.auth, id)
	if err != nil || record.State != "deleted" || record.CleanupState != "complete" {
		t.Fatalf("inspect delete: %+v %v", record, err)
	}
}

func TestPostgresFrozenQueryPageChecksCurrentSourceRetention(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN is required for PostgreSQL evidence")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, dsn, postgres.WithMaxConnections(8))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	createFixture := func(t *testing.T) fixture { return newFixtureWithStore(t, store) }
	t.Run("source_retention", func(t *testing.T) {
		testFrozenQueryPageSourceRetention(t, createFixture)
	})
	t.Run("budget_and_underlying_errors", func(t *testing.T) {
		testFrozenInputRecheckErrors(t, createFixture)
	})
	t.Run("maximum_candidates_and_long_explanations", func(t *testing.T) {
		testFrozenQueryPaginatesMaximumCandidatesWithLongExplanations(t, createFixture)
	})
	t.Run("public_json_bound", func(t *testing.T) {
		testFrozenLiteralQueryKeepsPublicPagesWithinJSONBound(t, createFixture)
	})
	t.Run("exact_snapshot_piece_digest", func(t *testing.T) {
		testFrozenQueryRejectsSnapshotPartDigestChanges(t, createFixture)
	})
	t.Run("later_permission_gap_bound", func(t *testing.T) {
		testFrozenQueryReservesRoomForLaterPermissionGaps(t, createFixture)
	})
}
