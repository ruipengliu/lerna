// Package sqlitefetch stores bounded original acquisition identities and task
// request reservations. Only qualified execution may access this trusted seam.
package sqlitefetch

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/sys/unix"
	"lerna/fetch"
	"lerna/tasks"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const timeout = 2 * time.Second

type Store struct{ db *sql.DB }

var _ fetch.AttemptStore = (*Store)(nil)

func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, fetch.Invalid
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fetch.Invalid
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fetch.Unavailable
	}
	var info unix.Stat_t
	err = unix.Fstat(int(file.Fd()), &info)
	file.Close()
	if err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0777 != 0600 || info.Uid != uint32(os.Getuid()) || info.Nlink != 1 {
		return nil, fetch.Invalid
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": []string{"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fetch.Unavailable
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS fetch_lock(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);INSERT OR IGNORE INTO fetch_lock VALUES(1,1);CREATE TABLE IF NOT EXISTS fetch_budgets(namespace TEXT NOT NULL,task TEXT NOT NULL,subject TEXT NOT NULL,limit_requests INTEGER NOT NULL,charged INTEGER NOT NULL,PRIMARY KEY(namespace,task));CREATE TABLE IF NOT EXISTS fetch_attempts(namespace TEXT NOT NULL,operation TEXT NOT NULL,intent BLOB NOT NULL CHECK(length(intent)<=4096),PRIMARY KEY(namespace,operation));CREATE TABLE IF NOT EXISTS fetch_outcomes(namespace TEXT NOT NULL,operation TEXT NOT NULL,outcome BLOB NOT NULL CHECK(length(outcome)<=1024),PRIMARY KEY(namespace,operation));`)
	if err != nil {
		db.Close()
		return nil, fetch.Unavailable
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func name(s string) bool      { return s != "" && len(s) <= 256 && utf8.ValidString(s) }
func valid(in fetch.AttemptIntent) bool {
	raw, err := hex.DecodeString(in.Fingerprint)
	return (in.EvidenceOperation == "" || name(in.EvidenceOperation)) && name(in.Task.Namespace) && name(in.Task.TaskID) && name(in.Subject) && name(in.OperationID) && err == nil && len(raw) == 32 && in.MaxRequests >= 1 && in.MaxRequests <= 5 && in.TaskLimit >= in.MaxRequests && in.TaskLimit <= 128
}

type reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inspect(ctx context.Context, q reader, namespace, operation string) (fetch.AttemptIntent, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT intent FROM fetch_attempts WHERE namespace=? AND operation=?`, namespace, operation).Scan(&raw)
	if err == sql.ErrNoRows {
		return fetch.AttemptIntent{}, fetch.Missing
	}
	if err != nil || len(raw) > 4096 {
		return fetch.AttemptIntent{}, fetch.Unavailable
	}
	var in fetch.AttemptIntent
	if json.Unmarshal(raw, &in) != nil || !valid(in) || in.Task.Namespace != namespace || in.OperationID != operation {
		return fetch.AttemptIntent{}, fetch.Unavailable
	}
	return in, nil
}
func budget(ctx context.Context, q reader, ref tasks.Ref) (fetch.TaskBudget, error) {
	var b fetch.TaskBudget
	err := q.QueryRowContext(ctx, `SELECT subject,limit_requests,charged FROM fetch_budgets WHERE namespace=? AND task=?`, ref.Namespace, ref.TaskID).Scan(&b.Subject, &b.Limit, &b.Charged)
	if err == sql.ErrNoRows {
		return b, fetch.Missing
	}
	if err != nil || !name(b.Subject) || b.Limit < 1 || b.Limit > 128 || b.Charged > b.Limit {
		return fetch.TaskBudget{}, fetch.Unavailable
	}
	return b, nil
}
func (s *Store) Inspect(ctx context.Context, namespace, operation string) (fetch.AttemptIntent, error) {
	if !name(namespace) || !name(operation) {
		return fetch.AttemptIntent{}, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return inspect(ctx, s.db, namespace, operation)
}
func (s *Store) Budget(ctx context.Context, ref tasks.Ref) (fetch.TaskBudget, error) {
	if !name(ref.Namespace) || !name(ref.TaskID) {
		return fetch.TaskBudget{}, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return budget(ctx, s.db, ref)
}
func (s *Store) Begin(ctx context.Context, in fetch.AttemptIntent) (bool, error) {
	if !valid(in) {
		return false, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fetch.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE fetch_lock SET version=version WHERE id=1`); err != nil {
		return false, fetch.Unavailable
	}
	old, err := inspect(ctx, tx, in.Task.Namespace, in.OperationID)
	if err == nil {
		if old != in {
			return false, fetch.IdentityConflict
		}
		return false, nil
	}
	if err != fetch.Missing {
		return false, err
	}
	b, err := budget(ctx, tx, in.Task)
	if err != nil && err != fetch.Missing {
		return false, err
	}
	if err == nil && (b.Subject != in.Subject || b.Limit != in.TaskLimit) {
		return false, fetch.IdentityConflict
	}
	if err == fetch.Missing {
		b = fetch.TaskBudget{Subject: in.Subject, Limit: in.TaskLimit}
		var count int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fetch_budgets`).Scan(&count) != nil {
			return false, fetch.Unavailable
		}
		if count >= 512 {
			return false, fetch.Capacity
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fetch_budgets VALUES(?,?,?,?,0)`, in.Task.Namespace, in.Task.TaskID, in.Subject, in.TaskLimit); err != nil {
			return false, fetch.Unavailable
		}
	}
	limited := in.MaxRequests > b.Limit-b.Charged
	var count int
	if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fetch_attempts`).Scan(&count) != nil {
		return false, fetch.Unavailable
	}
	if count >= 512 {
		return false, fetch.Capacity
	}
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > 4096 {
		return false, fetch.Invalid
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fetch_attempts VALUES(?,?,?)`, in.Task.Namespace, in.OperationID, raw); err != nil {
		return false, fetch.Unavailable
	}
	if limited {
		// Preserve this exact rejection without reserving requests. Inspection
		// must distinguish a known unsent attempt from an unknown acquisition.
		outcome, _ := json.Marshal(fetch.Outcome{Status: "limit_exceeded"})
		if _, err = tx.ExecContext(ctx, `INSERT INTO fetch_outcomes VALUES(?,?,?)`, in.Task.Namespace, in.OperationID, outcome); err != nil {
			return false, fetch.Unavailable
		}
		if err = tx.Commit(); err != nil {
			return false, fetch.Unavailable
		}
		return false, fetch.LimitExceeded
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fetch_budgets SET charged=charged+? WHERE namespace=? AND task=?`, in.MaxRequests, in.Task.Namespace, in.Task.TaskID); err != nil {
		return false, fetch.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return false, fetch.Unavailable
	}
	return true, nil
}
