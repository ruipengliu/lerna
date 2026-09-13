// Package sqliteauth is the private-format, file-backed reference AuthorizationStore.
package sqliteauth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"net/url"
	"os"
	"path/filepath"

	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	_ "modernc.org/sqlite"
)

func init() {
	gob.Register(&wire.ResourceSelector_Exact{})
	gob.Register(&wire.ResourceSelector_Set{})
	gob.Register(&wire.ResourceSelector_Subtree{})
	gob.Register(&wire.AuthorizationCommand_RegisterPrincipal{})
	gob.Register(&wire.AuthorizationCommand_RegisterResource{})
	gob.Register(&wire.AuthorizationCommand_ReplacePolicy{})
	gob.Register(&wire.AuthorizationCommand_IssueGrant{})
	gob.Register(&wire.AuthorizationCommand_CloseWindows{})
	gob.Register(&wire.AuthorizationCommand_CleanRecords{})
}

const maxSnapshotBytes = 16 << 20

type Store struct{ db *sql.DB }

func failure(code authorization.Code) error { return &authorization.Error{Code: code} }
func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, failure(authorization.Invalid)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0) {
		return nil, failure(authorization.Denied)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, failure(authorization.Unavailable)
	}
	if err := file.Close(); err != nil {
		return nil, failure(authorization.Unavailable)
	}
	uri := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_pragma": []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"}}
	uri.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, failure(authorization.Unavailable)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS authorization_state (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, document BLOB NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, failure(authorization.Unavailable)
	}
	var initial bytes.Buffer
	if err := gob.NewEncoder(&initial).Encode(authorization.State{}); err != nil {
		db.Close()
		return nil, failure(authorization.Unavailable)
	}
	if _, err = db.Exec(`INSERT OR IGNORE INTO authorization_state(id,version,document) VALUES(1,0,?)`, initial.Bytes()); err != nil {
		db.Close()
		return nil, failure(authorization.Unavailable)
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Version(ctx context.Context) (string, error) {
	var v string
	if err := s.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&v); err != nil {
		return "", failure(authorization.Unavailable)
	}
	return v, nil
}
func (s *Store) Load(ctx context.Context) (authorization.Snapshot, error) {
	var snapshot authorization.Snapshot
	var data []byte
	if err := s.db.QueryRowContext(ctx, "SELECT version,document FROM authorization_state WHERE id=1").Scan(&snapshot.Version, &data); err != nil {
		return snapshot, failure(authorization.Unavailable)
	}
	if len(data) > maxSnapshotBytes {
		return snapshot, failure(authorization.Unavailable)
	}
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshot.State); err != nil {
		return snapshot, failure(authorization.Unavailable)
	}
	return snapshot, nil
}
func (s *Store) Commit(ctx context.Context, version uint64, state authorization.State) error {
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(state); err != nil {
		return failure(authorization.Invalid)
	}
	if data.Len() > maxSnapshotBytes {
		return failure(authorization.Unavailable)
	}
	result, err := s.db.ExecContext(ctx, "UPDATE authorization_state SET version=version+1,document=? WHERE id=1 AND version=?", data.Bytes(), version)
	if err != nil {
		return failure(authorization.OutcomeUnknown)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return failure(authorization.OutcomeUnknown)
	}
	if count != 1 {
		return failure(authorization.Conflict)
	}
	return nil
}
