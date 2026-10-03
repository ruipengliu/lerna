// Package sqlite 保存设备原账本。迁移必须由管理入口显式执行；构造不会接纳或发送工作。
package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/adapters/internal/durable"
	sqlitedb "github.com/ruipengliu/lerna/adapters/sqlite/gen"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

//go:embed migrations/001_runtime.sql
var migration string

type CommitPhase string

const (
	BeforeCommit CommitPhase = "before_commit"
	AfterCommit  CommitPhase = "after_commit"
)

// CommitFault 仅用于管理验证故障：before 返回确认回滚，after 模拟提交回复丢失。
type CommitFault func(CommitPhase) error
type Option func(*Store)

func WithCommitFault(f CommitFault) Option { return func(s *Store) { s.fault = f } }

type Store struct {
	db     *sql.DB
	writer chan struct{}
	mu     sync.RWMutex
	id     string
	fault  CommitFault
	closed atomic.Bool
}

var _ runtime.Store = (*Store)(nil)

// Open 只打开持久文件并验证 SQLite 设置。内存库不能作为可恢复权威。
func Open(path string, options ...Option) (*Store, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, api.E("invalid_request", "persistent_sqlite_path_required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("_journal_mode", "WAL")
	q.Set("_synchronous", "FULL")
	q.Set("_foreign_keys", "on")
	q.Set("_txlock", "immediate")
	q.Set("_busy_timeout", "5000")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, writer: make(chan struct{}, 1)}
	for _, option := range options {
		option(s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	var journal string
	var syncMode, foreignKeys int
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		db.Close()
		return nil, err
	}
	if err = db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&syncMode); err != nil {
		db.Close()
		return nil, err
	}
	if err = db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		db.Close()
		return nil, err
	}
	if journal != "wal" || syncMode != 2 || foreignKeys != 1 {
		db.Close()
		return nil, api.E("dependency_unavailable", "sqlite_durability_configuration_rejected")
	}
	err = db.QueryRowContext(ctx, "SELECT database_id FROM harness_store_metadata WHERE singleton=1").Scan(&s.id)
	if err != nil && !strings.Contains(err.Error(), "no such table") && !errors.Is(err, sql.ErrNoRows) {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ID() string   { s.mu.RLock(); defer s.mu.RUnlock(); return s.id }
func (s *Store) Close() error { s.closed.Store(true); return s.db.Close() }
func (s *Store) acquire(ctx context.Context) error {
	if s.closed.Load() {
		return sql.ErrConnDone
	}
	select {
	case s.writer <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Store) release() { <-s.writer }

// Migrate 在独立短事务记录制品摘要与数据库身份；业务启动应只调用 Open。
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, migration); err != nil {
		return err
	}
	digest := api.Hash([]byte(migration))
	var old string
	err = tx.QueryRowContext(ctx, "SELECT artifact_digest FROM harness_migrations WHERE migration_id=1").Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if old != "" && old != digest {
		return api.E("invalid_state", "migration_artifact_changed")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO harness_store_metadata(singleton,database_id) VALUES(1,?) ON CONFLICT(singleton) DO NOTHING", api.NewID("database")); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO harness_migrations(migration_id,artifact_digest,state,checkpoint) VALUES(1,?,'applied','schema_ready') ON CONFLICT(migration_id) DO NOTHING", digest); err != nil {
		return err
	}
	var id string
	if err = tx.QueryRowContext(ctx, "SELECT database_id FROM harness_store_metadata WHERE singleton=1").Scan(&id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("migration commit: %w", err)
	}
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
	return nil
}

type transaction struct {
	db           *sql.Tx
	queries      *sqlitedb.Queries
	scope        runtime.Scope
	participants map[string]bool
	active       bool
	savepoint    uint64
}

func (tx *transaction) Scope() runtime.Scope { return tx.scope }
func (tx *transaction) check() error {
	if !tx.active {
		return api.E("invalid_state", "transaction_already_closed")
	}
	return nil
}
func (tx *transaction) namespace(ns string) error {
	if err := tx.check(); err != nil {
		return err
	}
	return durable.Participant(ns, tx.participants)
}

func (s *Store) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	if err := durable.Scope(scope, s.ID()); err != nil {
		return runtime.RolledBack, err
	}
	parts, err := durable.Participants(participants)
	if err != nil {
		return runtime.RolledBack, err
	}
	if fn == nil {
		return runtime.RolledBack, api.E("invalid_request", "transaction_callback_required")
	}
	if err := s.acquire(ctx); err != nil {
		return runtime.RolledBack, err
	}
	defer s.release()
	dbtx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runtime.RolledBack, err
	}
	tx := &transaction{db: dbtx, queries: sqlitedb.New(dbtx), scope: scope, participants: parts, active: true}
	defer func() { tx.active = false; dbtx.Rollback() }()
	if err = fn(tx); err != nil {
		return runtime.RolledBack, err
	}
	if err = ctx.Err(); err != nil {
		return runtime.RolledBack, err
	}
	if s.fault != nil {
		if err = s.fault(BeforeCommit); err != nil {
			return runtime.RolledBack, err
		}
	}
	if err = dbtx.Commit(); err != nil {
		// database/sql context cancellation before SQLite COMMIT 已确认回滚；其余提交错误保守核原身份。
		if errors.Is(err, sql.ErrTxDone) && ctx.Err() != nil {
			return runtime.RolledBack, ctx.Err()
		}
		return runtime.CommitUnknown, errors.Join(runtime.ErrCommitUnknown, err)
	}
	if s.fault != nil {
		if err = s.fault(AfterCommit); err != nil {
			return runtime.CommitUnknown, errors.Join(runtime.ErrCommitUnknown, err)
		}
	}
	return runtime.Committed, nil
}

func readArgs(scope runtime.Scope, ns, id string) sqlitedb.GetHeadParams {
	return sqlitedb.GetHeadParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, Namespace: ns, ObjectID: id}
}
func recordError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrNotFound
	}
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "PRIMARY KEY constraint failed")) {
		return runtime.ErrConflict
	}
	return err
}
func read(ctx context.Context, queries *sqlitedb.Queries, scope runtime.Scope, ns, id string, revision uint64, value any) (uint64, error) {
	if err := durable.Namespace(ns); err != nil {
		return 0, err
	}
	if err := durable.Text(id); err != nil {
		return 0, err
	}
	if revision > api.MaxSafeInteger {
		return 0, api.E("invalid_request", "invalid_revision")
	}
	if revision == 0 {
		row, err := queries.GetHead(ctx, readArgs(scope, ns, id))
		if err != nil {
			return 0, recordError(err)
		}
		return uint64(row.Revision), json.Unmarshal(row.Data, value)
	}
	b, err := queries.GetVersion(ctx, sqlitedb.GetVersionParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, Namespace: ns, ObjectID: id, Revision: int64(revision)})
	if err != nil {
		return 0, recordError(err)
	}
	return revision, json.Unmarshal(b, value)
}
func (s *Store) Read(ctx context.Context, scope runtime.Scope, ns, id string, rev uint64, value any) (uint64, error) {
	if err := durable.Scope(scope, s.ID()); err != nil {
		return 0, err
	}
	return read(ctx, sqlitedb.New(s.db), scope, ns, id, rev, value)
}
func (tx *transaction) Get(ctx context.Context, ns, id string, value any) (uint64, error) {
	if err := tx.namespace(ns); err != nil {
		return 0, err
	}
	return read(ctx, tx.queries, tx.scope, ns, id, 0, value)
}
func (tx *transaction) GetVersion(ctx context.Context, ns, id string, rev uint64, value any) error {
	if err := tx.namespace(ns); err != nil {
		return err
	}
	if rev == 0 {
		return api.E("invalid_request", "exact_revision_required")
	}
	_, err := read(ctx, tx.queries, tx.scope, ns, id, rev, value)
	return err
}
func (tx *transaction) Create(ctx context.Context, ns, id, parent string, value any) error {
	if err := tx.namespace(ns); err != nil {
		return err
	}
	if err := durable.Text(id); err != nil {
		return err
	}
	if parent != "" {
		if err := durable.Text(parent); err != nil {
			return err
		}
	}
	b, err := durable.Record(value)
	if err != nil {
		return err
	}
	if err = tx.queries.CreateHead(ctx, sqlitedb.CreateHeadParams{TenantID: tx.scope.TenantID, OwnerID: tx.scope.OwnerID, Namespace: ns, ObjectID: id, ParentID: parent, Data: b}); err != nil {
		return recordError(err)
	}
	return recordError(tx.queries.CreateVersion(ctx, sqlitedb.CreateVersionParams{TenantID: tx.scope.TenantID, OwnerID: tx.scope.OwnerID, Namespace: ns, ObjectID: id, Revision: 1, Data: b}))
}
func (tx *transaction) Put(ctx context.Context, ns, id string, expected uint64, value any) error {
	if err := tx.namespace(ns); err != nil {
		return err
	}
	if err := durable.Text(id); err != nil {
		return err
	}
	if expected == 0 || expected >= api.MaxSafeInteger {
		return api.E("invalid_request", "invalid_expected_revision")
	}
	b, err := durable.Record(value)
	if err != nil {
		return err
	}
	n, err := tx.queries.PutHead(ctx, sqlitedb.PutHeadParams{Data: b, TenantID: tx.scope.TenantID, OwnerID: tx.scope.OwnerID, Namespace: ns, ObjectID: id, Revision: int64(expected)})
	if err != nil {
		return recordError(err)
	}
	if n != 1 {
		return runtime.ErrConflict
	}
	return recordError(tx.queries.CreateVersion(ctx, sqlitedb.CreateVersionParams{TenantID: tx.scope.TenantID, OwnerID: tx.scope.OwnerID, Namespace: ns, ObjectID: id, Revision: int64(expected + 1), Data: b}))
}
func list(ctx context.Context, q *sqlitedb.Queries, scope runtime.Scope, ns, parent, after string, limit int) ([]runtime.Record, error) {
	if err := durable.Namespace(ns); err != nil {
		return nil, err
	}
	if err := durable.Limits(limit); err != nil {
		return nil, err
	}
	if parent != "" {
		if err := durable.Text(parent); err != nil {
			return nil, err
		}
	}
	if after != "" {
		if err := durable.Text(after); err != nil {
			return nil, err
		}
	}
	rows, err := q.ListHeads(ctx, sqlitedb.ListHeadsParams{TenantID: scope.TenantID, OwnerID: scope.OwnerID, Namespace: ns, Column4: parent, ParentID: parent, ObjectID: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	records := make([]runtime.Record, 0, len(rows))
	for _, r := range rows {
		records = append(records, runtime.Record{ID: r.ObjectID, Revision: uint64(r.Revision), ParentID: r.ParentID, Data: r.Data})
	}
	return records, nil
}
func (s *Store) List(ctx context.Context, scope runtime.Scope, ns, parent, after string, limit int) ([]runtime.Record, error) {
	if err := durable.Scope(scope, s.ID()); err != nil {
		return nil, err
	}
	return list(ctx, sqlitedb.New(s.db), scope, ns, parent, after, limit)
}
func (tx *transaction) List(ctx context.Context, ns, parent, after string, limit int) ([]runtime.Record, error) {
	if err := tx.namespace(ns); err != nil {
		return nil, err
	}
	return list(ctx, tx.queries, tx.scope, ns, parent, after, limit)
}

func (tx *transaction) LookupKey(ctx context.Context, ns, key string) (runtime.SemanticKey, error) {
	if err := tx.namespace(ns); err != nil {
		return runtime.SemanticKey{}, err
	}
	if err := durable.Text(key); err != nil {
		return runtime.SemanticKey{}, err
	}
	var result runtime.SemanticKey
	err := tx.db.QueryRowContext(ctx, "SELECT object_id,digest FROM runtime_semantic_keys WHERE tenant_id=? AND owner_id=? AND namespace=? AND semantic_key=?", tx.scope.TenantID, tx.scope.OwnerID, ns, key).Scan(&result.ObjectID, &result.Digest)
	return result, recordError(err)
}
func (tx *transaction) Bind(ctx context.Context, ns, key, id, digest string) error {
	if err := tx.namespace(ns); err != nil {
		return err
	}
	for _, v := range []string{key, id, digest} {
		if err := durable.Text(v); err != nil {
			return err
		}
	}
	old, err := tx.LookupKey(ctx, ns, key)
	if err == nil {
		if old.ObjectID == id && old.Digest == digest {
			return nil
		}
		return api.E("idempotency_conflict", "semantic_key_input_changed")
	}
	if !errors.Is(err, runtime.ErrNotFound) {
		return err
	}
	_, err = tx.db.ExecContext(ctx, "INSERT INTO runtime_semantic_keys(tenant_id,owner_id,namespace,semantic_key,object_id,digest) VALUES(?,?,?,?,?,?)", tx.scope.TenantID, tx.scope.OwnerID, ns, key, id, digest)
	return recordError(err)
}

func (tx *transaction) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	if err := tx.check(); err != nil {
		return err
	}
	if fn == nil {
		return api.E("invalid_request", "savepoint_callback_required")
	}
	tx.savepoint++
	name := fmt.Sprintf("business_%d", tx.savepoint)
	if _, err := tx.db.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	err := fn(tx)
	if err != nil {
		if _, rollbackErr := tx.db.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		if _, releaseErr := tx.db.ExecContext(ctx, "RELEASE SAVEPOINT "+name); releaseErr != nil {
			return errors.Join(err, releaseErr)
		}
		return err
	}
	_, err = tx.db.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

func (tx *transaction) Now(ctx context.Context) (time.Time, error) {
	if err := tx.check(); err != nil {
		return time.Time{}, err
	}
	var text string
	err := tx.db.QueryRowContext(ctx, "SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now')").Scan(&text)
	if err != nil {
		return time.Time{}, err
	}
	return api.ParseTime(text)
}

func (tx *transaction) LoadCommand(ctx context.Context, id string) (runtime.StoredCommand, error) {
	if err := tx.check(); err != nil {
		return runtime.StoredCommand{}, err
	}
	if err := durable.Identity(id); err != nil {
		return runtime.StoredCommand{}, err
	}
	if _, err := tx.db.ExecContext(ctx, "INSERT INTO runtime_command_locks(tenant_id,owner_id,command_id) VALUES(?,?,?) ON CONFLICT DO NOTHING", tx.scope.TenantID, tx.scope.OwnerID, id); err != nil {
		return runtime.StoredCommand{}, err
	}
	return loadCommand(ctx, tx.db, tx.scope, id)
}

type commandReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadCommand(ctx context.Context, db commandReader, scope runtime.Scope, id string) (runtime.StoredCommand, error) {
	var b []byte
	err := db.QueryRowContext(ctx, "SELECT data FROM runtime_commands WHERE tenant_id=? AND owner_id=? AND command_id=?", scope.TenantID, scope.OwnerID, id).Scan(&b)
	if err != nil {
		return runtime.StoredCommand{}, recordError(err)
	}
	var result runtime.StoredCommand
	err = json.Unmarshal(b, &result)
	return result, err
}
func (s *Store) LookupCommand(ctx context.Context, scope runtime.Scope, id string) (runtime.StoredCommand, error) {
	if err := durable.Scope(scope, s.ID()); err != nil {
		return runtime.StoredCommand{}, err
	}
	if err := durable.Identity(id); err != nil {
		return runtime.StoredCommand{}, err
	}
	return loadCommand(ctx, s.db, scope, id)
}
func (tx *transaction) SaveCommand(ctx context.Context, next runtime.StoredCommand) error {
	old, err := tx.LoadCommand(ctx, next.Command.CommandID)
	var previous *runtime.StoredCommand
	if err == nil {
		previous = &old
	} else if !errors.Is(err, runtime.ErrNotFound) {
		return err
	}
	if err = durable.Command(tx.scope, previous, next); err != nil {
		return err
	}
	b, err := durable.Record(next)
	if err != nil {
		return err
	}
	_, err = tx.db.ExecContext(ctx, "INSERT INTO runtime_commands(tenant_id,owner_id,command_id,principal_id,digest,data) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,owner_id,command_id) DO UPDATE SET data=excluded.data", tx.scope.TenantID, tx.scope.OwnerID, next.Command.CommandID, next.PrincipalID, next.Digest, b)
	return err
}
