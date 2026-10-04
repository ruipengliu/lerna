// Package pgstore shares finite PostgreSQL mechanics between the demo and Decision owners.
// Business tables, migrations, admission facts, and receipts remain owner-specific.
package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type Config struct {
	DSN, Schema                                       string
	MaxOpenConnections                                int
	TransactionTimeout, StatementTimeout, LockTimeout time.Duration
}
type Core struct {
	db        *sql.DB
	config    Config
	created   atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// connectionError keeps driver classification available without putting
// connection credentials or untrusted driver text into default error output.
type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string { return e.message }
func (e *connectionError) Unwrap() error { return e.cause }

func Open(ctx context.Context, cfg Config) (*Core, error) { return open(ctx, cfg, sql.Open) }

// open supplies only the private database/sql construction seam for mechanical
// lifecycle tests. Runtime always uses the fixed pgx driver.
func open(ctx context.Context, cfg Config, openDB func(string, string) (*sql.DB, error)) (*Core, error) {
	if ctx == nil || cfg.DSN == "" || !identifier.MatchString(cfg.Schema) || cfg.TransactionTimeout <= 0 || cfg.StatementTimeout < time.Millisecond || cfg.LockTimeout < time.Millisecond || cfg.StatementTimeout > cfg.TransactionTimeout || cfg.LockTimeout > cfg.StatementTimeout {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	if cfg.MaxOpenConnections < 0 || cfg.MaxOpenConnections > 64 {
		return nil, errors.New("invalid PostgreSQL connection bounds")
	}
	bounded, cancel := context.WithTimeout(ctx, cfg.TransactionTimeout)
	defer cancel()
	db, err := openDB("pgx", cfg.DSN)
	if err != nil {
		return nil, &connectionError{message: "PostgreSQL connection configuration rejected", cause: err}
	}
	connections := cfg.MaxOpenConnections
	if connections == 0 {
		connections = 16
	}
	db.SetMaxOpenConns(connections)
	db.SetMaxIdleConns(min(connections, 4))
	holder := &Core{db: db, config: cfg}
	if err = db.PingContext(bounded); err != nil {
		startupErr := &connectionError{message: "PostgreSQL connection unavailable", cause: err}
		if closeErr := holder.Close(); closeErr != nil {
			return holder, errors.Join(startupErr, closeErr)
		}
		return nil, startupErr
	}
	return holder, nil
}
func (s *Core) DB() *sql.DB { return s.db }

// A failed first physical Close remains unknown: database/sql cannot retry it.
func (s *Core) Close() error {
	s.closeOnce.Do(func() {
		if err := s.db.Close(); err != nil {
			s.closeErr = &connectionError{message: "PostgreSQL close unconfirmed", cause: err}
		}
	})
	return s.closeErr
}
func (s *Core) Table(name string) string { return `"` + s.config.Schema + `"."` + name + `"` }
func (s *Core) CreateSchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `CREATE SCHEMA "`+s.config.Schema+`"`)
	if err == nil {
		s.created.Store(true)
	}
	return err
}

// DropTestSchema only removes a test namespace created by this Core instance.
func (s *Core) DropTestSchema(ctx context.Context) error {
	if !s.created.Load() || !regexp.MustCompile(`^lerna_test_[a-f0-9]{24}$`).MatchString(s.config.Schema) {
		return errors.New("test schema cleanup ownership denied")
	}
	_, err := s.db.ExecContext(ctx, `DROP SCHEMA "`+s.config.Schema+`" CASCADE`)
	if err == nil {
		s.created.Store(false)
	}
	return err
}

type Token struct {
	pool       *workpool.State
	poolLocked bool
	store      *Core
	sql        *sql.Tx
	owner      contract.OwnerRef
	active     atomic.Bool
}

func (t *Token) Owner() contract.OwnerRef { return t.owner }
func (s *Core) SQL(ctx context.Context, tx runtime.Tx, owner contract.OwnerRef) (*sql.Tx, error) {
	t, ok := tx.(*Token)
	if !ok || t == nil || t.store != s || t.owner != owner || !t.active.Load() {
		return nil, runtime.ErrScope
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.sql, nil
}
func (s *Core) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	if ctx == nil || fn == nil {
		return errors.New("finite transaction context and callback required")
	}
	if _, err := contract.Encode(owner); err != nil {
		return runtime.ErrScope
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	token := &Token{store: s, sql: tx, owner: owner}
	token.active.Store(true)
	defer func() { token.active.Store(false); _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('synchronous_commit','on',true), set_config('statement_timeout',$1,true), set_config('lock_timeout',$2,true)`, fmt.Sprintf("%dms", s.config.StatementTimeout.Milliseconds()), fmt.Sprintf("%dms", s.config.LockTimeout.Milliseconds())); err != nil {
		return err
	}
	if err = fn(ctx, token); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	token.active.Store(false)
	if err = tx.Commit(); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return err
		}
		return fmt.Errorf("%w: %w", runtime.ErrCommitUnknown, err)
	}
	return nil
}
func (s *Core) Now(ctx context.Context, token runtime.Tx) (time.Time, error) {
	tx, err := s.LocalSQL(ctx, token)
	if err != nil {
		return time.Time{}, err
	}
	var now time.Time
	err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

// Settings is an observation of effective settings on this transaction's connection.
type Settings struct{ Isolation, SynchronousCommit, StatementTimeout, LockTimeout, ServerVersion string }

func (s *Core) Settings(ctx context.Context, token runtime.Tx) (Settings, error) {
	var out Settings
	tx, err := s.LocalSQL(ctx, token)
	if err != nil {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT current_setting('transaction_isolation'),current_setting('synchronous_commit'),current_setting('statement_timeout'),current_setting('lock_timeout'),current_setting('server_version')`).Scan(&out.Isolation, &out.SynchronousCommit, &out.StatementTimeout, &out.LockTimeout, &out.ServerVersion)
	return out, err
}

func (s *Core) LocalSQL(ctx context.Context, token runtime.Tx) (*sql.Tx, error) {
	t, ok := token.(*Token)
	if !ok || t == nil {
		return nil, runtime.ErrScope
	}
	return s.SQL(ctx, token, t.owner)
}
