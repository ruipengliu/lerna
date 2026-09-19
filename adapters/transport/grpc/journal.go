package grpc

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

// Journal retains bounded delivery identities on both peers. No automatic
// deletion: accepted reliable records remain recoverable across process exits.
// ponytail: finite installation ledger; add lifecycle-aware retirement when
// long-running deployments exhaust the configured record capacity.
type Journal struct {
	db    *sql.DB
	mu    sync.Mutex
	limit int
}

func OpenJournal(path string, limit int) (*Journal, error) {
	if path == "" || path == ":memory:" || limit < 1 || limit > 4096 {
		return nil, failure(authorization.Invalid)
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, failure(authorization.Denied)
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": {"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}, "_txlock": {"immediate"}}.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS delivery(peer TEXT NOT NULL, operation TEXT NOT NULL, seq INTEGER NOT NULL, payload BLOB NOT NULL, ack INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(peer,operation,seq))`, `CREATE TABLE IF NOT EXISTS configuration(id INTEGER PRIMARY KEY CHECK(id=1), capacity INTEGER NOT NULL)`} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	if _, e = db.Exec(`INSERT OR IGNORE INTO configuration VALUES(1,?)`, limit); e != nil {
		db.Close()
		return nil, e
	}
	var capacity int
	if e = db.QueryRow(`SELECT capacity FROM configuration WHERE id=1`).Scan(&capacity); e != nil || capacity != limit {
		db.Close()
		return nil, failure(authorization.Conflict)
	}
	return &Journal{db: db, limit: limit}, nil
}
func (j *Journal) Close() error { return j.db.Close() }
func (j *Journal) Save(ctx context.Context, peer string, u *wire.InvocationUpdate) (bool, error) {
	if len(peer) == 0 || len(peer) > 1024 || u == nil || u.OperationId == "" || len(u.OperationId) > 512 || u.Seq < 1 || u.Seq > 32 || !u.Reliable || !u.FullSnapshot || u.Snapshot == nil || u.Snapshot.Revision != u.Seq || proto.Size(u) > 1<<20 {
		return false, failure(authorization.Invalid)
	}
	raw, e := proto.MarshalOptions{Deterministic: true}.Marshal(u)
	if e != nil {
		return false, e
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	tx, e := j.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var old []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM delivery WHERE peer=? AND operation=? AND seq=?`, peer, u.OperationId, u.Seq).Scan(&old)
	if e == nil {
		if !bytes.Equal(old, raw) {
			return false, failure(authorization.IdentityConflict)
		}
		return false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return false, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM delivery`).Scan(&count); e != nil {
		return false, e
	}
	if count >= j.limit {
		return false, failure(authorization.Unavailable)
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO delivery(peer,operation,seq,payload) VALUES(?,?,?,?)`, peer, u.OperationId, u.Seq, raw); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, failure(authorization.OutcomeUnknown)
	}
	return true, nil
}
func (j *Journal) Pending(ctx context.Context, peer, op string) ([]*wire.InvocationUpdate, error) {
	rows, e := j.db.QueryContext(ctx, `SELECT payload FROM delivery WHERE peer=? AND operation=? AND ack=0 ORDER BY seq LIMIT 1`, peer, op)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []*wire.InvocationUpdate{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		u := new(wire.InvocationUpdate)
		if len(raw) > 1<<20 || proto.Unmarshal(raw, u) != nil {
			return nil, failure(authorization.Invalid)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (j *Journal) Ack(ctx context.Context, peer, op string, seq uint64) error {
	result, e := j.db.ExecContext(ctx, `UPDATE delivery SET ack=1 WHERE peer=? AND operation=? AND seq=?`, peer, op, seq)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return failure(authorization.Invalid)
	}
	return nil
}

// recorded returns the original snapshot for a revision even after its receipt.
// Core application progress may change without a new invocation revision; a
// transport retry must retain the originally published bytes for that identity.
func (j *Journal) recorded(ctx context.Context, peer, op string, seq uint64) (*wire.InvocationUpdate, error) {
	var raw []byte
	e := j.db.QueryRowContext(ctx, `SELECT payload FROM delivery WHERE peer=? AND operation=? AND seq=?`, peer, op, seq).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	u := new(wire.InvocationUpdate)
	if len(raw) > 1<<20 || proto.Unmarshal(raw, u) != nil {
		return nil, failure(authorization.Invalid)
	}
	return u, nil
}
