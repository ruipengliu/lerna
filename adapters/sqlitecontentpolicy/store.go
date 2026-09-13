// Package sqlitecontentpolicy persists the trusted host's current source rules.
package sqlitecontentpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"golang.org/x/sys/unix"
	"lerna/adapters/contentpolicy"
	"lerna/artifacts"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const maxRulesBytes = 1 << 20

type Store struct{ db *sql.DB }

var _ contentpolicy.RuleStore = (*Store)(nil)

func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	var info unix.Stat_t
	err = unix.Fstat(int(file.Fd()), &info)
	file.Close()
	if err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0777 != 0600 || info.Uid != uint32(os.Getuid()) || info.Nlink != 1 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": []string{"busy_timeout(500)", "journal_mode(WAL)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS source_policy(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version>0),rules BLOB NOT NULL CHECK(length(rules)<=1048576));`)
	if err != nil {
		db.Close()
		return nil, artifacts.Error("UNAVAILABLE")
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

func encode(rules []contentpolicy.Rule) ([]byte, error) {
	if _, err := contentpolicy.New(rules); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(rules)
	if err != nil || len(raw) > maxRulesBytes {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return raw, nil
}
func (s *Store) Initialize(ctx context.Context, rules []contentpolicy.Rule) error {
	raw, err := encode(rules)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err = s.db.ExecContext(bounded, `INSERT INTO source_policy(id,version,rules) VALUES(1,1,?) ON CONFLICT(id) DO NOTHING`, raw); err != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	return nil
}
func (s *Store) Load(ctx context.Context) (contentpolicy.RuleSet, error) {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var version int64
	var raw []byte
	if err := s.db.QueryRowContext(bounded, `SELECT version,rules FROM source_policy WHERE id=1`).Scan(&version, &raw); err != nil || version < 1 || len(raw) > maxRulesBytes {
		return contentpolicy.RuleSet{}, artifacts.Error("UNAVAILABLE")
	}
	var rules []contentpolicy.Rule
	if json.Unmarshal(raw, &rules) != nil {
		return contentpolicy.RuleSet{}, artifacts.Error("UNAVAILABLE")
	}
	if _, err := contentpolicy.New(rules); err != nil {
		return contentpolicy.RuleSet{}, artifacts.Error("UNAVAILABLE")
	}
	return contentpolicy.RuleSet{Version: uint64(version), Rules: rules}, nil
}
func (s *Store) Replace(ctx context.Context, rules []contentpolicy.Rule) error {
	raw, err := encode(rules)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	result, err := s.db.ExecContext(bounded, `UPDATE source_policy SET version=version+1,rules=? WHERE id=1 AND version<9223372036854775807`, raw)
	if err != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return artifacts.Error("UNAVAILABLE")
	}
	return nil
}
