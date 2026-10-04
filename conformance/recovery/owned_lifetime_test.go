//go:build integration

package recovery_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
)

func TestOwnedFixtureTwoReplacementsAndIsolatedCleanup(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			f := newOwnedFixture(t, backend, true)
			neighbor := newOwnedFixture(t, backend, true)
			ctx := contextFor(t)
			assertOwnedScopeExists(t, f, true)
			if backend == "postgres" {
				f.PGPeer(t, 0, 0)
			}
			h := hostFor(f.Store(), owner, principal)
			out, err := h.Record(ctx, command("original", "input", "hello", nil, future()), &principal)
			receipt := assertReceived(t, out, err)
			adjacent := hostFor(neighbor.Store(), owner, principal)
			out, err = adjacent.Record(ctx, command("neighbor", "input", "neighbor", nil, future()), &principal)
			assertReceived(t, out, err)
			writer := f.Store()
			worker := conformanceWorker(t, owner, writer, writer, writer, writer)
			batch, err := worker.Claim(ctx, "normal", 1, time.Minute)
			if err != nil || len(batch) != 1 {
				t.Fatalf("claim: %+v %v", batch, err)
			}
			startWork(t, worker, batch[0])
			for i := 0; i < 2; i++ {
				writer = f.Replace(t)
			}
			h = hostFor(writer, owner, principal)
			result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "original"}), &principal, h.Permissions, h, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := result.AsFound()
			if !ok {
				t.Fatal("original receipt missing")
			}
			assertReceiptSame(t, receipt, found.Receipt)
			worker = conformanceWorker(t, owner, writer, writer, writer, writer)
			if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
				t.Fatal(err)
			}
			got, err := h.Observe(ctx, "input", &principal)
			if err != nil || got.Job.ID != batch[0].Claim.JobID || got.Job.CompletedRevision != 1 || got.Projection == nil || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
				t.Fatalf("original claim projection: %+v %v", got, err)
			}
			if err = f.Cleanup(); err != nil {
				t.Fatal(err)
			}
			assertOwnedScopeExists(t, f, false)
			got, err = adjacent.Observe(ctx, "input", &principal)
			if err != nil || got.Input.Text != "neighbor" {
				t.Fatalf("neighbor scope damaged: %+v %v", got, err)
			}
		})
	}
}

func TestOwnedFixtureCancelledSetupAndOpenKeepRecoveryPossible(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := createOwnedFixture(canceled, backend, true)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("setup discarded cancellation: %v", err)
			}
			f := newOwnedFixture(t, backend, false)
			if _, err = f.Open(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("first writer discarded cancellation: %v", err)
			}
			writer, err := f.Open(contextFor(t))
			if err != nil {
				t.Fatal(err)
			}
			if err = writer.(interface{ Migrate(context.Context) error }).Migrate(contextFor(t)); err != nil {
				t.Fatal(err)
			}
			h := hostFor(writer, owner, principal)
			raw := command("original", "input", "recovered", nil, future())
			out, err := h.Record(contextFor(t), raw, &principal)
			receipt := assertReceived(t, out, err)
			if err = f.CloseWriter(); err != nil {
				t.Fatal(err)
			}
			if _, err = f.Open(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("replacement discarded cancellation: %v", err)
			}
			writer = f.Replace(t)
			h = hostFor(writer, owner, principal)
			out, err = h.Record(contextFor(t), raw, &principal)
			assertReceiptSame(t, receipt, assertReceived(t, out, err))
			if err = f.Cleanup(); err != nil {
				t.Fatal(err)
			}
			assertOwnedScopeExists(t, f, false)
		})
	}
}

func TestOwnedFixtureCleanupAfterRealHistoricalMigrationRefusal(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			var history *historicalFixture
			if backend == "postgres" {
				history = restoreHistoricalPG(t)
			} else {
				history = restoreHistoricalSQLite(t)
			}
			history.Fault(t, true)
			if err := history.Migrate(contextFor(t)); err == nil || !history.IsFault(err) {
				t.Fatalf("real historical migration fault absent: %v", err)
			}
			assertHistoricalQueries(t, historicalHost(history.Store), history)
			if err := history.Scope.Cleanup(); err != nil {
				t.Fatal(err)
			}
			assertOwnedScopeExists(t, history.Scope, false)
		})
	}
}

func TestOwnedFixtureCleanupKeepsActiveSQLiteFilesUntilConfirmedClose(t *testing.T) {
	f := newOwnedFixture(t, "sqlite", false)
	f.sq.TransactionTimeout = 100 * time.Millisecond
	if _, err := f.Open(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	store := f.Store()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	txctx := contextFor(t)
	go func() {
		done <- store.Within(txctx, owner, func(context.Context, runtime.Tx) error { close(entered); <-release; return nil })
	}()
	ctx := contextFor(t)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := f.Cleanup(); !errors.Is(err, sqlite.ErrCloseTimeout) {
		t.Fatalf("active cleanup did not retain close failure: %v", err)
	}
	assertOwnedScopeExists(t, f, true)
	if _, err := f.Open(ctx); err == nil {
		t.Fatal("cleanup admitted replacement writer")
	}
	second, err := sqlite.Open(ctx, f.sq)
	if second != nil {
		f.sqlitePeers = append(f.sqlitePeers, second)
		err = errors.Join(err, second.Close())
	}
	if !errors.Is(err, sqlite.ErrWriterActive) {
		t.Fatalf("active original writer no longer excluded peers: %v", err)
	}
	unblock()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("real callback shutdown: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = f.Cleanup(); err != nil {
		t.Fatal(err)
	}
	assertOwnedScopeExists(t, f, false)
	if err = f.Cleanup(); err != nil {
		t.Fatal("repeat cleanup:", err)
	}
}

func TestOwnedFixtureCleanupJoinsRealChildBeforeDeletingScope(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			cfg := processFixture(t, backend, "owned-child-cleanup")
			cfg.Gate = "writes_staged_before_commit"
			cfg.Raw = command("original", "input", "child", nil, "2026-10-03T02:00:00.000000Z")
			child := startHostProcess(t, cfg)
			child.event(t, cfg.Gate)
			if err := cfg.fixture.Cleanup(); err != nil {
				t.Fatal(err)
			}
			assertOwnedScopeExists(t, cfg.fixture, false)
		})
	}
}

// Namespace/path inspection is confined to this infrastructure scope. All
// original receipt, Job, Claim and projection verdicts above cross Host.
func assertOwnedScopeExists(t *testing.T, f *ownedFixture, want bool) {
	t.Helper()
	if f.backend == "sqlite" {
		_, err := os.Stat(f.directory)
		if (err == nil) != want || err != nil && !os.IsNotExist(err) {
			t.Fatalf("owned SQLite directory exists=%t wanted=%t: %v", err == nil, want, err)
		}
		return
	}
	db, err := sql.Open("pgx", f.pg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=$1)", f.pg.Schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("owned namespace exists=%t wanted=%t", exists, want)
	}
}
