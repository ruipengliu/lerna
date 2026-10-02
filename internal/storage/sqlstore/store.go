// Package sqlstore shares adapter mechanics; SQL remains independently generated
// for PostgreSQL and SQLite. Only trusted repository implementations use DBTX.
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ruipengliu/lerna/internal/durable"
)

type Queries struct{ Now, Reserve, Lookup, Save, Raise, Hint, Lock, Candidates, Lease, Update, Delete, Open, Repair, LookupJobKey, RepairJobs, LookupJobKeys string }
type Stats struct {
	SQL                 int64
	OpenJobs, OldestDue int64
	Connections         sql.DBStats
}

// DBTX has no transaction-control operations. Keep it inside trusted adapters.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Store struct {
	db         *sql.DB
	dialect    string
	q          Queries
	scopes     map[durable.Scope]bool
	writer     chan struct{}
	wake       chan struct{}
	sql        atomic.Int64
	NotifyFunc func()
	selectors  sync.Map
}

// CandidateSelector belongs to trusted storage assembly. It may select and
// lock bounded job rows and advance mechanical fairness counters; Lease and
// commit confirmation remain in the shared durable engine.
type CandidateSelector func(context.Context, DBTX, durable.Scope, string, int) ([]durable.Job, error)

func (s *Store) SetCandidateSelector(scope durable.Scope, selector CandidateSelector) error {
	if !s.scopes[scope] || selector == nil {
		return durable.ErrScope
	}
	s.selectors.Store(scope, selector)
	return nil
}

func New(db *sql.DB, dialect string, q Queries, scopes []durable.Scope) (*Store, error) {
	if db == nil || len(scopes) == 0 || dialect != "postgres" && dialect != "sqlite" {
		return nil, durable.ErrScope
	}
	s := &Store{db: db, dialect: dialect, q: q, scopes: map[durable.Scope]bool{}, writer: make(chan struct{}, 1), wake: make(chan struct{}, 1)}
	for _, scope := range scopes {
		if !scope.Valid() {
			return nil, durable.ErrScope
		}
		s.scopes[scope] = true
	}
	return s, nil
}
func (s *Store) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if s.NotifyFunc != nil {
		s.NotifyFunc()
	}
}
func (s *Store) Signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Store) Wake() <-chan struct{} { return s.wake }
func (s *Store) Begin(ctx context.Context, scope durable.Scope) (durable.Session, error) {
	if !s.scopes[scope] {
		return nil, durable.ErrScope
	}
	x := &session{store: s, ctx: ctx}
	if s.dialect == "sqlite" {
		select {
		case s.writer <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		conn, err := s.db.Conn(ctx)
		if err != nil {
			<-s.writer
			return nil, err
		}
		x.conn = conn
		x.dbtx = conn
		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			conn.Close()
			<-s.writer
			return nil, err
		}
	} else {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return nil, err
		}
		x.tx = tx
		x.dbtx = tx
	}
	x.active.Store(true)
	return x, nil
}
func (s *Store) Retryable(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "40001" || pg.Code == "40P01"
	}
	var sqlite interface{ Code() int }
	return errors.As(err, &sqlite) && (sqlite.Code()&255 == 5 || sqlite.Code()&255 == 6)
}

type confirmedRollback struct{ error }

func (e confirmedRollback) Unwrap() error { return e.error }
func (s *Store) CommitRolledBack(err error) bool {
	var rollback confirmedRollback
	if errors.As(err, &rollback) {
		return true
	}
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01")
}
func (s *Store) Stats(ctx context.Context, scope durable.Scope) (Stats, error) {
	if !s.scopes[scope] {
		return Stats{}, durable.ErrScope
	}
	stats := Stats{SQL: s.sql.Load(), Connections: s.db.Stats()}
	s.sql.Add(1)
	err := s.db.QueryRowContext(ctx, s.q.Open, scope.TenantID, scope.OwnerID).Scan(&stats.OpenJobs, &stats.OldestDue)
	return stats, err
}

type session struct {
	store  *Store
	ctx    context.Context
	dbtx   DBTX
	tx     *sql.Tx
	conn   *sql.Conn
	active atomic.Bool
}
type handle struct {
	x   *session
	ctx context.Context
}

func (h handle) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	if !h.x.active.Load() || h.ctx.Err() != nil {
		return nil, durable.ErrTx
	}
	h.x.store.sql.Add(1)
	return h.x.dbtx.ExecContext(h.ctx, q, args...)
}
func (h handle) QueryContext(_ context.Context, q string, args ...any) (*sql.Rows, error) {
	if !h.x.active.Load() || h.ctx.Err() != nil {
		return nil, durable.ErrTx
	}
	h.x.store.sql.Add(1)
	return h.x.dbtx.QueryContext(h.ctx, q, args...)
}
func (h handle) QueryRowContext(_ context.Context, q string, args ...any) *sql.Row {
	if !h.x.active.Load() || h.ctx.Err() != nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return h.x.store.db.QueryRowContext(ctx, "SELECT 0")
	}
	h.x.store.sql.Add(1)
	return h.x.dbtx.QueryRowContext(h.ctx, q, args...)
}
func (x *session) Handle(ctx context.Context) any { return handle{x, ctx} }
func (x *session) h() handle                      { return handle{x, x.ctx} }
func (x *session) Now(ctx context.Context) (int64, error) {
	var v int64
	err := x.h().QueryRowContext(ctx, x.store.q.Now).Scan(&v)
	return v, err
}
func (x *session) Reserve(ctx context.Context, s durable.Scope, r durable.CommandRecord) (durable.CommandRecord, bool, error) {
	result, err := x.h().ExecContext(ctx, x.store.q.Reserve, s.TenantID, s.OwnerID, r.Key.ServiceID, r.Key.CommandID, r.Digest, r.Method, r.TargetID, r.ExpiresAt)
	if err != nil {
		return r, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return r, false, err
	}
	r, err = x.Lookup(ctx, s, r.Key)
	if count == 0 && errors.Is(err, durable.ErrNotFound) {
		err = durable.ErrScope
	}
	return r, count == 1, err
}
func (x *session) Lookup(ctx context.Context, s durable.Scope, key durable.CommandKey) (durable.CommandRecord, error) {
	r := durable.CommandRecord{Key: key}
	var receipt string
	err := x.h().QueryRowContext(ctx, x.store.q.Lookup, s.TenantID, s.OwnerID, key.ServiceID, key.CommandID).Scan(&r.Digest, &r.Method, &r.TargetID, &r.State, &r.PreparationRef, &receipt, &r.DecisionType, &r.ExpiresAt, &r.RetainUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return r, durable.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if receipt != "" {
		r.Receipt = &durable.Receipt{}
		if err = json.Unmarshal([]byte(receipt), r.Receipt); err != nil {
			return r, durable.ErrInvariant
		}
	}
	return r, nil
}
func (x *session) Save(ctx context.Context, s durable.Scope, r durable.CommandRecord) error {
	receipt := ""
	if r.Receipt != nil {
		raw, err := json.Marshal(r.Receipt)
		if err != nil {
			return err
		}
		receipt = string(raw)
	}
	result, err := x.h().ExecContext(ctx, x.store.q.Save, s.TenantID, s.OwnerID, r.Key.ServiceID, r.Key.CommandID, r.State, r.PreparationRef, receipt, r.DecisionType, r.RetainUntil)
	return affected(result, err)
}

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (durable.Job, error) {
	var j durable.Job
	err := row.Scan(&j.Scope.TenantID, &j.Scope.OwnerID, &j.Key.Kind, &j.Key.Responsibility, &j.ID, &j.SourceRef, &j.State, &j.DueAt, &j.WorkRevision, &j.LeaseEpoch, &j.LeaseUntil, &j.HolderID, &j.WaitReason)
	if errors.Is(err, sql.ErrNoRows) {
		err = durable.ErrNotFound
	}
	return j, err
}
func (x *session) Raise(ctx context.Context, s durable.Scope, k durable.JobKey, source, id string, due int64) (durable.Job, error) {
	j, err := scanJob(x.h().QueryRowContext(ctx, x.store.q.Raise, s.TenantID, s.OwnerID, k.Kind, k.Responsibility, id, source, due))
	if errors.Is(err, durable.ErrNotFound) {
		err = durable.ErrConflict
	}
	return j, err
}
func (x *session) Hint(ctx context.Context, s durable.Scope, k durable.JobKey, due int64) error {
	_, err := x.h().ExecContext(ctx, x.store.q.Hint, s.TenantID, s.OwnerID, k.Kind, k.Responsibility, due)
	return err
}
func (x *session) Repair(ctx context.Context, s durable.Scope, k durable.JobKey, source, id string, due int64) (durable.Job, bool, error) {
	r, e := x.h().ExecContext(ctx, x.store.q.Repair, s.TenantID, s.OwnerID, k.Kind, k.Responsibility, id, source, due)
	if e != nil {
		return durable.Job{}, false, e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return durable.Job{}, false, e
	}
	j, e := scanJob(x.h().QueryRowContext(ctx, x.store.q.LookupJobKey, s.TenantID, s.OwnerID, k.Kind, k.Responsibility))
	if e != nil {
		return j, false, e
	}
	if j.SourceRef != source {
		return j, false, durable.ErrConflict
	}
	return j, n > 0, nil
}
func (x *session) LockJob(ctx context.Context, s durable.Scope, id string) (durable.Job, error) {
	return scanJob(x.h().QueryRowContext(ctx, x.store.q.Lock, s.TenantID, s.OwnerID, id))
}
func (x *session) Candidates(ctx context.Context, s durable.Scope, kind string, limit int) ([]durable.Job, error) {
	if selector, ok := x.store.selectors.Load(s); ok {
		return selector.(CandidateSelector)(ctx, x.h(), s, kind, limit)
	}
	rows, err := x.h().QueryContext(ctx, x.store.q.Candidates, s.TenantID, s.OwnerID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []durable.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (x *session) Lease(ctx context.Context, j durable.Job, holder string, until int64) (durable.Job, error) {
	return scanJob(x.h().QueryRowContext(ctx, x.store.q.Lease, j.Scope.TenantID, j.Scope.OwnerID, j.ID, holder, until))
}
func (x *session) UpdateJob(ctx context.Context, j durable.Job) error {
	result, err := x.h().ExecContext(ctx, x.store.q.Update, j.Scope.TenantID, j.Scope.OwnerID, j.ID, j.State, j.DueAt, j.HolderID, j.LeaseUntil, j.WaitReason)
	return affected(result, err)
}
func (x *session) DeleteDone(ctx context.Context, s durable.Scope, k durable.JobKey) error {
	result, err := x.h().ExecContext(ctx, x.store.q.Delete, s.TenantID, s.OwnerID, k.Kind, k.Responsibility)
	return affected(result, err)
}
func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return durable.ErrPrecondition
	}
	return nil
}
func (x *session) release() {
	if x.conn != nil {
		x.conn.Close()
		<-x.store.writer
	}
}
func (x *session) Commit(ctx context.Context) error {
	x.active.Store(false)
	if x.tx != nil {
		return x.tx.Commit()
	}
	_, err := x.conn.ExecContext(ctx, "COMMIT")
	if err != nil {
		rb, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, rbErr := x.conn.ExecContext(rb, "ROLLBACK")
		cancel()
		x.release()
		if rbErr == nil {
			return confirmedRollback{err}
		}
		return errors.Join(err, rbErr)
	}
	x.release()
	return nil
}
func (x *session) Rollback(ctx context.Context) error {
	x.active.Store(false)
	if x.tx != nil {
		err := x.tx.Rollback()
		if errors.Is(err, sql.ErrTxDone) {
			return nil
		}
		return err
	}
	_, err := x.conn.ExecContext(ctx, "ROLLBACK")
	x.release()
	return err
}

func (x *session) RepairMany(ctx context.Context, s durable.Scope, items []durable.JobRepair, due int64) ([]durable.Job, bool, error) {
	body, e := json.Marshal(items)
	if e != nil {
		return nil, false, e
	}
	r, e := x.h().ExecContext(ctx, x.store.q.RepairJobs, s.TenantID, s.OwnerID, string(body), due)
	if e != nil {
		return nil, false, e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return nil, false, e
	}
	rows, e := x.h().QueryContext(ctx, x.store.q.LookupJobKeys, s.TenantID, s.OwnerID, string(body))
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	jobs := []durable.Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, false, e
		}
		jobs = append(jobs, j)
	}
	return jobs, n > 0, rows.Err()
}
