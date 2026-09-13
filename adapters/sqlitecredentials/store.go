// Package sqlitecredentials is the local ciphertext-only CredentialStore.
package sqlitecredentials

import (
	"context"
	"database/sql"
	"encoding/json"
	"lerna/credentials"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

const MaxRecords = 64
const maxDocument = 16384

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, credentials.Invalid
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, credentials.Invalid
	}
	st, e := os.Lstat(path)
	if e == nil && (!st.Mode().IsRegular() || st.Mode().Perm() != 0600) {
		return nil, credentials.Denied
	}
	if e != nil && !os.IsNotExist(e) {
		return nil, credentials.Unavailable
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, credentials.Unavailable
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_pragma": []string{"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, credentials.Unavailable
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS credential_ciphertext(ref TEXT PRIMARY KEY,revision INTEGER NOT NULL,document BLOB NOT NULL CHECK(length(document)<=16384))`); e != nil {
		db.Close()
		return nil, credentials.Unavailable
	}
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS credential_lifecycle(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,document BLOB NOT NULL CHECK(length(document)<=524288)); INSERT OR IGNORE INTO credential_lifecycle(id,revision,document) VALUES(1,0,'{"Revision":0,"Rotations":[]}')`); e != nil {
		db.Close()
		return nil, credentials.Unavailable
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func decode(raw []byte) (credentials.Record, error) {
	var r credentials.Record
	if len(raw) > maxDocument || json.Unmarshal(raw, &r) != nil || !credentials.ValidRecord(r) {
		return r, credentials.Unavailable
	}
	return r, nil
}
func (s *Store) Get(ctx context.Context, ref string) (credentials.Record, error) {
	if len(ref) == 0 || len(ref) > 128 {
		return credentials.Record{}, credentials.Invalid
	}
	var raw []byte
	var revision uint64
	e := s.db.QueryRowContext(ctx, "SELECT revision,document FROM credential_ciphertext WHERE ref=?", ref).Scan(&revision, &raw)
	if e == sql.ErrNoRows {
		return credentials.Record{}, credentials.Missing
	}
	if e != nil {
		return credentials.Record{}, credentials.Unavailable
	}
	r, e := decode(raw)
	if e != nil {
		return credentials.Record{}, e
	}
	if r.Ref != ref || r.Revision != revision {
		return credentials.Record{}, credentials.Unavailable
	}
	return r, nil
}
func (s *Store) Swap(ctx context.Context, expected uint64, r credentials.Record) error {
	if !credentials.ValidRecord(r) || expected >= 1<<32 || r.Revision != expected+1 {
		return credentials.Invalid
	}
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > maxDocument {
		return credentials.Invalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return credentials.Unavailable
	}
	defer tx.Rollback()
	state, e := loadLifecycle(ctx, tx)
	if e != nil {
		return e
	}
	if !writeAllowed(state, r) {
		return credentials.Conflict
	}
	if e = swapRecord(ctx, tx, expected, r, raw); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return credentials.Unavailable
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]credentials.Record, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT ref,revision,document FROM credential_ciphertext ORDER BY ref LIMIT 65")
	if e != nil {
		return nil, credentials.Unavailable
	}
	defer rows.Close()
	out := []credentials.Record{}
	for rows.Next() {
		var ref string
		var rev uint64
		var raw []byte
		if e = rows.Scan(&ref, &rev, &raw); e != nil {
			return nil, credentials.Unavailable
		}
		r, e := decode(raw)
		if e != nil || r.Ref != ref || r.Revision != rev {
			return nil, credentials.Unavailable
		}
		out = append(out, r)
	}
	if rows.Err() != nil || len(out) > MaxRecords {
		return nil, credentials.Unavailable
	}
	return out, nil
}
