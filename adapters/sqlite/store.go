// Package sqlite implements file-backed internal Host storage ports.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type Config struct {
	Path                            string
	TransactionTimeout, BusyTimeout time.Duration
}
type Store struct {
	db            *sql.DB
	config        Config
	writer        chan struct{}
	closed        atomic.Bool
	closer        chan struct{}
	closeComplete bool
	closeErr      error
	shutdown      context.Context
	stop          context.CancelFunc
	releaseWriter func() error
}

var ErrWriterActive = errors.New("SQLite writable Host already active")
var ErrCloseTimeout = errors.New("SQLite owner transaction did not stop before close deadline")

func Open(ctx context.Context, cfg Config) (*Store, error) {
	return open(ctx, cfg, sql.Open, acquireWriter)
}
func open(ctx context.Context, cfg Config, openDB func(string, string) (*sql.DB, error), acquire func(context.Context, string) (func() error, error)) (*Store, error) {
	if ctx == nil || cfg.Path == "" || cfg.Path == ":memory:" || cfg.TransactionTimeout <= 0 || cfg.BusyTimeout < time.Millisecond || cfg.BusyTimeout > cfg.TransactionTimeout {
		return nil, errors.New("invalid file SQLite configuration")
	}
	path, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	release, startupErr := acquire(ctx, path)
	if release == nil {
		return nil, startupErr
	}
	shutdown, stop := context.WithCancel(context.Background())
	holder := &Store{config: cfg, writer: make(chan struct{}, 1), closer: make(chan struct{}, 1), shutdown: shutdown, stop: stop, releaseWriter: release}
	fail := func(err error) (*Store, error) {
		if closeErr := holder.Close(); closeErr != nil {
			return holder, errors.Join(&lifecycleError{stage: "startup", cause: err}, closeErr)
		}
		return nil, &lifecycleError{stage: "startup", cause: err}
	}
	if startupErr != nil {
		return fail(startupErr)
	}
	uri := url.URL{Scheme: "file", Path: path}
	params := url.Values{"_journal_mode": {"WAL"}, "_synchronous": {"FULL"}, "_foreign_keys": {"on"}, "_busy_timeout": {fmt.Sprint(cfg.BusyTimeout.Milliseconds())}, "_txlock": {"immediate"}}
	uri.RawQuery = params.Encode()
	holder.db, err = openDB("sqlite3", uri.String())
	if err != nil {
		return fail(err)
	}
	holder.db.SetMaxOpenConns(1)
	holder.db.SetMaxIdleConns(1)
	bounded, cancel := context.WithTimeout(ctx, cfg.TransactionTimeout)
	defer cancel()
	if err = holder.db.PingContext(bounded); err != nil {
		return fail(err)
	}
	return holder, nil
}

type lifecycleError struct {
	stage string
	cause error
}

func (e *lifecycleError) Error() string {
	if e.stage == "startup" {
		return "SQLite startup failed"
	}
	return "SQLite " + e.stage + " unconfirmed"
}
func (e *lifecycleError) Unwrap() error { return e.cause }

// Close cancels and drains entire owner transactions before releasing the
// process lock. A callback must obey its finite context. If it does not exit in
// time, ownership stays held; the caller can retry Close after it has exited.
func (s *Store) Close() error {
	s.closed.Store(true)
	s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), s.config.TransactionTimeout)
	defer cancel()
	select {
	case s.closer <- struct{}{}:
		defer func() { <-s.closer }()
	case <-ctx.Done():
		return ErrCloseTimeout
	}
	if s.closeComplete {
		return s.closeErr
	}
	select {
	case s.writer <- struct{}{}:
		defer func() { <-s.writer }()
	case <-ctx.Done():
		return ErrCloseTimeout
	}
	// Drain timeouts above can retry. Once native Close is actually attempted,
	// retain its outcome forever and release exclusion only after confirmed nil.
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			s.closeErr = &lifecycleError{stage: "database close", cause: err}
			s.closeComplete = true
			return s.closeErr
		}
	}
	if s.releaseWriter != nil {
		if err := s.releaseWriter(); err != nil {
			s.closeErr = &lifecycleError{stage: "writer release", cause: err}
		}
	}
	s.closeComplete = true
	return s.closeErr
}

type transaction struct {
	pool       *workpool.State
	poolLocked bool
	store      *Store
	sql        *sql.Tx
	owner      contract.OwnerRef
	active     atomic.Bool
}

func (t *transaction) Owner() contract.OwnerRef { return t.owner }
func (s *Store) token(ctx context.Context, token runtime.Tx, owner contract.OwnerRef) (*sql.Tx, error) {
	t, ok := token.(*transaction)
	if !ok || t == nil || t.store != s || t.owner != owner || !t.active.Load() {
		return nil, runtime.ErrScope
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.sql, nil
}
func (s *Store) localToken(ctx context.Context, token runtime.Tx) (*sql.Tx, error) {
	t, ok := token.(*transaction)
	if !ok || t == nil {
		return nil, runtime.ErrScope
	}
	return s.token(ctx, token, t.owner)
}
func (s *Store) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	if ctx == nil || fn == nil {
		return errors.New("finite transaction context and callback required")
	}
	if _, err := contract.Encode(owner); err != nil {
		return runtime.ErrScope
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	stopShutdown := context.AfterFunc(s.shutdown, cancel)
	defer stopShutdown()
	select {
	case s.writer <- struct{}{}:
		defer func() { <-s.writer }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.closed.Load() {
		return errors.New("SQLite store closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	token := &transaction{store: s, sql: tx, owner: owner}
	token.active.Store(true)
	defer func() { token.active.Store(false); _ = tx.Rollback() }()
	settings, err := s.Settings(ctx, token)
	if err != nil {
		return err
	}
	if settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.ForeignKeys != 1 || settings.BusyTimeout != int(s.config.BusyTimeout.Milliseconds()) {
		return errors.New("SQLite effective durability rejected")
	}
	if err = fn(ctx, token); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	token.active.Store(false)
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", runtime.ErrCommitUnknown, err)
	}
	return nil
}
func (s *Store) Now(ctx context.Context, token runtime.Tx) (time.Time, error) {
	if _, err := s.localToken(ctx, token); err != nil {
		return time.Time{}, err
	}
	// Device Host's UTC wall clock is the trusted clock boundary, not CLI time.
	return time.Now().UTC(), nil
}

// Settings observes effective durability on the supplied transaction connection.
type Settings struct {
	JournalMode, SQLiteVersion            string
	Synchronous, ForeignKeys, BusyTimeout int
}

func (s *Store) Settings(ctx context.Context, token runtime.Tx) (Settings, error) {
	var out Settings
	tx, err := s.localToken(ctx, token)
	if err != nil {
		return out, err
	}
	for _, query := range []struct {
		sql   string
		value any
	}{
		{"PRAGMA journal_mode", &out.JournalMode}, {"PRAGMA synchronous", &out.Synchronous},
		{"PRAGMA foreign_keys", &out.ForeignKeys}, {"PRAGMA busy_timeout", &out.BusyTimeout},
		{"SELECT sqlite_version()", &out.SQLiteVersion},
	} {
		if err = tx.QueryRowContext(ctx, query.sql).Scan(query.value); err != nil {
			return out, err
		}
	}
	return out, nil
}
