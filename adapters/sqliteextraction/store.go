// Package sqliteextraction persists bounded, private extraction candidates.
package sqliteextraction

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"lerna/adapters/internal/sqliteopen"
	"lerna/extraction"
	"lerna/memory"
	"sync"
	"time"
)

const timeout = 2 * time.Second
const maxRecords = 512
const maxDocument = 16384

type Store struct {
	db        *sql.DB
	closeOnce sync.Once
	closeErr  error
}

var _ extraction.CandidateStore = (*Store)(nil)

func Open(path string) (*Store, error) {
	db, err := sqliteopen.Open(path, time.Second, memory.Invalid, memory.Unavailable)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS candidate_lock(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);INSERT OR IGNORE INTO candidate_lock VALUES(1,1);CREATE TABLE IF NOT EXISTS candidates(namespace TEXT NOT NULL,operation TEXT NOT NULL,document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,operation));CREATE TABLE IF NOT EXISTS retired_candidates(namespace TEXT NOT NULL,operation TEXT NOT NULL,subject TEXT NOT NULL,committed INTEGER NOT NULL CHECK(committed IN (0,1)),PRIMARY KEY(namespace,operation));`)
	if err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if err = migrateInvocation(ctx, db); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS candidate_saves(namespace TEXT NOT NULL,candidate TEXT NOT NULL,subject TEXT NOT NULL,operation TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('pending','retired')),identity BLOB NOT NULL,document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,candidate),UNIQUE(namespace,operation));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS candidate_source_fences(namespace TEXT NOT NULL,kind TEXT NOT NULL,source_key TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(namespace,kind,source_key));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS candidate_cleanup(namespace TEXT NOT NULL,candidate TEXT NOT NULL,subject TEXT NOT NULL,operation TEXT NOT NULL,document BLOB NOT NULL CHECK(length(document)<=4096),PRIMARY KEY(namespace,candidate),UNIQUE(namespace,operation));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cleanup_progress(namespace TEXT NOT NULL,subject TEXT NOT NULL,consumer TEXT NOT NULL,config TEXT NOT NULL,version INTEGER NOT NULL,after_candidate TEXT NOT NULL,PRIMARY KEY(namespace,subject,consumer));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS extraction_triggers(namespace TEXT NOT NULL,trigger_id TEXT NOT NULL,subject TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('active','cancelled')),document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,trigger_id));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS trigger_rounds(namespace TEXT NOT NULL,trigger_id TEXT NOT NULL,event_key TEXT NOT NULL,subject TEXT NOT NULL,submit_operation TEXT NOT NULL,document BLOB NOT NULL CHECK(length(document)<=16384),PRIMARY KEY(namespace,trigger_id,event_key),UNIQUE(namespace,submit_operation));`); err != nil {
		db.Close()
		return nil, memory.Unavailable
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.db.Close() })
	return s.closeErr
}
func name(s string) bool { return len(s) > 0 && len(s) <= 256 }
func encode(r extraction.CandidateRecord) ([]byte, error) {
	if r.InvocationSHA256 != "" {
		raw, err := hex.DecodeString(r.InvocationSHA256)
		if err != nil || len(raw) != 32 {
			return nil, memory.Invalid
		}
	}
	if !name(r.Namespace) || !name(r.Subject) || !name(r.OperationID) || r.Candidate.About == "" || len(r.Candidate.About) > 512 {
		return nil, memory.Invalid
	}
	c := r.Candidate
	if c.Kind != "preference" && c.Kind != "inference" && c.Kind != "fact" && c.Kind != "experience" {
		return nil, memory.Invalid
	}
	if !name(c.Attribute) || c.Value == "" || len(c.Value) > 4096 || c.Conditions == "" || len(c.Conditions) > 1024 || c.Confidence == nil || !name(c.Confidence.Assessment) || !name(c.Confidence.Method) || c.Confidence.Basis == "" || len(c.Confidence.Basis) > 1024 || len(c.Sources) == 0 || len(c.Sources) > 16 {
		return nil, memory.Invalid
	}
	for _, source := range c.Sources {
		if source == nil || source.Ref == nil || !name(source.Ref.Kind) || !name(source.Ref.Key) || source.Ref.Revision == 0 || !name(source.Method) || source.GetFragment() == "" || len(source.GetFragment()) > 512 {
			return nil, memory.Invalid
		}
	}
	if _, err := extraction.IntersectRestrictions(1, []extraction.Restrictions{r.Restrictions}); err != nil {
		return nil, memory.Invalid
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > maxDocument {
		return nil, memory.Invalid
	}
	return data, nil
}
func (s *Store) Commit(ctx context.Context, r extraction.CandidateRecord) error {
	data, err := encode(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_lock SET version=version WHERE id=1`); err != nil {
		return memory.Unavailable
	}
	var retired int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM retired_candidates WHERE namespace=? AND operation=?`, r.Namespace, r.OperationID).Scan(&retired); err != nil {
		return memory.Unavailable
	}
	if retired != 0 {
		return memory.ReplayUnavailable
	}
	for _, source := range r.Candidate.Sources {
		through, e := sourceFence(ctx, tx, r.Namespace, source.Ref.Kind, source.Ref.Key)
		if e != nil {
			return e
		}
		if source.Ref.Revision <= through {
			return memory.ReplayUnavailable
		}
	}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT document FROM candidates WHERE namespace=? AND operation=?`, r.Namespace, r.OperationID).Scan(&old)
	if err == nil {
		if string(old) != string(data) {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM candidates)+(SELECT count(*) FROM retired_candidates)`).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count >= maxRecords {
		return memory.Capacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO candidates(namespace,operation,document) VALUES(?,?,?)`, r.Namespace, r.OperationID, data); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
func (s *Store) Lookup(ctx context.Context, namespace, operation string) (extraction.CandidateRecord, error) {
	if !name(namespace) || !name(operation) {
		return extraction.CandidateRecord{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT document FROM candidates WHERE namespace=? AND operation=?`, namespace, operation).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return extraction.CandidateRecord{}, memory.Missing
	}
	if err != nil || len(data) > maxDocument {
		return extraction.CandidateRecord{}, memory.Unavailable
	}
	var r extraction.CandidateRecord
	if json.Unmarshal(data, &r) != nil || r.Namespace != namespace || r.OperationID != operation {
		return extraction.CandidateRecord{}, memory.Unavailable
	}
	if _, err = encode(r); err != nil {
		return extraction.CandidateRecord{}, memory.Unavailable
	}
	return r, nil
}
