// Package sqlite stores immutable declarations and a replaceable text
// index. A target version gate serializes new invocation admission with updates.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/schema"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const MaxEntries = 2048
const maxDeclaration = 65536

// ImplementationCheck is supplied by trusted host assembly, never by a query.
// It must verify that the exact descriptor has a callable implementation.
type ImplementationCheck func(catalog.Entry) error
type Store struct {
	db             *sql.DB
	implementation ImplementationCheck
}

func failure(c authorization.Code) error { return &authorization.Error{Code: c} }
func Open(path string, check ImplementationCheck) (*Store, error) {
	if check == nil {
		return nil, failure(authorization.Invalid)
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, failure(authorization.Denied)
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_pragma": []string{"busy_timeout(1000)", "journal_mode(WAL)", "synchronous(FULL)"}, "_txlock": []string{"immediate"}}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS catalog_meta(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,index_revision INTEGER NOT NULL,cursor_key BLOB NOT NULL)`, `CREATE TABLE IF NOT EXISTS catalog_entries(ref TEXT PRIMARY KEY,summary BLOB NOT NULL,declaration BLOB NOT NULL,active INTEGER NOT NULL,available INTEGER NOT NULL)`, `CREATE TABLE IF NOT EXISTS catalog_index(ref TEXT PRIMARY KEY,terms TEXT NOT NULL)`} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		db.Close()
		return nil, e
	}
	if _, e = db.Exec(`INSERT OR IGNORE INTO catalog_meta VALUES(1,0,0,?)`, key); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db, implementation: check}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) CursorKey(ctx context.Context) ([]byte, error) {
	var key []byte
	e := s.db.QueryRowContext(ctx, `SELECT cursor_key FROM catalog_meta WHERE id=1`).Scan(&key)
	return key, e
}
func validate(e catalog.Entry) error {
	if e.Source.Kind == "" || e.Source.Key == "" || e.Source.Revision == 0 || len(e.Source.Kind) > 128 || len(e.Source.Key) > 128 {
		return failure(authorization.Invalid)
	}
	r := e.Ref
	c := e.Capability
	if c.Name == "" || c.Version == "" || c.Implementation == "" || c.ImplementationVersion == "" || c.Resource == "" || c.Purpose == "" || c.Location != "local" || !c.Exclusive || len(c.Input.Document) > 16384 || len(c.Output.Document) > 16384 {
		return failure(authorization.Invalid)
	}
	if r.Namespace == "" || len(r.Namespace) > 128 || r.Name != c.Name || r.Version != c.Version || r.Implementation != c.Implementation || r.ImplementationVersion != c.ImplementationVersion || r.Digest != c.Digest() || e.Resource != c.Resource || e.Purpose != c.Purpose || e.Location != c.Location || e.Title == "" || e.Category == "" || e.ResourceType == "" || e.Preconditions == "" || e.Effects == "" || e.Unsupported == "" || e.Guarantees == "" || len(e.Aliases) > 16 {
		return failure(authorization.Invalid)
	}
	for _, v := range []string{r.Name, r.Version, r.Implementation, r.ImplementationVersion, e.Title, e.Category, e.Resource, e.DiscoveryResource, e.ResourceType, e.Purpose, e.Location} {
		if len(v) > 256 || strings.ContainsRune(v, 0) {
			return failure(authorization.Invalid)
		}
	}
	if len(e.Preconditions) > 2048 || len(e.Effects) > 2048 || len(e.Unsupported) > 2048 || len(e.Guarantees) > 2048 {
		return failure(authorization.Invalid)
	}
	for _, a := range e.Aliases {
		if len(a) > 256 {
			return failure(authorization.Invalid)
		}
	}
	if _, err := schema.New([]schema.Resource{c.Input, c.Output}); err != nil {
		return failure(authorization.Invalid)
	}
	b, err := json.Marshal(e)
	if err != nil || len(b) > maxDeclaration {
		return failure(authorization.Invalid)
	}
	return nil
}
func (s *Store) Replace(ctx context.Context, expected uint64, entries []catalog.Entry) (uint64, error) {
	if len(entries) < 1 || len(entries) > MaxEntries {
		return 0, failure(authorization.Invalid)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := s.implementation(e); err != nil {
			return 0, err
		}
		if err := validate(e); err != nil {
			return 0, err
		}
		key := catalog.Key(e.Ref)
		if seen[key] {
			return 0, failure(authorization.IdentityConflict)
		}
		seen[key] = true
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var revision uint64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM catalog_meta WHERE id=1`).Scan(&revision); e != nil {
		return 0, e
	}
	if revision != expected {
		return revision, failure(authorization.Conflict)
	}
	var total int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM catalog_entries`).Scan(&total); e != nil {
		return revision, e
	}
	versions := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT ref,summary FROM catalog_entries`)
	if err != nil {
		return revision, err
	}
	for rows.Next() {
		var key string
		var b []byte
		if err = rows.Scan(&key, &b); err != nil {
			rows.Close()
			return revision, err
		}
		var other catalog.Summary
		if json.Unmarshal(b, &other) != nil {
			rows.Close()
			return revision, failure(authorization.Unavailable)
		}
		r := other.Ref
		r.Digest = ""
		versions[catalog.Key(r)] = key
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return revision, err
	}
	if _, e = tx.ExecContext(ctx, `UPDATE catalog_entries SET active=0`); e != nil {
		return revision, e
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return revision, err
		}
		key := catalog.Key(entry.Ref)
		entry.Available = true
		raw, _ := json.Marshal(entry)
		summary, _ := json.Marshal(entry.Summary())
		var old []byte
		err := tx.QueryRowContext(ctx, `SELECT declaration FROM catalog_entries WHERE ref=?`, key).Scan(&old)
		if err == nil && string(old) != string(raw) {
			return revision, failure(authorization.IdentityConflict)
		}
		if err != nil && err != sql.ErrNoRows {
			return revision, err
		}
		if err == sql.ErrNoRows {
			total++
			if total > 4096 {
				return revision, failure(authorization.Unavailable)
			}
		}
		identity := entry.Ref
		identity.Digest = ""
		versionKey := catalog.Key(identity)
		if existing, ok := versions[versionKey]; ok && existing != key {
			return revision, failure(authorization.IdentityConflict)
		}
		versions[versionKey] = key

		if _, e = tx.ExecContext(ctx, `INSERT INTO catalog_entries VALUES(?,?,?,1,1) ON CONFLICT(ref) DO UPDATE SET active=1`, key, summary, raw); e != nil {
			return revision, e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE catalog_meta SET revision=revision+1 WHERE id=1`); e != nil {
		return revision, e
	}
	if e = tx.Commit(); e != nil {
		return revision, failure(authorization.OutcomeUnknown)
	}
	return revision + 1, nil
}
func (s *Store) Snapshot(ctx context.Context, after string, limit int) (catalog.Snapshot, error) {
	out := catalog.Snapshot{Rows: []catalog.Summary{}}
	if limit < 1 || limit > 4096 || len(after) > 2048 {
		return out, failure(authorization.Invalid)
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM catalog_meta WHERE id=1`).Scan(&out.Revision); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT ref,summary,available FROM catalog_entries WHERE active=1 AND ref>? ORDER BY ref LIMIT ?`, after, limit+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Rows) == limit {
			out.More = true
			break
		}
		var b []byte
		var available bool
		if e = rows.Scan(&out.After, &b, &available); e != nil {
			return out, e
		}
		var r catalog.Summary
		if json.Unmarshal(b, &r) != nil {
			return out, failure(authorization.Unavailable)
		}
		r.Available = available
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}
func (s *Store) Describe(ctx context.Context, ref catalog.Ref) (catalog.Entry, uint64, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return catalog.Entry{}, 0, e
	}
	defer tx.Rollback()
	return describe(ctx, tx, ref, true)
}
func describe(ctx context.Context, tx *sql.Tx, ref catalog.Ref, active bool) (catalog.Entry, uint64, error) {
	var entry catalog.Entry
	var revision uint64
	if e := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_meta WHERE id=1`).Scan(&revision); e != nil {
		return entry, 0, e
	}
	var b []byte
	var enabled, available bool
	e := tx.QueryRowContext(ctx, `SELECT declaration,active,available FROM catalog_entries WHERE ref=?`, catalog.Key(ref)).Scan(&b, &enabled, &available)
	if e == sql.ErrNoRows || e == nil && active && !enabled {
		return entry, revision, failure(authorization.NotFound)
	}
	if e != nil {
		return entry, revision, e
	}
	if len(b) > maxDeclaration || json.Unmarshal(b, &entry) != nil || entry.Ref != ref || entry.Capability.Digest() != ref.Digest {
		return entry, revision, failure(authorization.Unavailable)
	}
	entry.Available = available
	return entry, revision, nil
}
func (s *Store) WithVersion(ctx context.Context, ref catalog.Ref, fn func(catalog.Entry) error) error {
	if fn == nil {
		return failure(authorization.Invalid)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	entry, _, e := describe(ctx, tx, ref, true)
	if e != nil {
		return e
	}
	if !entry.Available {
		return failure(authorization.Unavailable)
	}
	return fn(entry)
}
func (s *Store) Historical(ctx context.Context, ref catalog.Ref) (catalog.Entry, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return catalog.Entry{}, e
	}
	defer tx.Rollback()
	entry, _, e := describe(ctx, tx, ref, false)
	return entry, e
}
func (s *Store) SetAvailable(ctx context.Context, ref catalog.Ref, available bool) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE catalog_entries SET available=? WHERE ref=? AND active=1`, available, catalog.Key(ref))
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return failure(authorization.NotFound)
	}
	if _, e = tx.ExecContext(ctx, `UPDATE catalog_meta SET revision=revision+1 WHERE id=1`); e != nil {
		return e
	}
	return tx.Commit()
}

var _ catalog.Store = (*Store)(nil)

func (s *Store) LookupSummary(ctx context.Context, ref catalog.Ref) (catalog.Summary, uint64, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return catalog.Summary{}, 0, e
	}
	defer tx.Rollback()
	var revision uint64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM catalog_meta WHERE id=1`).Scan(&revision); e != nil {
		return catalog.Summary{}, 0, e
	}
	var raw []byte
	var available bool
	e = tx.QueryRowContext(ctx, `SELECT summary,available FROM catalog_entries WHERE ref=? AND active=1`, catalog.Key(ref)).Scan(&raw, &available)
	if e == sql.ErrNoRows {
		return catalog.Summary{}, revision, failure(authorization.NotFound)
	}
	if e != nil {
		return catalog.Summary{}, revision, e
	}
	var summary catalog.Summary
	if len(raw) > maxDeclaration || json.Unmarshal(raw, &summary) != nil || summary.Ref != ref {
		return summary, revision, failure(authorization.Unavailable)
	}
	summary.Available = available
	return summary, revision, nil
}
