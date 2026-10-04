// Package target provides an isolated SQLite test target. Its observations are
// test facts, never Executor Effect records or supplier guarantees.
package target

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var ErrConflict = errors.New("original key content conflict")
var ErrGuaranteeExpired = errors.New("guarantee_expired")

// ErrNotFound only means no matching fact is currently saved. It does not
// authorize a replacement request or exclude an original request applying later.
var ErrNotFound = errors.New("not_found")

type QueryMode string

const QueryEnabled QueryMode = "enabled"
const QueryDisabled QueryMode = "disabled"

var ErrUnsupported = errors.New("unsupported")

type Config struct {
	Path, Identity         string
	Window                 time.Duration
	QueryMode              QueryMode
	IOTimeout, BusyTimeout time.Duration
	// Now is a test clock, called only after acquiring the writable transaction.
	Now func() time.Time
}
type Request struct {
	Key, Resource string
	Data          []byte
}
type Value struct {
	Resource string
	Version  int64
	Data     []byte
}
type Receipt struct {
	Key, Digest     string
	Start, Deadline time.Time
	Value           Value
}
type Receive struct {
	Sequence        int64
	At              time.Time
	Digest, Outcome string
}
type Fact struct {
	Pending    bool
	DatabaseID string
	Identity   string
	Receipt
	Receives []Receive
}
type Target struct {
	db             *sql.DB
	cfg            Config
	gate           chan struct{}
	release        func() error
	mu             sync.Mutex
	closed         bool
	closing        bool
	closeAttempted bool
	closeErr       error
}
type ObserverConfig struct {
	Path, Identity string
	IOTimeout      time.Duration
}
type Observer struct {
	db  *sql.DB
	cfg ObserverConfig
}

//go:embed migrations/0001_target.sql
var migration string

//go:embed migrations/0002_plans.sql
var planMigration string

// Open returns a cleanup handle with an error if initialization cannot confirm
// Close. Callers must retain that handle and physical scope until Close succeeds.
func Open(ctx context.Context, cfg Config) (*Target, error) {
	if ctx == nil || !filepath.IsAbs(cfg.Path) || cfg.Path == ":memory:" || len(cfg.Identity) == 0 || len(cfg.Identity) > 128 || cfg.Window <= 0 || cfg.Window > 24*time.Hour || (cfg.QueryMode != QueryEnabled && cfg.QueryMode != QueryDisabled) || cfg.IOTimeout <= 0 || cfg.IOTimeout > 30*time.Second || cfg.BusyTimeout < time.Millisecond || cfg.BusyTimeout > cfg.IOTimeout || cfg.Now == nil {
		return nil, errors.New("invalid finite test target configuration")
	}
	bounded, cancel := context.WithTimeout(ctx, cfg.IOTimeout)
	defer cancel()
	release, err := acquireWriter(bounded, cfg.Path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", dsn(cfg.Path, url.Values{"_journal_mode": {"WAL"}, "_synchronous": {"FULL"}, "_foreign_keys": {"on"}, "_busy_timeout": {fmt.Sprint(cfg.BusyTimeout.Milliseconds())}, "_txlock": {"immediate"}}))
	if err != nil {
		return nil, errors.Join(err, release())
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t := &Target{db: db, cfg: cfg, gate: make(chan struct{}, 1), release: release}
	if err = t.initialize(bounded); err != nil {
		closeErr := t.Close()
		if closeErr != nil {
			return t, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return t, nil
}
func dsn(path string, params url.Values) string {
	uri := url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}
	return uri.String()
}
func (t *Target) initialize(ctx context.Context) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applicationID, tableCount, userVersion int
	if err = tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount); err != nil {
		return err
	}
	const targetApplicationID = 0x4c545447

	if (applicationID == 0 && (tableCount != 0 || userVersion != 0)) || (applicationID != 0 && (applicationID != targetApplicationID || userVersion < 1 || userVersion > 2)) {
		return errors.New("foreign or unsupported target database")
	}
	for _, m := range []struct {
		version int
		sql     string
	}{{1, migration}, {2, planMigration}} {
		sum := sha256.Sum256([]byte(m.sql))
		checksum := hex.EncodeToString(sum[:])
		if userVersion < m.version {
			if _, err = tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO target_migrations(version,checksum) VALUES(?,?)", m.version, checksum); err != nil {
				return err
			}
		}
		var stored string
		if err = tx.QueryRowContext(ctx, "SELECT checksum FROM target_migrations WHERE version=?", m.version).Scan(&stored); err != nil {
			return err
		}
		if stored != checksum {
			return errors.New("test target migration checksum mismatch")
		}
	}

	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO target_identity(singleton,identity,database_id,window_ns,query_mode) VALUES(1,?,?,?,?)`, t.cfg.Identity, hex.EncodeToString(nonce[:]), int64(t.cfg.Window), string(t.cfg.QueryMode)); err != nil {
		return err
	}
	var identity, mode string
	var window int64
	if err = tx.QueryRowContext(ctx, `SELECT identity,window_ns,query_mode FROM target_identity WHERE singleton=1`).Scan(&identity, &window, &mode); err != nil {
		return err
	}
	if identity != t.cfg.Identity || window != int64(t.cfg.Window) || mode != string(t.cfg.QueryMode) {
		return errors.New("test target durable identity/configuration mismatch")
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA application_id=1280595015; PRAGMA user_version=2"); err != nil {
		return err
	}
	return tx.Commit()
}
func (t *Target) enter(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("context required")
	}
	bounded, cancel := context.WithTimeout(ctx, t.cfg.IOTimeout)
	select {
	case t.gate <- struct{}{}:
	case <-bounded.Done():
		cancel()
		return nil, nil, bounded.Err()
	}
	t.mu.Lock()
	closed := t.closed || t.closing
	t.mu.Unlock()
	if closed {
		<-t.gate
		cancel()
		return nil, nil, errors.New("test target closed")
	}
	return bounded, func() { <-t.gate; cancel() }, nil
}
func (t *Target) Close() error {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), t.cfg.IOTimeout)
	defer cancel()
	select {
	case t.gate <- struct{}{}:
		defer func() { <-t.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closeAttempted {
		return t.closeErr
	}
	t.closeAttempted = true
	if t.db != nil {
		if err := t.db.Close(); err != nil {
			t.closeErr = lifetimeCause("target native close unknown", err)
			return t.closeErr
		}
	}
	if t.release != nil {
		if err := t.release(); err != nil {
			t.closeErr = lifetimeCause("target writer release unknown", err)
			return t.closeErr
		}
	}
	t.closed = true
	return nil
}

type lifetimeError struct {
	stage string
	cause error
}

func (e *lifetimeError) Error() string { return e.stage }
func (e *lifetimeError) Unwrap() error { return e.cause }
func lifetimeCause(stage string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lifetimeError{stage: stage, cause: cause}
}

func (t *Target) Write(ctx context.Context, r Request) (Receipt, error) {
	if err := validateRequest(r); err != nil {
		return Receipt{}, err
	}
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return Receipt{}, err
	}
	defer leave()
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	out, cause := t.writeTx(ctx, tx, r, false)
	if cause != nil && !writeRejection(cause) {
		return Receipt{}, cause
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, fmt.Errorf("test target commit outcome unknown: %w", err)
	}
	if cause != nil {
		return Receipt{}, cause
	}
	return out, nil
}

func (t *Target) Read(ctx context.Context, resource string) (Value, error) {
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return Value{}, err
	}
	defer leave()
	out := Value{Resource: resource}
	err = t.db.QueryRowContext(ctx, `SELECT version,data FROM target_values WHERE resource=?`, resource).Scan(&out.Version, &out.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return Value{}, ErrNotFound
	}
	return out, err
}

// OpenObserver opens an explicitly privileged read-only connection. If failed
// initialization also fails Close, the returned handle remains the caller's
// cleanup responsibility despite the error. Do not delete its physical scope.
func OpenObserver(ctx context.Context, cfg ObserverConfig) (*Observer, error) {
	if ctx == nil || !filepath.IsAbs(cfg.Path) || cfg.Identity == "" || cfg.IOTimeout <= 0 || cfg.IOTimeout > 30*time.Second {
		return nil, errors.New("invalid finite observer configuration")
	}
	bounded, cancel := context.WithTimeout(ctx, cfg.IOTimeout)
	defer cancel()
	db, err := sql.Open("sqlite3", dsn(cfg.Path, url.Values{"mode": {"ro"}, "_query_only": {"true"}, "_foreign_keys": {"on"}, "_busy_timeout": {"10"}}))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	observer := &Observer{db: db, cfg: cfg}
	fail := func(cause error) (*Observer, error) {
		closeErr := observer.Close()
		if closeErr != nil {
			return observer, errors.Join(cause, closeErr)
		}
		return nil, cause
	}
	var identity string
	if err = db.QueryRowContext(bounded, `SELECT identity FROM target_identity WHERE singleton=1`).Scan(&identity); err != nil {
		return fail(err)
	}
	if identity != cfg.Identity {
		return fail(errors.New("observer durable identity mismatch"))
	}
	return observer, nil
}
func (o *Observer) Close() error { return o.db.Close() }
func (o *Observer) Observe(ctx context.Context, key string) (Fact, error) {
	if ctx == nil {
		return Fact{}, errors.New("context required")
	}
	ctx, cancel := context.WithTimeout(ctx, o.cfg.IOTimeout)
	defer cancel()
	tx, err := o.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Fact{}, err
	}
	defer tx.Rollback()
	fact := Fact{Identity: o.cfg.Identity}

	fact.Receipt, err = received(ctx, tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Fact{}, ErrNotFound
	}
	if err != nil {
		return Fact{}, err
	}
	fact.Pending = fact.Value.Version == 0
	if err = tx.QueryRowContext(ctx, "SELECT database_id FROM target_identity WHERE singleton=1").Scan(&fact.DatabaseID); err != nil {
		return Fact{}, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT sequence,at_ns,digest,outcome FROM target_receives WHERE original_key=? ORDER BY sequence`, key)
	if err != nil {
		return Fact{}, err
	}
	for rows.Next() {
		var receive Receive
		var at int64
		if err = rows.Scan(&receive.Sequence, &at, &receive.Digest, &receive.Outcome); err != nil {
			rows.Close()
			return Fact{}, err
		}
		receive.At = time.Unix(0, at).UTC()
		fact.Receives = append(fact.Receives, receive)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return Fact{}, err
	}
	return fact, tx.Commit()
}

// committed reads the target's saved original processing fact, independently of
// the mutable current resource value.
func committed(ctx context.Context, tx *sql.Tx, key string) (Receipt, error) {
	var out Receipt
	var start, deadline int64
	err := tx.QueryRowContext(ctx, `SELECT r.original_key,r.resource,r.digest,r.start_ns,r.deadline_ns,c.version,c.data FROM target_requests r JOIN target_commits c USING(original_key) WHERE r.original_key=?`, key).Scan(&out.Key, &out.Value.Resource, &out.Digest, &start, &deadline, &out.Value.Version, &out.Value.Data)
	if err != nil {
		return Receipt{}, err
	}
	out.Start = time.Unix(0, start).UTC()
	out.Deadline = time.Unix(0, deadline).UTC()
	return out, nil
}

func (t *Target) Query(ctx context.Context, key string) (Receipt, error) {
	if t.cfg.QueryMode == QueryDisabled {
		return Receipt{}, ErrUnsupported
	}
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return Receipt{}, err
	}
	defer leave()
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	out, err := committed(ctx, tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	return out, tx.Commit()
}

type Migration struct {
	Version  int
	Checksum string
}
type Settings struct {
	DatabaseID, JournalMode, SQLiteVersion string
	Synchronous, ForeignKeys, BusyTimeout  int
	Migrations                             []Migration
}

// Settings observes the same connection that performs writes. It is test
// configuration evidence, not a provider capability advertisement.
func (t *Target) Settings(ctx context.Context) (Settings, error) {
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer leave()
	out := Settings{}
	for _, q := range []struct {
		sql   string
		value any
	}{
		{"PRAGMA journal_mode", &out.JournalMode}, {"PRAGMA synchronous", &out.Synchronous}, {"PRAGMA foreign_keys", &out.ForeignKeys}, {"PRAGMA busy_timeout", &out.BusyTimeout}, {"SELECT sqlite_version()", &out.SQLiteVersion}, {"SELECT database_id FROM target_identity WHERE singleton=1", &out.DatabaseID},
	} {
		if err = t.db.QueryRowContext(ctx, q.sql).Scan(q.value); err != nil {
			return Settings{}, err
		}
	}
	rows, err := t.db.QueryContext(ctx, "SELECT version,checksum FROM target_migrations ORDER BY version")
	if err != nil {
		return Settings{}, err
	}
	for rows.Next() {
		var m Migration
		if err = rows.Scan(&m.Version, &m.Checksum); err != nil {
			rows.Close()
			return Settings{}, err
		}
		out.Migrations = append(out.Migrations, m)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return Settings{}, err
	}
	return out, nil
}
