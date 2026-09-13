// Package sqlitecontext stores private, immutable decision context snapshots.
package sqlitecontext

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"lerna/contextassembly"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const MaxSnapshots = 512
const MaxDocument = 65536
const Timeout = 2 * time.Second

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, contextassembly.Invalid
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, contextassembly.Invalid
	}
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return nil, contextassembly.Invalid
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, contextassembly.Unavailable
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, contextassembly.Unavailable
	}
	if f.Close() != nil {
		return nil, contextassembly.Unavailable
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": []string{"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, contextassembly.Unavailable
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS context_lock(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);
 INSERT OR IGNORE INTO context_lock VALUES(1,1);
 CREATE TABLE IF NOT EXISTS context_snapshots(namespace TEXT NOT NULL,task_id TEXT NOT NULL,decision INTEGER NOT NULL CHECK(decision>0),subject TEXT NOT NULL,semantic TEXT NOT NULL,document BLOB NOT NULL CHECK(length(document)<=65536),PRIMARY KEY(namespace,task_id,decision));
 CREATE TABLE IF NOT EXISTS context_checkpoints(namespace TEXT NOT NULL,task_id TEXT NOT NULL,decision INTEGER NOT NULL,digest BLOB NOT NULL CHECK(length(digest)=32),PRIMARY KEY(namespace,task_id,decision));
 CREATE TABLE IF NOT EXISTS context_retired(namespace TEXT NOT NULL,task_id TEXT NOT NULL,decision INTEGER NOT NULL,PRIMARY KEY(namespace,task_id,decision));
 CREATE TABLE IF NOT EXISTS context_revision_invalidations(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(namespace,collection,record_key,revision));
 CREATE TABLE IF NOT EXISTS context_source_invalidations(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,missing INTEGER NOT NULL CHECK(missing IN (0,1)),revision TEXT NOT NULL,PRIMARY KEY(namespace,collection,record_key,missing));`)
	if err != nil {
		db.Close()
		return nil, contextassembly.Unavailable
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func label(v string) bool     { return len(v) > 0 && len(v) <= 256 }
func validKey(k contextassembly.Key) bool {
	return label(k.Namespace) && label(k.TaskID) && k.Decision > 0 && k.Decision <= 1<<32
}
func valid(s contextassembly.Snapshot) bool {
	hash, e := hex.DecodeString(s.SemanticSHA256)
	return validKey(s.Key) && label(s.Subject) && e == nil && len(hash) == 32 && hex.EncodeToString(hash) == s.SemanticSHA256 && len(s.Document) <= MaxDocument && json.Valid(s.Document)
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func read(ctx context.Context, q querier, k contextassembly.Key) (contextassembly.Snapshot, error) {
	out := contextassembly.Snapshot{Key: k}
	var retired bool
	e := q.QueryRowContext(ctx, `WITH target(namespace,task_id,decision) AS (VALUES(?,?,?)) SELECT COALESCE(s.subject,''),COALESCE(s.semantic,''),COALESCE(s.document,'{}'),r.decision IS NOT NULL FROM target LEFT JOIN context_snapshots s USING(namespace,task_id,decision) LEFT JOIN context_retired r USING(namespace,task_id,decision) WHERE s.decision IS NOT NULL OR r.decision IS NOT NULL`, k.Namespace, k.TaskID, k.Decision).Scan(&out.Subject, &out.SemanticSHA256, &out.Document, &retired)
	if e == sql.ErrNoRows {
		return contextassembly.Snapshot{}, contextassembly.Missing
	}
	if e == nil && retired {
		return contextassembly.Snapshot{}, contextassembly.Invalidated
	}
	if e != nil || !valid(out) {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	return out, nil
}
func (s *Store) Read(ctx context.Context, k contextassembly.Key) (contextassembly.Snapshot, error) {
	if !validKey(k) {
		return contextassembly.Snapshot{}, contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return read(ctx, s.db, k)
}
func (s *Store) Bind(ctx context.Context, in contextassembly.Snapshot) (contextassembly.Snapshot, error) {
	// Own the candidate bytes before entering an asynchronous database operation.
	in.Document = append([]byte(nil), in.Document...)
	if !valid(in) {
		return contextassembly.Snapshot{}, contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE context_lock SET version=version WHERE id=1`); e != nil {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	original, e := read(ctx, tx, in.Key)
	if e == nil {
		if original.Subject != in.Subject || original.SemanticSHA256 != in.SemanticSHA256 {
			return contextassembly.Snapshot{}, contextassembly.IdentityConflict
		}
		return original, nil
	}
	if e != contextassembly.Missing {
		return contextassembly.Snapshot{}, e
	}
	if e = checkWatermarks(ctx, tx, in); e != nil {
		return contextassembly.Snapshot{}, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, identityCount).Scan(&count); e != nil {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	if count >= MaxSnapshots {
		return contextassembly.Snapshot{}, contextassembly.Capacity
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO context_snapshots VALUES(?,?,?,?,?,?)`, in.Key.Namespace, in.Key.TaskID, in.Key.Decision, in.Subject, in.SemanticSHA256, in.Document); e != nil {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	if e = tx.Commit(); e != nil {
		return contextassembly.Snapshot{}, contextassembly.Unavailable
	}
	return in, nil
}
