package sqlitecatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"lerna/authorization"
	"lerna/catalog"
)

func (s *Store) Index(ctx context.Context, refs []catalog.Ref, revision uint64) (catalog.IndexResult, error) {
	out := catalog.IndexResult{Terms: map[string]string{}, Status: "CURRENT"}
	if len(refs) > 4096 {
		return out, failure(authorization.Invalid)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, `SELECT index_revision FROM catalog_meta WHERE id=1`).Scan(&out.Revision); e != nil {
		return out, e
	}
	if out.Revision != revision {
		out.Status = "STALE"
		var staging int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='catalog_rebuild'`).Scan(&staging); e != nil {
			return out, e
		}
		if staging > 0 {
			var rev uint64
			err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_rebuild WHERE id=1`).Scan(&rev)
			if err == nil && rev == revision {
				out.Status = "REBUILDING"
			} else if err != nil && err != sql.ErrNoRows {
				return out, err
			}
		}
		if out.Revision == 0 && out.Status != "REBUILDING" {
			out.Status = "MISSING"
		}
		return out, nil
	}
	for _, ref := range refs {
		var text string
		if e = tx.QueryRowContext(ctx, `SELECT terms FROM catalog_index WHERE ref=?`, catalog.Key(ref)).Scan(&text); e != nil {
			if e != sql.ErrNoRows {
				return out, e
			}
			out.Status = "PARTIAL"
			continue
		}
		out.Terms[catalog.Key(ref)] = text
	}
	return out, nil
}
func (s *Store) Rebuild(ctx context.Context, batch int) (bool, error) {
	if batch < 1 || batch > 64 {
		return false, failure(authorization.Invalid)
	}
	// Each pass stages a bounded page; only a complete revision becomes current.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS catalog_rebuild(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,after_ref TEXT NOT NULL)`); e != nil {
		return false, e
	}
	var revision, index uint64
	if e = tx.QueryRowContext(ctx, `SELECT revision,index_revision FROM catalog_meta WHERE id=1`).Scan(&revision, &index); e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS catalog_index(ref TEXT PRIMARY KEY,terms TEXT NOT NULL)`); e != nil {
		return false, e
	}
	if revision == index {
		var missing int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM catalog_entries e LEFT JOIN catalog_index i ON e.ref=i.ref WHERE e.active=1 AND i.ref IS NULL`).Scan(&missing); e != nil {
			return false, e
		}
		if missing == 0 {
			return true, tx.Commit()
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM catalog_rebuild`); e != nil {
			return false, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE catalog_meta SET index_revision=0 WHERE id=1`); e != nil {
			return false, e
		}
	}
	var staging uint64
	var after string
	err := tx.QueryRowContext(ctx, `SELECT revision,after_ref FROM catalog_rebuild WHERE id=1`).Scan(&staging, &after)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err != nil || staging != revision {
		if _, e = tx.ExecContext(ctx, `DELETE FROM catalog_index`); e != nil {
			return false, e
		}
		after = ""
		if _, e = tx.ExecContext(ctx, `INSERT INTO catalog_rebuild VALUES(1,?,'') ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,after_ref=''`, revision); e != nil {
			return false, e
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT ref,summary FROM catalog_entries WHERE active=1 AND ref>? ORDER BY ref LIMIT ?`, after, batch)
	if e != nil {
		return false, e
	}
	type pair struct {
		key string
		row catalog.Summary
	}
	items := []pair{}
	for rows.Next() {
		var key string
		var b []byte
		if e = rows.Scan(&key, &b); e != nil {
			rows.Close()
			return false, e
		}
		var row catalog.Summary
		if e = json.Unmarshal(b, &row); e != nil {
			rows.Close()
			return false, e
		}
		items = append(items, pair{key, row})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false, e
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, e = tx.ExecContext(ctx, `INSERT OR REPLACE INTO catalog_index VALUES(?,?)`, item.key, catalog.Text(item.row)); e != nil {
			return false, e
		}
		after = item.key
	}
	if _, e = tx.ExecContext(ctx, `UPDATE catalog_rebuild SET after_ref=? WHERE id=1`, after); e != nil {
		return false, e
	}
	complete := len(items) < batch
	if complete {
		if _, e = tx.ExecContext(ctx, `UPDATE catalog_meta SET index_revision=? WHERE id=1`, revision); e != nil {
			return false, e
		}
	}
	return complete, tx.Commit()
}
func (s *Store) InvalidateIndex(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS catalog_rebuild(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,after_ref TEXT NOT NULL)`, `DELETE FROM catalog_rebuild`, `DELETE FROM catalog_index`, `UPDATE catalog_meta SET index_revision=0 WHERE id=1`} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit()
}
