//go:build integration

package recovery_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
)

func sqliteDatabase(t *testing.T) *ownedFixture { return newOwnedFixture(t, "sqlite", true) }
func TestSQLiteSharedAdmissionBehaviors(t *testing.T) {
	runAdmissionBehaviors(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func TestSQLiteEveryTransactionChecksEffectiveDurability(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	ctx := contextFor(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			settings, err := store.Settings(ctx, tx)
			if err != nil {
				return err
			}
			if settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.ForeignKeys != 1 || settings.BusyTimeout != 100 || settings.SQLiteVersion == "" {
				t.Fatalf("incorrect connection durability: %+v", settings)
			}
			version, number, source := sqlite3.Version()
			if settings.SQLiteVersion != version {
				t.Fatalf("runtime/library version mismatch: %+v %s %d", settings, version, number)
			}
			t.Logf("effective settings %+v; sqlite source %s version number %d", settings, source, number)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Replace forces a fresh actual product connection to the same owned file.
		fixture.Replace(t)
		store = fixture.SQLite()
	}
	if _, err := store.Settings(ctx, nil); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("nil transaction accepted: %v", err)
	}
}

// The helper is a real independent OS process, sharing only the database path
// and the explicit fake command corpus. No private SQL is used for its verdict.
func TestSQLiteWriterProcess(t *testing.T) {
	path := os.Getenv("LERNA_SQLITE_PROCESS_PATH")
	if path == "" {
		return
	}
	store, err := sqlite.Open(contextFor(t), sqlite.Config{Path: path, TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond})
	if os.Getenv("LERNA_SQLITE_PROCESS_ACTION") == "excluded" {
		if !errors.Is(err, sqlite.ErrWriterActive) {
			t.Fatalf("second process writer not excluded: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	out, err := hostFor(store, owner, principal).Record(contextFor(t), []byte(os.Getenv("LERNA_SQLITE_PROCESS_COMMAND")), &principal)
	assertReceived(t, out, err)
}
func sqliteProcess(t *testing.T, path, action string, command []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteWriterProcess$", "-test.v")
	child.Env = append(os.Environ(), "LERNA_SQLITE_PROCESS_PATH="+path, "LERNA_SQLITE_PROCESS_ACTION="+action, "LERNA_SQLITE_PROCESS_COMMAND="+string(command))
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("real writer process %s failed: %v\n%s", action, err, output)
	}
}
func TestSQLiteWriterExclusionAndNormalProcessReopen(t *testing.T) {
	fixture := sqliteDatabase(t)
	path := fixture.sq.Path
	original := command("process-original", "process-input", "written by independent process", nil, future())
	sqliteProcess(t, path, "excluded", original)
	if err := fixture.CloseWriter(); err != nil {
		t.Fatal(err)
	}
	sqliteProcess(t, path, "write", original)
	replacement := fixture.Replace(t)
	h := hostFor(replacement, owner, principal)
	before, err := h.Observe(contextFor(t), "process-input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	queried, err := contract.GetCommand(contextFor(t), readWire(contract.CommandRef{Owner: owner, CommandID: "process-original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("closed process lost original receipt")
	}
	out, err := h.Record(contextFor(t), original, &principal)
	assertReceiptSame(t, found.Receipt, assertReceived(t, out, err))
	after, err := h.Observe(contextFor(t), "process-input", &principal)
	if err != nil || after.Input.Text != "written by independent process" || after.Input.Revision != 1 || after.Job.ID != before.Job.ID || after.Job.WorkRevision != 1 || after.Job.CompletedRevision != 0 || after.Job.State != "ready" {
		t.Fatalf("process replacement changed durable responsibility: %+v %v", after, err)
	}
}

func TestSQLiteMigrationRecordsExactVersionAndRejectsAlteredChecksum(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	status, err := store.MigrationStatus(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != 5 || status.Checksum != sqlite.MigrationV5Checksum() {
		t.Fatalf("missing current migration record: %+v", status)
	}
	// Deliberate corruption is a storage fault fixture, not a business observation.
	cfg := fixture.sq
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(contextFor(t), "UPDATE schema_migrations SET checksum='corrupt' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(contextFor(t)); err == nil {
		t.Fatal("altered immutable migration was accepted")
	}
}

func TestSQLiteUncoordinatedSQLLockProcess(t *testing.T) {
	path := os.Getenv("LERNA_SQLITE_LOCK_PATH")
	if path == "" {
		return
	}
	ctx := contextFor(t)
	// An intentionally uncoordinated raw SQL process injects a real SQLite write
	// lock. Product Host writers are excluded earlier by the lifetime flock.
	db, err := sql.Open("sqlite3", path+"?_txlock=immediate&_busy_timeout=100")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	fmt.Fprintln(os.Stdout, "SQL_WRITE_LOCK_HELD")
	released := make(chan struct{})
	go func() { bufio.NewReader(os.Stdin).ReadString('\n'); close(released) }()
	select {
	case <-released:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func holdSQLiteProcessLock(t *testing.T, path string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteUncoordinatedSQLLockProcess$", "-test.v")
	child.Env = append(os.Environ(), "LERNA_SQLITE_LOCK_PATH="+path)
	input, err := child.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err = child.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if scanner.Text() == "SQL_WRITE_LOCK_HELD" {
				ready <- struct{}{}
			}
		}
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			io.WriteString(input, "release\n")
			input.Close()
			<-scanned
			err := child.Wait()
			cancel()
			if err != nil {
				t.Errorf("SQL-lock process failed: %v %s", err, stderr.String())
			}
		})
	}
	t.Cleanup(release)
	select {
	case <-ready:
	case <-ctx.Done():
		release()
		t.Fatal("SQL-lock process did not establish bounded lock")
	}
	return release
}
func TestSQLiteBusyDeadlineRollsBackAndNormalControlStillCommits(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	cfg := fixture.sq
	h := hostFor(store, owner, principal)
	release := holdSQLiteProcessLock(t, cfg.Path)
	original := command("busy-original", "busy-input", "normal after real lock", nil, future())
	start := time.Now()
	_, err := h.Record(contextFor(t), original, &principal)
	assertReason(t, err, "dependency_unavailable")
	var lockError sqlite3.Error
	if !errors.As(err, &lockError) || lockError.Code != sqlite3.ErrBusy {
		t.Fatalf("fault was not real SQLite busy: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("busy deadline exceeded: %v", elapsed)
	}
	release()
	queried, err := contract.GetCommand(contextFor(t), readWire(contract.CommandRef{Owner: owner, CommandID: "busy-original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := queried.AsNotFound(); !ok {
		t.Fatal("busy failure persisted a partial receipt")
	}
	if _, err = h.Observe(contextFor(t), "busy-input", &principal); err == nil {
		t.Fatal("busy failure persisted input/Job")
	}
	out, err := h.Record(contextFor(t), original, &principal)
	assertReceived(t, out, err)
	got, err := h.Observe(contextFor(t), "busy-input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.WorkRevision != 1 {
		t.Fatalf("normal control failed: %+v %v", got, err)
	}
}
func TestSQLiteQueuedCancellationAndActiveTransactionRollback(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	held := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("writer coordinator control timed out")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	original := command("queue-canceled", "cancel-input", "normal after cancel", nil, future())
	start := time.Now()
	_, err := h.Record(canceled, original, &principal)
	assertReason(t, err, "dependency_unavailable")
	if time.Since(start) > time.Second {
		t.Fatal("canceled writer wait was unbounded")
	}
	queued, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	_, err = h.Record(queued, original, &principal)
	stop()
	assertReason(t, err, "dependency_unavailable")
	close(release)
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	active, abort := context.WithCancel(ctx)
	ref := contract.CommandRef{Owner: owner, CommandID: "active-canceled"}
	err = store.Within(active, owner, func(txctx context.Context, tx runtime.Tx) error {
		now, err := store.Now(txctx, tx)
		if err != nil {
			return err
		}
		if err = store.SaveInput(txctx, tx, owner, demo.Input{ID: "active-input", Revision: 1, Text: "must roll back", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		object := contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "active-input"}
		if _, err = store.Trigger(txctx, tx, object, "project", 1, now); err != nil {
			return err
		}
		if err = store.SaveCommand(txctx, tx, ref, runtime.CommandRecord{Digest: "fault-fixture", Receipt: contract.NewCommandReceiptApplied(contract.CommandReceiptApplied{CommandRef: ref, ObjectRef: object, Revision: "1"})}); err != nil {
			return err
		}
		abort()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("active cancel did not roll back: %v", err)
	}
	for _, ref := range []contract.CommandRef{{Owner: owner, CommandID: "queue-canceled"}, ref} {
		result, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result.AsNotFound(); !ok {
			t.Fatal("canceled admission retained receipt")
		}
	}
	if _, err = h.Observe(ctx, "active-input", &principal); err == nil {
		t.Fatal("active cancel retained input/Job")
	}
	out, err := h.Record(ctx, original, &principal)
	assertReceived(t, out, err)
	out, err = h.Record(ctx, command("active-canceled", "active-input", "normal active control", nil, future()), &principal)
	assertReceived(t, out, err)
}

func TestSQLiteCloseCancelsAndWaitsForEntireOwnerTransaction(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	cfg := fixture.sq
	ctx := contextFor(t)
	staged := make(chan struct{})
	unwinding := make(chan struct{})
	permitExit := make(chan struct{})
	finished := make(chan error, 1)
	var permitOnce sync.Once
	release := func() { permitOnce.Do(func() { close(permitExit) }) }
	t.Cleanup(release)
	go func() {
		finished <- store.Within(ctx, owner, func(txctx context.Context, tx runtime.Tx) error {
			now, err := store.Now(txctx, tx)
			if err != nil {
				return err
			}
			if err = store.SaveInput(txctx, tx, owner, demo.Input{ID: "closing-input", Revision: 1, Text: "must roll back", CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			if _, err = store.Trigger(txctx, tx, contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "closing-input"}, "project", 1, now); err != nil {
				return err
			}
			close(staged)
			select {
			case <-txctx.Done():
				close(unwinding)
			case <-permitExit:
				return nil
			}
			select {
			case <-permitExit:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-staged:
	case <-ctx.Done():
		t.Fatal("close fixture failed to stage real transaction")
	}
	closed := make(chan error, 1)
	go func() { closed <- store.Close() }()
	select {
	case <-unwinding:
	case err := <-closed:
		release()
		<-finished
		t.Fatalf("Close released ownership while original Tx was active: %v", err)
	case <-ctx.Done():
		release()
		t.Fatal("Close failed to cancel active owner transaction")
	}
	second, err := sqlite.Open(ctx, cfg)
	if second != nil {
		second.Close()
	}
	if !errors.Is(err, sqlite.ErrWriterActive) {
		release()
		t.Fatalf("replacement writer entered during original Tx unwind: %v", err)
	}
	release()
	if err = <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("closing owner transaction did not cancel: %v", err)
	}
	select {
	case err = <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Close did not finish after Tx exited")
	}
	replacement := fixture.Replace(t)
	h := hostFor(replacement, owner, principal)
	if _, err = h.Observe(ctx, "closing-input", &principal); err == nil {
		t.Fatal("closing transaction committed partial facts")
	}
	out, err := h.Record(ctx, command("closing-control", "closing-input", "normal replacement", nil, future()), &principal)
	assertReceived(t, out, err)
}

func TestSQLiteCloseDeadlineRetainsOwnershipUntilCallbackExits(t *testing.T) {
	fixture := newOwnedFixture(t, "sqlite", false)
	fixture.sq.TransactionTimeout = 100 * time.Millisecond
	fixture.sq.BusyTimeout = 20 * time.Millisecond
	cfg := fixture.sq
	if _, err := fixture.Open(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	store := fixture.SQLite()
	var err error
	entered := make(chan struct{})
	permit := make(chan struct{})
	finished := make(chan error, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(permit) }) }
	t.Cleanup(release)
	// A deliberately invalid callback ignores its cancellation until explicitly
	// released. Close must fail in a bounded time and retain the writer exclusion.
	ctx := contextFor(t)
	go func() {
		finished <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error { close(entered); <-permit; return nil })
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		release()
		t.Fatal("close deadline fixture failed")
	}
	start := time.Now()
	if err = fixture.CloseWriter(); !errors.Is(err, sqlite.ErrCloseTimeout) {
		release()
		t.Fatalf("bad callback was not bounded: %v", err)
	}
	if time.Since(start) > time.Second {
		release()
		t.Fatal("Close wait exceeded finite bound")
	}
	second, err := sqlite.Open(contextFor(t), cfg)
	if second != nil {
		second.Close()
	}
	if !errors.Is(err, sqlite.ErrWriterActive) {
		release()
		t.Fatalf("timed-out Close released original ownership: %v", err)
	}
	release()
	if err = <-finished; !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("invalid callback committed after shutdown: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.Replace(t)
	if err = fixture.SQLite().Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
}
func TestSQLiteDeviceClockPreservesUTCNanosecondInstants(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	clock := &controlledClock{admissionStore: store, instant: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	h := durablework.New(owner, store, store, store, clock, durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true}}))
	fixturePool(h)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("time-zero", "time-input", "zero fraction", nil, "2026-10-03T00:00:00.000001Z"), &principal)
	assertReceived(t, out, err)
	got, err := h.Observe(ctx, "time-input", &principal)
	if err != nil || !got.Input.CreatedAt.Equal(clock.instant) || !got.Input.UpdatedAt.Equal(clock.instant) || !got.Job.DueAt.Equal(clock.instant) {
		t.Fatalf("whole second changed: %+v %v", got, err)
	}
	clock.instant = clock.instant.Add(100*time.Millisecond + time.Nanosecond)
	one := contract.Revision("1")
	out, err = h.Record(ctx, command("time-fraction", "time-input", "fraction", &one, "2026-10-03T00:00:01.000000Z"), &principal)
	assertReceived(t, out, err)
	got, err = h.Observe(ctx, "time-input", &principal)
	if err != nil || !got.Input.UpdatedAt.Equal(clock.instant) || !got.Job.DueAt.Equal(clock.instant) || got.Input.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("accurate fractional instant changed: %+v %v", got, err)
	}
}

func TestSQLiteHistoricalV1FileRestoresOriginalDecisionsAndPendingJob(t *testing.T) {
	fixture := filepath.Join("..", "fixtures", "durable-work", "sqlite-v1")
	data, err := os.ReadFile(filepath.Join(fixture, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	scopeFixture := newOwnedFixture(t, "sqlite", false)
	path := scopeFixture.sq.Path
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reportData, err := os.ReadFile(filepath.Join(fixture, "writer-observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Observation demo.Observation
		Outcomes    []contract.TransportOutcome
		Migration   sqlite.Migration
	}
	if err = json.Unmarshal(reportData, &report); err != nil {
		t.Fatal(err)
	}
	if _, err = scopeFixture.Open(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	store := scopeFixture.SQLite()
	if err = store.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	status, err := store.MigrationStatus(contextFor(t))
	if err != nil || status.Version != 5 || status.Checksum != sqlite.MigrationV5Checksum() {
		t.Fatalf("restored migration changed: %+v %v", status, err)
	}
	versions, err := store.MigrationVersions(contextFor(t))
	if err != nil || len(versions) != 5 || versions[0] != report.Migration || versions[1].Version != 2 || versions[1].Checksum != "sha256:3791b3fc5ca49c18eee04b2afcaa54c2aae9c5afbf3f1e3fd98f5cce8715e01c" || versions[2].Version != 3 || versions[2].Checksum != sqlite.MigrationV3Checksum() || versions[3].Version != 4 || versions[3].Checksum != sqlite.MigrationV4Checksum() || versions[4] != status {
		t.Fatalf("actual historical v1/v2 identity: %+v %v", versions, err)
	}
	scope := contract.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-owner"}
	subject := contract.SubjectBinding{TenantID: scope.TenantID, SubjectID: "fixture-writer", DelegationChain: []contract.DelegatedSubject{}}
	h := hostFor(store, scope, subject)
	ctx := contextFor(t)
	before, err := h.Observe(ctx, "v1-input", &subject)
	if err != nil || before.Input.Text != "PG v1 portable input 🌍" || before.Input.Revision != 1 || before.Job.ID != report.Observation.Job.ID || before.Job.WorkRevision != 1 || before.Job.CompletedRevision != 0 || before.Job.State != "ready" {
		t.Fatalf("historical file lost pending original: %+v %v", before, err)
	}
	commandsData, err := os.ReadFile(filepath.Join(fixture, "commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var commands []json.RawMessage
	if err = json.Unmarshal(commandsData, &commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || len(report.Outcomes) != 3 {
		t.Fatal("historical corpus/observation incomplete")
	}
	for i, data := range commands {
		original, ok := report.Outcomes[i].AsReceived()
		if !ok {
			t.Fatal("historical writer outcome unconfirmed")
		}
		id := contract.ID("applied-original")
		if i == 1 {
			id = "expired-original"
		}
		result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: scope, CommandID: id}), &subject, h.Permissions, h, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := result.AsFound()
		if !ok {
			t.Fatal("historical receipt unavailable")
		}
		assertReceiptSame(t, original.Receipt, found.Receipt)
		out, err := h.Record(ctx, data, &subject)
		assertReceiptSame(t, original.Receipt, assertReceived(t, out, err))
	}
	if _, err = h.Observe(ctx, "expired-input", &subject); err == nil {
		t.Fatal("historical expired command acquired input/Job")
	}
	after, err := h.Observe(ctx, "v1-input", &subject)
	if err != nil || after.Job.ID != before.Job.ID || after.Input.Revision != 1 || after.Job.WorkRevision != 1 {
		t.Fatalf("historical retransmission changed responsibility: %+v %v", after, err)
	}

	worker := conformanceWorker(t, scope, store, store, store, store)
	batch, err := worker.Claim(ctx, "historical-v2", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.JobID != report.Observation.Job.ID || batch[0].Claim.Epoch != 1 || batch[0].Input.Text != "PG v1 portable input 🌍" {
		t.Fatalf("historical v2 claim: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	projected, err := h.Observe(ctx, "v1-input", &subject)
	if err != nil || projected.Job.ID != report.Observation.Job.ID || projected.Job.CompletedRevision != 1 || projected.Job.State != "done" || projected.Projection == nil || projected.Projection.InputRevision != 1 || projected.Projection.TextDigest != "sha256:eeebf3ebdb81d9e669ca989199225a42df8bfe152a9e247c1b5981052f615bbc" {
		t.Fatalf("historical v2 projection: %+v %v", projected, err)
	}
}

func TestSQLiteWorkBusyAndCanceledQueueRetainNormalResponsibility(t *testing.T) {
	fixture := sqliteDatabase(t)
	store := fixture.SQLite()
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	worker := conformanceWorker(t, owner, store, store, store, store)
	cfg := fixture.sq
	releaseBusy := holdSQLiteProcessLock(t, cfg.Path)
	_, err = worker.Claim(ctx, "busy", 1, time.Minute)
	var lockError sqlite3.Error
	if !errors.As(err, &lockError) || lockError.Code != sqlite3.ErrBusy {
		t.Fatalf("claim did not report actual busy: %v", err)
	}
	releaseBusy()
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	awaitStage(t, ctx, held)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = worker.Claim(canceled, "canceled", 1, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued claim cancellation: %v", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.State != "ready" || got.Job.CompletedRevision != 0 {
		t.Fatalf("busy/cancel lost responsibility: %+v %v", got, err)
	}
	batch, err := worker.Claim(ctx, "normal", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.Epoch != 1 {
		t.Fatalf("normal claim: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}
