// Package sqlitememory implements the private, bounded SQLite memory Store.
package sqlitememory

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/sys/unix"
	"lerna/memory"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxRevisions = 512
const MaxDocument = 16384
const Timeout = 2 * time.Second

type Store struct {
	db        *sql.DB
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

func Open(path string) (*Store, error) {
	return openStore(path, false)
}

func openStore(path string, restoring bool) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, memory.Invalid
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, memory.Invalid
	}
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return nil, memory.Invalid
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, memory.Unavailable
	}
	if restoring && os.IsNotExist(err) {
		return nil, memory.Missing
	}
	flags := os.O_RDWR | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if !restoring {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, memory.Unavailable
	}
	owned := true
	defer func() {
		if owned {
			f.Close()
		}
	}()
	if !privateDatabase(f) {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	if err = lockDatabase(ctx, f, restoring); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": []string{"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, memory.Unavailable
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS memory_lock(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);
 INSERT OR IGNORE INTO memory_lock VALUES(1,1);
 CREATE TABLE IF NOT EXISTS memory_recovery(id INTEGER PRIMARY KEY CHECK(id=1),quarantined INTEGER NOT NULL CHECK(quarantined IN (0,1)));
 INSERT OR IGNORE INTO memory_recovery VALUES(1,0);
 CREATE TABLE IF NOT EXISTS memory_recovery_scopes(namespace TEXT NOT NULL,collection TEXT NOT NULL,config TEXT NOT NULL,authority TEXT NOT NULL,epoch TEXT NOT NULL,origin INTEGER NOT NULL,verified INTEGER NOT NULL,applied INTEGER NOT NULL,retained INTEGER NOT NULL,missing INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(namespace,collection));
 CREATE TABLE IF NOT EXISTS memory_recovery_erased(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(namespace,collection,record_key,revision));
 CREATE TABLE IF NOT EXISTS memory_recovery_deleted(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(namespace,collection,record_key));
 CREATE TABLE IF NOT EXISTS memory_consumers(namespace TEXT NOT NULL,collection TEXT NOT NULL,consumer TEXT NOT NULL,config TEXT NOT NULL,position INTEGER NOT NULL CHECK(position>=0),PRIMARY KEY(namespace,collection,consumer));
 CREATE TABLE IF NOT EXISTS memory_source_fences(namespace TEXT NOT NULL,kind TEXT NOT NULL,source_key TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(namespace,kind,source_key));
 CREATE TABLE IF NOT EXISTS memory_erasure_events(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(namespace,collection,record_key,revision),UNIQUE(namespace,collection,position));
 CREATE TABLE IF NOT EXISTS memory_tombstones(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),operation TEXT NOT NULL,PRIMARY KEY(namespace,collection,record_key));
 CREATE TABLE IF NOT EXISTS memory_revisions(namespace TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,collection,record_key,revision));
 CREATE TABLE IF NOT EXISTS memory_disclosures(namespace TEXT NOT NULL,read_id TEXT NOT NULL,document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,read_id));
 CREATE TABLE IF NOT EXISTS memory_operations(namespace TEXT NOT NULL,operation TEXT NOT NULL,subject TEXT NOT NULL,semantic TEXT NOT NULL,collection TEXT NOT NULL,record_key TEXT NOT NULL,revision INTEGER NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(namespace,operation),UNIQUE(namespace,collection,position));`)
	if err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if restoring {
		if _, err = db.ExecContext(ctx, `UPDATE memory_recovery SET quarantined=1 WHERE id=1`); err != nil {
			db.Close()
			return nil, memory.Unavailable
		}
	} else {
		var quarantined bool
		if err = db.QueryRowContext(ctx, `SELECT quarantined FROM memory_recovery WHERE id=1`).Scan(&quarantined); err != nil {
			db.Close()
			return nil, memory.Unavailable
		}
		if quarantined {
			db.Close()
			return nil, memory.Quarantined
		}
	}
	owned = false
	return &Store{db: db, file: f}, nil
}
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.db.Close()
		closed := s.file.Close()
		if s.closeErr == nil {
			s.closeErr = closed
		}
	})
	return s.closeErr
}
func label(v string) bool        { return len(v) > 0 && len(v) <= 256 }
func validRef(r memory.Ref) bool { return label(r.Namespace) && label(r.Collection) && label(r.Key) }
func (s *Store) Read(ctx context.Context, ref memory.Ref, revision uint64) (memory.Revision, error) {
	if !validRef(ref) || revision == 0 || revision > 1<<32 {
		return memory.Revision{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out := memory.Revision{Ref: ref, Revision: revision}
	err := s.db.QueryRowContext(ctx, `SELECT document FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, ref.Namespace, ref.Collection, ref.Key, revision).Scan(&out.Document)
	if err == sql.ErrNoRows {
		return memory.Revision{}, memory.Missing
	}
	if err != nil || len(out.Document) > MaxDocument || !json.Valid(out.Document) {
		return memory.Revision{}, memory.Unavailable
	}
	return out, nil
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func lookup(ctx context.Context, q querier, namespace, op string) (memory.Receipt, error) {
	out := memory.Receipt{OperationID: op}
	out.Ref.Namespace = namespace
	err := q.QueryRowContext(ctx, `SELECT subject,semantic,collection,record_key,revision,position FROM memory_operations WHERE namespace=? AND operation=?`, namespace, op).Scan(&out.Subject, &out.SemanticSHA256, &out.Ref.Collection, &out.Ref.Key, &out.Revision, &out.Position)
	if err == sql.ErrNoRows {
		return memory.Receipt{}, memory.Missing
	}
	if err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	return out, nil
}
func (s *Store) LookupOperation(ctx context.Context, namespace, op string) (memory.Receipt, error) {
	if !label(namespace) || !label(op) {
		return memory.Receipt{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return lookup(ctx, s.db, namespace, op)
}
func (s *Store) Commit(ctx context.Context, in memory.Change) (memory.Receipt, error) {
	r := in.Record
	hash, e := hex.DecodeString(in.SemanticSHA256)
	if !validRef(r.Ref) || !label(in.OperationID) || !label(in.Subject) || e != nil || len(hash) != 32 || hex.EncodeToString(hash) != in.SemanticSHA256 || in.Expected >= 1<<32 || r.Revision != in.Expected+1 || len(r.Document) > MaxDocument || !json.Valid(r.Document) {
		return memory.Receipt{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	defer tx.Rollback()
	// Acquire the writer lock before reading the expected revision. All writers
	// compete at the same SQLite boundary instead of upgrading a stale snapshot.
	if _, err = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	old, err := lookup(ctx, tx, r.Ref.Namespace, in.OperationID)
	if err == nil {
		if old.Subject == in.Subject && old.Ref == r.Ref && old.SemanticSHA256 == "" {
			return memory.Receipt{}, memory.ReplayUnavailable
		}
		if old.Subject != in.Subject || old.SemanticSHA256 != in.SemanticSHA256 || old.Ref != r.Ref || old.Revision != r.Revision {
			return memory.Receipt{}, memory.IdentityConflict
		}
		var body []byte
		if err = tx.QueryRowContext(ctx, `SELECT document FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Key, r.Revision).Scan(&body); err != nil {
			return memory.Receipt{}, memory.Unavailable
		}
		if string(body) != string(r.Document) {
			return memory.Receipt{}, memory.IdentityConflict
		}
		return old, nil
	}
	if err != memory.Missing {
		return memory.Receipt{}, err
	}
	if err = sourceAllowed(ctx, tx, r.Ref.Namespace, r.Document); err != nil {
		return memory.Receipt{}, err
	}
	var deleted int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_tombstones WHERE namespace=? AND collection=? AND record_key=?`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Key).Scan(&deleted); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if deleted != 0 {
		return memory.Receipt{}, memory.Conflict
	}
	var latest, count uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM memory_operations WHERE namespace=? AND collection=? AND record_key=?`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Key).Scan(&latest); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if latest != in.Expected {
		return memory.Receipt{}, memory.Conflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM memory_revisions)+(SELECT COUNT(*) FROM memory_tombstones)+(SELECT COUNT(*) FROM memory_erasure_events)`).Scan(&count); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if count >= MaxRevisions {
		return memory.Receipt{}, memory.Capacity
	}
	var position uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=?)`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Namespace, r.Ref.Collection).Scan(&position); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_revisions VALUES(?,?,?,?,?)`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Key, r.Revision, r.Document); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_operations VALUES(?,?,?,?,?,?,?,?)`, r.Ref.Namespace, in.OperationID, in.Subject, in.SemanticSHA256, r.Ref.Collection, r.Ref.Key, r.Revision, position); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	return memory.Receipt{OperationID: in.OperationID, Subject: in.Subject, SemanticSHA256: in.SemanticSHA256, Ref: r.Ref, Revision: r.Revision, Position: position}, nil
}
func (s *Store) ReadChanges(ctx context.Context, namespace, collection string, after uint64, limit int) ([]memory.Receipt, error) {
	if !label(namespace) || !label(collection) || after > 1<<32 || limit < 1 || limit > MaxRevisions {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT operation,subject,semantic,record_key,revision,position FROM memory_operations WHERE namespace=? AND collection=? AND position>? ORDER BY position LIMIT ?`, namespace, collection, after, limit)
	if err != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	out := []memory.Receipt{}
	for rows.Next() {
		r := memory.Receipt{}
		r.Ref.Namespace = namespace
		r.Ref.Collection = collection
		if rows.Scan(&r.OperationID, &r.Subject, &r.SemanticSHA256, &r.Ref.Key, &r.Revision, &r.Position) != nil {
			return nil, memory.Unavailable
		}
		out = append(out, r)
	}
	if rows.Err() != nil {
		return nil, memory.Unavailable
	}
	return out, nil
}

// Head is internal metadata; the Memory service authorizes the dependency first.
func (s *Store) Head(ctx context.Context, ref memory.Ref) (uint64, error) {
	if !validRef(ref) {
		return 0, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var revision uint64
	e := s.db.QueryRowContext(ctx, `SELECT revision FROM memory_operations WHERE namespace=? AND collection=? AND record_key=? ORDER BY revision DESC LIMIT 1`, ref.Namespace, ref.Collection, ref.Key).Scan(&revision)
	if e == sql.ErrNoRows {
		return 0, memory.Missing
	}
	if e != nil || revision == 0 || revision > 1<<32 {
		return 0, memory.Unavailable
	}
	return revision, nil
}
