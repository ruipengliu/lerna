// Package simjob is an independent durable asynchronous counter target.
package simjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"lerna/execution"
	"lerna/internal/randomid"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Clock interface{ Now() (time.Time, error) }
type Driver struct {
	clock                    Clock
	db                       *sql.DB
	correlation, cancellable bool
}
type Snapshot struct {
	Value         int64
	Version       uint64
	Changes, Jobs int
}

func Open(path string, correlation, cancellable bool, clock Clock) (*Driver, error) {
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
	for _, q := range []string{`PRAGMA busy_timeout=5000`, `PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE IF NOT EXISTS resource(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL,version INTEGER NOT NULL)`, `INSERT OR IGNORE INTO resource VALUES(1,0,1)`, `CREATE TABLE IF NOT EXISTS expired_lookup(id TEXT PRIMARY KEY)`, `CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, handle TEXT UNIQUE NOT NULL, delta INTEGER NOT NULL, expected INTEGER NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL, value INTEGER NOT NULL, version INTEGER NOT NULL,completed_at INTEGER NOT NULL)`} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	return &Driver{clock: clock, db: db, correlation: correlation, cancellable: cancellable}, nil
}
func (d *Driver) Close() error { return d.db.Close() }
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
	_, e := d.StartAsync(ctx, c)
	return e
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	f, e := d.InspectAsync(ctx, c, execution.Association{})
	return f.Observation, e
}
func (d *Driver) StartAsync(ctx context.Context, c execution.Call) (execution.Fact, error) {
	var in struct {
		Delta int64 `json:"delta"`
	}
	if json.Unmarshal(c.Input, &in) != nil || in.Delta < 1 || in.Delta > 10 {
		return execution.Fact{}, fmt.Errorf("input")
	}
	handle, e := randomid.New()
	if e != nil {
		return execution.Fact{}, e
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return execution.Fact{}, e
	}
	defer tx.Rollback()
	var digest, oldHandle string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint,handle FROM jobs WHERE id=?`, c.Request.OperationID).Scan(&digest, &oldHandle)
	if e == nil {
		if digest != c.Request.Fingerprint() {
			return execution.Fact{}, fmt.Errorf("identity conflict")
		}
		handle = oldHandle
	} else if e == sql.ErrNoRows {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&count); e != nil {
			return execution.Fact{}, e
		}
		if count >= 64 {
			return execution.Fact{}, fmt.Errorf("capacity")
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO jobs VALUES(?,?,?,?,?,'running',1,0,1,0)`, c.Request.OperationID, c.Request.Fingerprint(), handle, in.Delta, c.Request.ResourceVersion)
		if e != nil {
			return execution.Fact{}, e
		}
	} else {
		return execution.Fact{}, e
	}
	if e = tx.Commit(); e != nil {
		return execution.Fact{}, e
	}
	return d.InspectAsync(ctx, c, execution.Association{Target: "counter-jobs", OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint(), Handle: handle})
}
func (d *Driver) InspectAsync(ctx context.Context, c execution.Call, a execution.Association) (execution.Fact, error) {
	f := execution.Fact{Source: "job-driver", Version: 1, Association: execution.Association{Target: "counter-jobs", OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint()}, Observation: execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}}
	if a.Handle == "" {
		var count int
		if e := d.db.QueryRowContext(ctx, `SELECT count(*) FROM expired_lookup WHERE id=?`, c.Request.OperationID).Scan(&count); e != nil {
			return f, e
		}
		if count > 0 {
			return f, nil
		}
	}
	if a.Handle == "" && !d.correlation {
		return f, nil
	}
	query := `SELECT fingerprint,handle,state,revision,value,version,completed_at FROM jobs WHERE id=?`
	arg := c.Request.OperationID
	if a.Handle != "" {
		if a.Target != "counter-jobs" || a.OperationID != c.Request.OperationID || a.Fingerprint != c.Request.Fingerprint() {
			return f, fmt.Errorf("association")
		}
		query = `SELECT fingerprint,handle,state,revision,value,version,completed_at FROM jobs WHERE id=? AND handle=?`
	}
	var digest, handle, state string
	var revision, version uint64
	var value int64
	var row *sql.Row
	if a.Handle != "" {
		row = d.db.QueryRowContext(ctx, query, arg, a.Handle)
	} else {
		row = d.db.QueryRowContext(ctx, query, arg)
	}
	e := row.Scan(&digest, &handle, &state, &revision, &value, &version, &f.CompletedAt)
	if e == sql.ErrNoRows {
		return f, nil
	}
	if e != nil {
		return f, e
	}
	if digest != c.Request.Fingerprint() {
		return f, fmt.Errorf("identity")
	}
	f.Version = revision
	f.Association.Handle = handle
	evidence, _ := json.Marshal(struct {
		Operation, State string
		Revision         uint64
	}{c.Request.OperationID, state, revision})
	f.Observation.Evidence = evidence
	output, _ := json.Marshal(struct {
		Value   int64  `json:"value"`
		Version uint64 `json:"version"`
	}{value, version})
	switch state {
	case "running":
		f.Observation.Phase = "IN_PROGRESS"
	case "completed":
		f.Observation = execution.Observation{Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Output: output, Evidence: evidence}
	case "cancelled", "rejected":
		f.Observation = execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Output: output, Evidence: evidence}
	}
	return f, nil
}

// Complete is the independent target's job runner, never called by Inspect.
func (d *Driver) Complete(ctx context.Context, op string) error {
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var state string
	var delta int64
	var expected uint64
	if e = tx.QueryRowContext(ctx, `SELECT state,delta,expected FROM jobs WHERE id=?`, op).Scan(&state, &delta, &expected); e != nil {
		return e
	}
	if state != "running" {
		return nil
	}
	var value int64
	var version uint64
	if e = tx.QueryRowContext(ctx, `SELECT value,version FROM resource WHERE id=1`).Scan(&value, &version); e != nil {
		return e
	}
	state = "rejected"
	if expected == version {
		value += delta
		version++
		state = "completed"
		if _, e = tx.ExecContext(ctx, `UPDATE resource SET value=?,version=? WHERE id=1`, value, version); e != nil {
			return e
		}
	}
	now, e := d.clock.Now()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE jobs SET state=?,revision=revision+1,value=?,version=?,completed_at=? WHERE id=?`, state, value, version, now.UnixNano(), op); e != nil {
		return e
	}
	return tx.Commit()
}
func (d *Driver) Snapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	e := d.db.QueryRowContext(ctx, `SELECT value,version,(SELECT count(*) FROM jobs WHERE state='completed'),(SELECT count(*) FROM jobs) FROM resource WHERE id=1`).Scan(&s.Value, &s.Version, &s.Changes, &s.Jobs)
	return s, e
}
func (d *Driver) Cancel(ctx context.Context, c execution.Call, a execution.Association, op string) error {
	if !d.cancellable {
		return fmt.Errorf("unsupported")
	}
	if op == "" {
		return fmt.Errorf("cancel identity")
	}
	f, e := d.InspectAsync(ctx, c, a)
	if e != nil {
		return e
	}
	if f.Association.Handle == "" {
		return fmt.Errorf("unknown association")
	}
	_, e = d.db.ExecContext(ctx, `UPDATE jobs SET state='cancelled',revision=revision+1 WHERE id=? AND handle=? AND state='running'`, c.Request.OperationID, f.Association.Handle)
	return e
}

// ExpireLookup simulates the end of a target's original-key lookup guarantee.
// Job history remains independent truth; key expiry never authorizes a new job.
func (d *Driver) ExpireLookup(ctx context.Context, op string) error {
	_, e := d.db.ExecContext(ctx, `INSERT OR IGNORE INTO expired_lookup VALUES(?)`, op)
	return e
}

// Fence proves negative coverage by durably preventing any late original Start.
func (d *Driver) Fence(ctx context.Context, c execution.Call) error {
	handle, e := randomid.New()
	if e != nil {
		return e
	}
	_, e = d.db.ExecContext(ctx, `INSERT INTO jobs VALUES(?,?,?,0,1,'rejected',1,0,1,0)`, c.Request.OperationID, c.Request.Fingerprint(), handle)
	return e
}
