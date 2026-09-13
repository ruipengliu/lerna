// Package simapi implements an independently durable, versioned counter API.
// Only this coordinator owns its private SQLite target; it is not a shared
// external resource lock. Operation records are retained until the target ends.
package simapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"lerna/execution"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

type Driver struct{ db *sql.DB }
type Snapshot struct {
	Value   int64  `json:"value"`
	Version uint64 `json:"version"`
	Changes int    `json:"changes"`
}

func Open(path string) (*Driver, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, fmt.Errorf("private target file required")
	}
	f, e := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA busy_timeout=5000`, `PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE IF NOT EXISTS resource(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL,version INTEGER NOT NULL)`, `INSERT OR IGNORE INTO resource VALUES(1,0,1)`, `CREATE TABLE IF NOT EXISTS operations(id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,occurred INTEGER NOT NULL,value INTEGER NOT NULL,version INTEGER NOT NULL)`} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	return &Driver{db}, nil
}
func (d *Driver) Close() error { return d.db.Close() }
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
	var input struct {
		Delta int64 `json:"delta"`
	}
	if json.Unmarshal(c.Input, &input) != nil || input.Delta < 1 || input.Delta > 10 {
		return fmt.Errorf("invalid counter input")
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var digest string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint FROM operations WHERE id=?`, c.Request.OperationID).Scan(&digest)
	if e == nil {
		if digest != c.Request.Fingerprint() {
			return fmt.Errorf("identity conflict")
		}
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM operations`).Scan(&count); e != nil {
		return e
	}
	if count >= 64 {
		return fmt.Errorf("capacity")
	}
	var value int64
	var version uint64
	if e = tx.QueryRowContext(ctx, `SELECT value,version FROM resource WHERE id=1`).Scan(&value, &version); e != nil {
		return e
	}
	occurred := 0
	if version == c.Request.ResourceVersion {
		occurred = 1
		value += input.Delta
		version++
		if _, e = tx.ExecContext(ctx, `UPDATE resource SET value=?,version=? WHERE id=1`, value, version); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO operations VALUES(?,?,?,?,?)`, c.Request.OperationID, c.Request.Fingerprint(), occurred, value, version); e != nil {
		return e
	}
	return tx.Commit()
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	var digest string
	var occurred int
	var value int64
	var version uint64
	e := d.db.QueryRowContext(ctx, `SELECT fingerprint,occurred,value,version FROM operations WHERE id=?`, c.Request.OperationID).Scan(&digest, &occurred, &value, &version)
	if e == sql.ErrNoRows {
		return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
	}
	if e != nil {
		return execution.Observation{}, e
	}
	if digest != c.Request.Fingerprint() {
		return execution.Observation{}, fmt.Errorf("identity conflict")
	}
	body, _ := json.Marshal(struct {
		Value   int64  `json:"value"`
		Version uint64 `json:"version"`
	}{value, version})
	evidence, _ := json.Marshal(struct {
		Operation, Fingerprint string
		Occurred               bool
		Version                uint64
	}{c.Request.OperationID, digest, occurred == 1, version})
	if occurred == 0 {
		return execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Output: body, Evidence: evidence}, nil
	}
	return execution.Observation{Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Output: body, Evidence: evidence}, nil
}
func (d *Driver) Snapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	e := d.db.QueryRowContext(ctx, `SELECT value,version,(SELECT count(*) FROM operations WHERE occurred=1) FROM resource WHERE id=1`).Scan(&s.Value, &s.Version, &s.Changes)
	return s, e
}
