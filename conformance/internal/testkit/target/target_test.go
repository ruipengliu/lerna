package target_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/mattn/go-sqlite3"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/conformance/internal/testkit/target"
)

func TestWriteReadAndIndependentObserverSurviveReopen(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	receipt, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte{0, 255, 10, 13}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Value.Version != 1 || string(receipt.Value.Data) != string([]byte{0, 255, 10, 13}) {
		t.Fatalf("receipt: %+v", receipt)
	}
	assertValue := func() {
		t.Helper()
		value, err := f.writer.Read(f.ctx, "fake-document")
		if err != nil || value.Version != 1 || string(value.Data) != string([]byte{0, 255, 10, 13}) {
			t.Fatalf("read: %+v %v", value, err)
		}
		observer, err := f.observer()
		if err != nil {
			t.Fatal(err)
		}
		fact, err := observer.Observe(f.ctx, "original-1")
		if err != nil || fact.Identity != "test-target-04" || fact.Value.Version != 1 || string(fact.Value.Data) != string([]byte{0, 255, 10, 13}) || len(fact.Receives) != 1 {
			t.Fatalf("observer: %+v %v", fact, err)
		}
	}
	assertValue()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	f.open()
	assertValue()
}

type fixture struct {
	closers   []func() error
	t         *testing.T
	ctx       context.Context
	cfg       target.Config
	writer    *target.Target
	directory string
	now       time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	directory, err := os.MkdirTemp("", "lerna-target-04-")
	if err != nil {
		t.Fatal(err)
	}
	// Acknowledge this exact scope before opening any database.
	record, err := os.OpenFile(filepath.Join(directory, "owned-scope"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, err = fmt.Fprintln(record, directory)
		err = errors.Join(err, record.Sync(), record.Close())
	}
	if registry := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY"); registry != "" && err == nil {
		file, e := os.OpenFile(registry, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if e == nil {
			_, e = fmt.Fprintln(file, "sqlite-target", directory)
			e = errors.Join(e, file.Sync(), file.Close())
		}
		err = e
	}
	if err == nil {
		dir, e := os.Open(directory)
		if e == nil {
			e = errors.Join(dir.Sync(), dir.Close())
		}
		err = e
		parent, e := os.Open(filepath.Dir(directory))
		if e == nil {
			e = errors.Join(parent.Sync(), parent.Close())
		}
		err = errors.Join(err, e)
	}
	if err != nil {
		t.Fatalf("retain owned scope %s: %v", directory, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	f := &fixture{t: t, ctx: ctx, directory: directory, now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	f.cfg = target.Config{Path: filepath.Join(directory, "target.sqlite"), Identity: "test-target-04", Window: time.Minute, QueryMode: target.QueryEnabled, IOTimeout: time.Second, BusyTimeout: 10 * time.Millisecond, Now: func() time.Time { return f.now }}
	t.Logf("owned SQLite target: %s", directory)
	t.Cleanup(func() {
		cancel()
		closed := true
		for i := len(f.closers) - 1; i >= 0; i-- {
			if err := f.closers[i](); err != nil {
				t.Errorf("retain %s: reader/lock close %v", directory, err)
				closed = false
			}
		}
		if !closed {
			return
		}
		if f.writer != nil {
			if err := f.writer.Close(); err != nil {
				t.Errorf("retain %s: writer close %v", directory, err)
				return
			}
		}
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *fixture) open() *target.Target {
	f.t.Helper()
	writer, err := f.trackedOpen(f.cfg)
	f.writer = writer
	if err != nil {
		f.t.Fatal(err)
	}
	f.writer = writer
	return writer
}

func TestOriginalKeyReplayPreservesWindowAndRejectsChangedInput(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	first, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Second)
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	writer = f.open()
	replay, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Value.Version != 1 || string(replay.Value.Data) != "first" || !replay.Start.Equal(first.Start) || !replay.Deadline.Equal(first.Deadline) || replay.Digest != first.Digest {
		t.Fatalf("replay changed original: %+v / %+v", first, replay)
	}
	_, err = writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("changed")})
	if !errors.Is(err, target.ErrConflict) {
		t.Fatalf("changed input: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, "original-1")
	if err != nil {
		t.Fatal(err)
	}
	if fact.Value.Version != 1 || string(fact.Value.Data) != "first" || len(fact.Receives) != 3 || fact.Receives[0].Outcome != "applied" || fact.Receives[1].Outcome != "replayed" || fact.Receives[2].Outcome != "conflict" {
		t.Fatalf("independent target fact: %+v", fact)
	}
	next, err := writer.Write(f.ctx, target.Request{Key: "original-2", Resource: "fake-document", Data: []byte("second")})
	if err != nil {
		t.Fatal(err)
	}
	if next.Value.Version != 2 {
		t.Fatalf("new original version: %+v", next)
	}
	value, err := writer.Read(f.ctx, "fake-document")
	if err != nil || value.Version != 2 || string(value.Data) != "second" {
		t.Fatalf("new current value: %+v %v", value, err)
	}
	old, err := observer.Observe(f.ctx, "original-1")
	if err != nil || old.Value.Version != 1 || string(old.Value.Data) != "first" {
		t.Fatalf("original committed value: %+v %v", old, err)
	}
}

func TestExpiredGuaranteeDoesNotEraseOriginalProcessingFact(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	first, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	f.now = first.Deadline
	_, err = writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("first")})
	if !errors.Is(err, target.ErrGuaranteeExpired) {
		t.Fatalf("expiry must expose guarantee shortage: %v", err)
	}
	original, err := writer.Query(f.ctx, "original-1")
	if err != nil || original.Value.Version != 1 || string(original.Value.Data) != "first" || !original.Deadline.Equal(first.Deadline) {
		t.Fatalf("expiry erased original processing fact: %+v %v", original, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	writer = f.open()
	_, err = writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("changed")})
	if !errors.Is(err, target.ErrGuaranteeExpired) {
		t.Fatalf("reopen must not extend guarantee: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, "original-1")
	if err != nil || fact.Value.Version != 1 || len(fact.Receives) != 3 || fact.Receives[1].Outcome != "guarantee_expired" || fact.Receives[2].Outcome != "guarantee_expired" {
		t.Fatalf("expiry observation: %+v %v", fact, err)
	}
	// No original processing fact currently saved is not evidence that a request
	// was never received or cannot arrive/apply later.
	_, err = writer.Query(f.ctx, "not-currently-found")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("query absent original: %v", err)
	}
	_, err = writer.Read(f.ctx, "absent-resource")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("read absent resource: %v", err)
	}
}

func TestDisabledQueryStillHasIndependentPrivilegedObservation(t *testing.T) {
	f := newFixture(t)
	f.cfg.QueryMode = target.QueryDisabled
	writer := f.open()
	_, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("test-only material")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.Query(f.ctx, "original-1")
	if !errors.Is(err, target.ErrUnsupported) {
		t.Fatalf("ordinary query must expose unsupported: %v", err)
	}
	value, err := writer.Read(f.ctx, "fake-document")
	if err != nil || value.Version != 1 || string(value.Data) != "test-only material" {
		t.Fatalf("ordinary read: %+v %v", value, err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, "original-1")
	if err != nil || fact.Value.Version != 1 || len(fact.Receives) != 1 {
		t.Fatalf("privileged observation: %+v %v", fact, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	writer = f.open()
	_, err = writer.Query(f.ctx, "not-currently-found")
	if !errors.Is(err, target.ErrUnsupported) {
		t.Fatalf("reopened no-query mode: %v", err)
	}
}

func TestTargetIdentityDurabilitySettingsAndMigrationAreObserved(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	settings, err := writer.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DatabaseID == "" || settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.ForeignKeys != 1 || settings.BusyTimeout != 10 || settings.SQLiteVersion != "3.53.4" || len(settings.Migrations) != 1 || settings.Migrations[0].Version != 1 || len(settings.Migrations[0].Checksum) != 64 {
		t.Fatalf("actual target settings: %+v", settings)
	}
	t.Logf("SQLite target settings: %+v", settings)
	if _, err = f.trackedOpen(f.cfg); !errors.Is(err, target.ErrWriterActive) {
		t.Fatalf("second writer must be excluded: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	changed := f.cfg
	changed.Identity = "another-target"
	if wrong, err := f.trackedOpen(changed); err == nil {
		wrong.Close()
		t.Fatal("reopen changed identity")
	}
	changed = f.cfg
	changed.Window = 2 * time.Minute
	if wrong, err := f.trackedOpen(changed); err == nil {
		wrong.Close()
		t.Fatal("reopen changed guarantee window")
	}
	changed = f.cfg
	changed.QueryMode = target.QueryDisabled
	if wrong, err := f.trackedOpen(changed); err == nil {
		wrong.Close()
		t.Fatal("reopen changed query guarantee")
	}
	writer = f.open()
	reopened, err := writer.Settings(f.ctx)
	if err != nil || reopened.DatabaseID != settings.DatabaseID {
		t.Fatalf("durable DB identity changed: %+v %v", reopened, err)
	}
	second := newFixture(t)
	other, err := second.open().Settings(second.ctx)
	if err != nil || other.DatabaseID == settings.DatabaseID {
		t.Fatalf("isolated files need independent DB identities: %+v %v", other, err)
	}
	observer, err := f.trackedObserver(target.ObserverConfig{Path: f.cfg.Path, Identity: "another-target", IOTimeout: time.Second})
	if err == nil {
		observer.Close()
		t.Fatal("observer ignored durable identity")
	}
}

func TestBoundedNativeSQLiteContentionHasNormalCompletionControl(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	external, err := sql.Open("sqlite3", f.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	f.closers = append(f.closers, external.Close)
	external.SetMaxOpenConns(1)
	lock, err := external.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.closers = append(f.closers, lock.Close)
	if _, err = lock.ExecContext(f.ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	rollback := func() {
		t.Helper()
		if _, err := lock.ExecContext(f.ctx, "ROLLBACK"); err != nil {
			t.Fatal(err)
		}
	}
	request := target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("after-lock")}
	_, err = writer.Write(f.ctx, request)
	var native sqlite3.Error
	if !errors.As(err, &native) || native.Code != sqlite3.ErrBusy {
		rollback()
		t.Fatalf("expected preserved native busy cause: %v", err)
	}
	rollback()
	_, err = writer.Write(f.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, "original-1")
	if err != nil || len(fact.Receives) != 1 || fact.Value.Version != 1 || string(fact.Value.Data) != "after-lock" {
		t.Fatalf("normal after busy: %+v %v", fact, err)
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	_, err = writer.Write(cancelled, target.Request{Key: "original-2", Resource: "fake-document", Data: []byte("cancelled")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled I/O: %v", err)
	}
	_, err = writer.Query(f.ctx, "original-2")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("cancelled entry created fact: %v", err)
	}
}

func TestRejectsForeignDatabaseAndUnboundedConfiguration(t *testing.T) {
	f := newFixture(t)
	foreign, err := sql.Open("sqlite3", f.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = foreign.ExecContext(f.ctx, "CREATE TABLE unrelated_owner(value TEXT)")
	if err != nil {
		foreign.Close()
		t.Fatal(err)
	}
	if err = foreign.Close(); err != nil {
		t.Fatal(err)
	}
	if wrong, err := f.trackedOpen(f.cfg); err == nil {
		wrong.Close()
		t.Fatal("target adopted unrelated owner database")
	}
	second := newFixture(t)
	writer := second.open()
	_, err = writer.Write(second.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("normal")})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*target.Config){
		func(c *target.Config) { c.IOTimeout = 0 }, func(c *target.Config) { c.IOTimeout = 31 * time.Second }, func(c *target.Config) { c.Window = 0 }, func(c *target.Config) { c.Window = 25 * time.Hour }, func(c *target.Config) { c.QueryMode = "unknown" }, func(c *target.Config) { c.Now = nil }, func(c *target.Config) { c.Path = ":memory:" }, func(c *target.Config) { c.BusyTimeout = c.IOTimeout + time.Second },
	} {
		cfg := second.cfg
		change(&cfg)
		if wrong, err := second.trackedOpen(cfg); err == nil {
			wrong.Close()
			t.Fatal("invalid config accepted")
		}
	}
	_, err = writer.Write(second.ctx, target.Request{Key: "bad", Resource: "fake-document", Data: make([]byte, 1024*1024+1)})
	if err == nil {
		t.Fatal("unbounded input accepted")
	}
	_, err = writer.Query(second.ctx, "bad")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("invalid input became fact: %v", err)
	}
}

func TestOriginalKeyBindsResourceAsWellAsExactBytes(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	_, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "a", Data: []byte{0, 'b', 0, 'c'}})
	if err != nil {
		t.Fatal(err)
	}
	// Separators inside the actual bytes/name must not turn another resource
	// and different exact bytes into the same original request meaning.
	_, err = writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "a\x00\x00b", Data: []byte{'c'}})
	if !errors.Is(err, target.ErrConflict) {
		t.Fatalf("different resource/input reused original key: %v", err)
	}
	original, err := writer.Query(f.ctx, "original-1")
	if err != nil || original.Value.Resource != "a" || string(original.Value.Data) != string([]byte{0, 'b', 0, 'c'}) {
		t.Fatalf("original processing fact: %+v %v", original, err)
	}
}

func (f *fixture) observer() (*target.Observer, error) {
	return f.trackedObserver(target.ObserverConfig{Path: f.cfg.Path, Identity: f.cfg.Identity, IOTimeout: time.Second})
}
func (f *fixture) trackedObserver(cfg target.ObserverConfig) (*target.Observer, error) {
	o, err := target.OpenObserver(f.ctx, cfg)
	if o != nil {
		f.closers = append(f.closers, o.Close)
	}
	return o, err
}
func (f *fixture) trackedOpen(cfg target.Config) (*target.Target, error) {
	writer, err := target.Open(f.ctx, cfg)
	if writer != nil {
		f.closers = append(f.closers, writer.Close)
	}
	return writer, err
}

func TestConcurrentOriginalKeyReceivesOnlyCommitOneVersion(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	results := make(chan error, 8)
	for range 8 {
		go func() {
			receipt, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("same")})
			if err == nil && (receipt.Value.Version != 1 || string(receipt.Value.Data) != "same") {
				err = fmt.Errorf("changed original receipt: %+v", receipt)
			}
			results <- err
		}()
	}
	for range 8 {
		select {
		case err := <-results:
			if err != nil {
				t.Error(err)
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, "original-1")
	if err != nil || len(fact.Receives) != 8 || fact.Value.Version != 1 {
		t.Fatalf("concurrent durable receives: %+v %v", fact, err)
	}
	applied := 0
	for _, receive := range fact.Receives {
		if receive.Outcome == "applied" {
			applied++
		} else if receive.Outcome != "replayed" {
			t.Fatalf("unexpected receive: %+v", receive)
		}
	}
	if applied != 1 {
		t.Fatalf("target recorded %d original commits", applied)
	}
}

func TestInvalidTestClockCannotCommitWrappedTime(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	valid := f.now
	for _, invalid := range []time.Time{time.Time{}, time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC), time.Unix(0, math.MaxInt64)} {
		f.now = invalid
		_, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("normal")})
		if err == nil {
			t.Fatalf("unrepresentable durable window accepted: %s", invalid)
		}
		_, err = writer.Query(f.ctx, "original-1")
		if !errors.Is(err, target.ErrNotFound) {
			t.Fatalf("invalid clock committed original: %v", err)
		}
	}
	f.now = valid
	receipt, err := writer.Write(f.ctx, target.Request{Key: "original-1", Resource: "fake-document", Data: []byte("normal")})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := writer.Query(f.ctx, "original-1")
	if err != nil || !saved.Start.Equal(receipt.Start) || !saved.Deadline.Equal(receipt.Deadline) {
		t.Fatalf("valid exact clock window: %+v %v", saved, err)
	}
}
