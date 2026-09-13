package simworkflow

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
	"strings"
)

type Driver struct {
	db  *sql.DB
	def Definition
}
type Snapshot struct {
	State   string `json:"state"`
	Ledger  int64  `json:"ledger"`
	Version uint64 `json:"version"`
}

func Open(path string, def Definition) (*Driver, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, fmt.Errorf("private target required")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := &url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_txlock", "immediate")
	q.Add("_pragma", "busy_timeout(1000)")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `CREATE TABLE IF NOT EXISTS fence(id INTEGER PRIMARY KEY CHECK(id=1),control_version INTEGER,business_version INTEGER,intent TEXT,operation TEXT,expected INTEGER)`, `INSERT OR IGNORE INTO fence VALUES(1,1,1,'RESUME','',0)`, `CREATE TABLE IF NOT EXISTS business(id INTEGER PRIMARY KEY CHECK(id=1),definition BLOB NOT NULL,ledger INTEGER NOT NULL)`, `CREATE TABLE IF NOT EXISTS records(id TEXT PRIMARY KEY,quantity INTEGER,limit_value INTEGER,verified INTEGER,state TEXT,version INTEGER)`, `CREATE TABLE IF NOT EXISTS operations(id TEXT PRIMARY KEY,fingerprint TEXT,occurred INTEGER,output BLOB)`} {
		if _, e = db.Exec(stmt); e != nil {
			db.Close()
			return nil, e
		}
	}
	raw, _ := json.Marshal(def)
	if _, e = db.Exec(`INSERT OR IGNORE INTO business VALUES(1,?,1000)`, raw); e != nil {
		db.Close()
		return nil, e
	}
	var old []byte
	if e = db.QueryRow(`SELECT definition FROM business`).Scan(&old); e != nil || string(old) != string(raw) {
		db.Close()
		return nil, fmt.Errorf("business binding mismatch")
	}
	return &Driver{db, def}, nil
}
func (d *Driver) Close() error { return d.db.Close() }
func (d *Driver) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	var s Snapshot
	e := d.db.QueryRowContext(ctx, `SELECT state,(SELECT ledger FROM business),version FROM records WHERE id=?`, id).Scan(&s.State, &s.Ledger, &s.Version)
	return s, e
}

// Seed creates bounded independent business fixtures once. It never replaces an
// existing record or resets a ledger or operation history.
func (d *Driver) Seed(ctx context.Context, id, state string, quantity, limit int64, verified bool) error {
	if state != "draft" && state != "submitted" && state != "authorized" && state != "rejected" {
		return fmt.Errorf("seed state")
	}
	if quantity < 1 || quantity > limit || limit > 1000 || len(id) > 64 {
		return fmt.Errorf("seed values")
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&n); e != nil {
		return e
	}
	if n >= 64 {
		return fmt.Errorf("record capacity")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO records VALUES(?,?,?,?,?,1)`, id, quantity, limit, verified, state); e != nil {
		return e
	}
	return tx.Commit()
}
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
	if len(c.Input) > 32768 || !strings.HasPrefix(c.Request.Capability, d.def.Kind+".") {
		return fmt.Errorf("target binding")
	}
	action := strings.TrimPrefix(c.Request.Capability, d.def.Kind+".")
	var input map[string]json.RawMessage
	if e := json.Unmarshal(c.Input, &input); e != nil {
		return e
	}
	var id string
	if json.Unmarshal(input["record"], &id) != nil || len(id) < 1 || len(id) > 64 {
		return fmt.Errorf("record")
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var fingerprint string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint FROM operations WHERE id=?`, c.Request.OperationID).Scan(&fingerprint)
	if e == nil {
		if fingerprint != c.Request.Fingerprint() {
			return fmt.Errorf("identity conflict")
		}
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM operations`).Scan(&n); e != nil {
		return e
	}
	if n >= 64 {
		return fmt.Errorf("operation capacity")
	}
	var quantity, limit, ledger int64
	var verified bool
	state := ""
	version := uint64(1)
	e = tx.QueryRowContext(ctx, `SELECT quantity,limit_value,verified,state,version FROM records WHERE id=?`, id).Scan(&quantity, &limit, &verified, &state, &version)
	absent := e == sql.ErrNoRows
	if e != nil && !absent {
		return e
	}
	if e = tx.QueryRowContext(ctx, `SELECT ledger FROM business`).Scan(&ledger); e != nil {
		return e
	}
	next := state
	newLedger := ledger
	var control uint64
	var intent string
	if e = tx.QueryRowContext(ctx, `SELECT control_version,intent FROM fence WHERE id=1`).Scan(&control, &intent); e != nil {
		return e
	}
	valid := version == c.Request.ResourceVersion && c.Control != nil && c.Control.Resource == d.Resource() && c.Control.Authority == Authority && c.Control.Version == control && intent == "RESUME"
	switch action {
	case "draft":
		if json.Unmarshal(input[d.def.Quantity], &quantity) != nil || json.Unmarshal(input[d.def.Limit], &limit) != nil || json.Unmarshal(input[d.def.Verified], &verified) != nil {
			return fmt.Errorf("business fields")
		}
		valid = valid && absent && quantity > 0 && quantity <= 1000 && limit > 0 && limit <= 1000
		next = "draft"
	case "submit":
		valid = valid && !absent && state == "draft" && verified && quantity <= limit
		next = "submitted"
	case "authorize":
		valid = valid && !absent && state == "submitted"
		next = "authorized"
		if d.def.Mode == "reserve" {
			newLedger -= quantity
		} else {
			newLedger += quantity
		}
	case "reject":
		valid = valid && !absent && state == "submitted"
		next = "rejected"
	case "withdraw":
		valid = valid && !absent && state == "authorized"
		next = "withdrawn"
		if d.def.Mode == "reserve" {
			newLedger += quantity
		} else {
			newLedger -= quantity
		}
	case "archive":
		valid = valid && !absent && (state == "rejected" || state == "withdrawn")
		next = "archived"
	default:
		return fmt.Errorf("unsupported action")
	}
	valid = valid && newLedger >= 0 && newLedger <= 2000
	occurred := 0
	if valid {
		if absent {
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&n); e != nil {
				return e
			}
			if n >= 64 {
				return fmt.Errorf("record capacity")
			}
		}
		occurred = 1
		if _, e = tx.ExecContext(ctx, `UPDATE fence SET business_version=business_version+1 WHERE id=1`); e != nil {
			return e
		}
		version++
		state = next
		ledger = newLedger
		if _, e = tx.ExecContext(ctx, `INSERT INTO records VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,version=excluded.version`, id, quantity, limit, verified, state, version); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE business SET ledger=?`, ledger); e != nil {
			return e
		}
	}
	out, _ := json.Marshal(Snapshot{state, ledger, version})
	if _, e = tx.ExecContext(ctx, `INSERT INTO operations VALUES(?,?,?,?)`, c.Request.OperationID, c.Request.Fingerprint(), occurred, out); e != nil {
		return e
	}
	return tx.Commit()
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	var fp string
	var occurred int
	var out []byte
	e := d.db.QueryRowContext(ctx, `SELECT fingerprint,occurred,output FROM operations WHERE id=?`, c.Request.OperationID).Scan(&fp, &occurred, &out)
	if e == sql.ErrNoRows {
		return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
	}
	if e != nil {
		return execution.Observation{}, e
	}
	if fp != c.Request.Fingerprint() {
		return execution.Observation{}, fmt.Errorf("identity conflict")
	}
	evidence, _ := json.Marshal(struct {
		Operation, Fingerprint string
		Occurred               bool
	}{c.Request.OperationID, fp, occurred == 1})
	if occurred == 0 {
		return execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Evidence: evidence}, nil
	}
	return execution.Observation{Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Output: out, Evidence: evidence}, nil
}
