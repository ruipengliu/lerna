// Package simresource is an independent durable asynchronous counter target.
package simresource

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
	clock      Clock
	db         *sql.DB
	entry, key string
}
type Snapshot struct {
	Value          int64
	Version        uint64
	Changes, Jobs  int
	ControlVersion uint64
	Intent         string
}

func Open(path string, entry, key string, clock Clock) (*Driver, error) {
	if (entry != "add" && entry != "scale") || key == "" || len(key) > 128 || clock == nil {
		return nil, fmt.Errorf("target binding")
	}
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
	for _, q := range []string{`PRAGMA busy_timeout=5000`, `PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE IF NOT EXISTS resource(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL,version INTEGER NOT NULL,control_version INTEGER NOT NULL,intent TEXT NOT NULL,control_op TEXT NOT NULL,control_expected INTEGER NOT NULL)`, `INSERT OR IGNORE INTO resource VALUES(1,0,1,1,'RESUME','',1)`, `CREATE TABLE IF NOT EXISTS target_binding(id INTEGER PRIMARY KEY CHECK(id=1),resource_key TEXT NOT NULL)`, `CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, handle TEXT UNIQUE NOT NULL, delta INTEGER NOT NULL, expected INTEGER NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL, value INTEGER NOT NULL, version INTEGER NOT NULL,completed_at INTEGER NOT NULL,control_version INTEGER NOT NULL,entry TEXT NOT NULL)`} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	if _, e = db.Exec(`INSERT OR IGNORE INTO target_binding VALUES(1,?)`, key); e != nil {
		db.Close()
		return nil, e
	}
	var stored string
	if e = db.QueryRow(`SELECT resource_key FROM target_binding WHERE id=1`).Scan(&stored); e != nil || stored != key {
		db.Close()
		return nil, fmt.Errorf("target identity mismatch")
	}
	return &Driver{clock: clock, db: db, entry: entry, key: key}, nil
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
		var control uint64
		var intent string
		if e = tx.QueryRowContext(ctx, `SELECT control_version,intent FROM resource WHERE id=1`).Scan(&control, &intent); e != nil {
			return execution.Fact{}, e
		}
		if c.Control == nil || c.Control.Resource != d.ref() || c.Control.Authority != authority || c.Control.Version != c.Request.ControlVersion || c.Control.Version != control || intent != "RESUME" {
			return execution.Fact{}, fmt.Errorf("resource fenced")
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&count); e != nil {
			return execution.Fact{}, e
		}
		if count >= 64 {
			return execution.Fact{}, fmt.Errorf("capacity")
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO jobs VALUES(?,?,?,?,?,'running',1,0,1,0,?,?)`, c.Request.OperationID, c.Request.Fingerprint(), handle, in.Delta, c.Request.ResourceVersion, c.Control.Version, d.entry)
		if e != nil {
			return execution.Fact{}, e
		}
	} else {
		return execution.Fact{}, e
	}
	if e = tx.Commit(); e != nil {
		return execution.Fact{}, e
	}
	return d.InspectAsync(ctx, c, execution.Association{Target: "counter-jobs/" + d.key, OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint(), Handle: handle})
}
func (d *Driver) InspectAsync(ctx context.Context, c execution.Call, a execution.Association) (execution.Fact, error) {
	tx, e := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return execution.Fact{}, e
	}
	defer tx.Rollback()
	return d.inspectSnapshot(ctx, tx, c, a)
}

// Job absence and the fence must come from one snapshot: a job could have
// completed between independent reads before the newer fence was applied.
func (d *Driver) inspectSnapshot(ctx context.Context, tx *sql.Tx, c execution.Call, a execution.Association) (execution.Fact, error) {
	f := execution.Fact{Source: "job-driver", Version: 1, Association: execution.Association{Target: "counter-jobs/" + d.key, OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint()}, Observation: execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}}

	query := `SELECT fingerprint,handle,state,revision,value,version,completed_at FROM jobs WHERE id=?`
	arg := c.Request.OperationID
	if a.Handle != "" {
		if a.Target != "counter-jobs/"+d.key || a.OperationID != c.Request.OperationID || a.Fingerprint != c.Request.Fingerprint() {
			return f, fmt.Errorf("association")
		}
	}
	var digest, handle, state string
	var revision, version uint64
	var value int64
	row := tx.QueryRowContext(ctx, query, arg)
	e := row.Scan(&digest, &handle, &state, &revision, &value, &version, &f.CompletedAt)
	if e == sql.ErrNoRows {
		var control uint64
		err := tx.QueryRowContext(ctx, `SELECT control_version FROM resource WHERE id=1`).Scan(&control)
		if err != nil {
			return f, err
		}
		// A higher durable target fence prevents this original request arriving later.
		if c.Request.ControlVersion > 0 && c.Request.ControlVersion < control {
			f.Observation = execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Evidence: []byte(`{"proof":"target-control-fence"}`)}
		}
		return f, nil
	}
	if e != nil {
		return f, e
	}
	if digest != c.Request.Fingerprint() || a.Handle != "" && a.Handle != handle {
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
	var state, entry string
	var jobControl uint64
	var delta int64
	var expected uint64
	if e = tx.QueryRowContext(ctx, `SELECT state,delta,expected,control_version,entry FROM jobs WHERE id=?`, op).Scan(&state, &delta, &expected, &jobControl, &entry); e != nil {
		return e
	}
	if state != "running" {
		return nil
	}
	var value int64
	var version, control uint64
	var intent string
	if e = tx.QueryRowContext(ctx, `SELECT value,version,control_version,intent FROM resource WHERE id=1`).Scan(&value, &version, &control, &intent); e != nil {
		return e
	}
	state = "rejected"
	if expected == version && jobControl == control && intent == "RESUME" {
		if entry == "add" {
			value += delta
		} else {
			value *= delta
		}
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
	e := d.db.QueryRowContext(ctx, `SELECT value,version,(SELECT count(*) FROM jobs WHERE state='completed'),(SELECT count(*) FROM jobs),control_version,intent FROM resource WHERE id=1`).Scan(&s.Value, &s.Version, &s.Changes, &s.Jobs, &s.ControlVersion, &s.Intent)
	return s, e
}
func (d *Driver) Cancel(ctx context.Context, c execution.Call, a execution.Association, op string) error {

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
