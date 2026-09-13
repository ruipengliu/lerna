package sqlitecontext

import (
	"context"
	"database/sql"
	"strconv"

	"lerna/contextassembly"
)

const MaxInvalidations = 512

func checkWatermarks(ctx context.Context, tx *sql.Tx, in contextassembly.Snapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT collection,record_key,missing,revision,0 FROM context_source_invalidations WHERE namespace=? UNION ALL SELECT collection,record_key,0,revision,1 FROM context_revision_invalidations WHERE namespace=? LIMIT ?`, in.Key.Namespace, in.Key.Namespace, MaxInvalidations+1)
	if err != nil {
		return contextassembly.Unavailable
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > MaxInvalidations {
			return contextassembly.Unavailable
		}
		change := contextassembly.SourceInvalidation{Namespace: in.Key.Namespace}
		var revision string
		var exact bool
		if err = rows.Scan(&change.Collection, &change.Key, &change.Missing, &revision, &exact); err != nil {
			return contextassembly.Unavailable
		}
		change.ThroughRevision, err = strconv.ParseUint(revision, 10, 64)
		if err != nil {
			return contextassembly.Unavailable
		}
		var affected bool
		if exact {
			affected, err = contextassembly.SnapshotUsesRevision(in.Document, contextassembly.Reference{Namespace: change.Namespace, Collection: change.Collection, Key: change.Key, Revision: change.ThroughRevision})
		} else {
			affected, err = contextassembly.SnapshotAffected(in.Document, change)
		}
		if err != nil {
			return err
		}
		if affected {
			return contextassembly.Invalidated
		}
	}
	if rows.Err() != nil {
		return contextassembly.Unavailable
	}
	return nil
}

// InvalidateSource removes active logical payloads in one bounded transaction.
// It does not claim physical erasure of SQLite free pages, WAL or old backups.
// Snapshot capacity (512) bounds the scan; timeout bounds the transaction.
func (s *Store) InvalidateSource(ctx context.Context, in contextassembly.SourceInvalidation) (int, error) {
	if !contextassembly.ValidInvalidation(in) {
		return 0, contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, contextassembly.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE context_lock SET version=version WHERE id=1`); err != nil {
		return 0, contextassembly.Unavailable
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT revision FROM context_source_invalidations WHERE namespace=? AND collection=? AND record_key=? AND missing=?`, in.Namespace, in.Collection, in.Key, in.Missing).Scan(&previous)
	if err == nil {
		revision, e := strconv.ParseUint(previous, 10, 64)
		if e != nil || revision == 0 {
			return 0, contextassembly.Unavailable
		}
		if revision > in.ThroughRevision {
			in.ThroughRevision = revision
		}
	} else if err == sql.ErrNoRows {
		var count int
		if tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM context_source_invalidations)+(SELECT COUNT(*) FROM context_revision_invalidations)`).Scan(&count) != nil {
			return 0, contextassembly.Unavailable
		}
		if count >= MaxInvalidations {
			return 0, contextassembly.Capacity
		}
	} else {
		return 0, contextassembly.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO context_source_invalidations VALUES(?,?,?,?,?) ON CONFLICT(namespace,collection,record_key,missing) DO UPDATE SET revision=excluded.revision`, in.Namespace, in.Collection, in.Key, in.Missing, strconv.FormatUint(in.ThroughRevision, 10)); err != nil {
		return 0, contextassembly.Unavailable
	}
	count, err := retireMatchingSnapshots(ctx, tx, in.Namespace, func(document []byte) (bool, error) { return contextassembly.SnapshotAffected(document, in) })
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, contextassembly.Unavailable
	}
	return count, nil
}

func retireMatchingSnapshots(ctx context.Context, tx *sql.Tx, namespace string, affected func([]byte) (bool, error)) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT s.task_id,s.decision,s.document FROM context_snapshots s LEFT JOIN context_retired r USING(namespace,task_id,decision) WHERE s.namespace=? AND r.decision IS NULL LIMIT ?`, namespace, MaxSnapshots+1)
	if err != nil {
		return 0, contextassembly.Unavailable
	}
	var keys []contextassembly.Key
	count := 0
	for rows.Next() {
		count++
		if count > MaxSnapshots {
			rows.Close()
			return 0, contextassembly.Unavailable
		}
		key := contextassembly.Key{Namespace: namespace}
		var document []byte
		if err = rows.Scan(&key.TaskID, &key.Decision, &document); err != nil {
			rows.Close()
			return 0, contextassembly.Unavailable
		}
		matches, e := affected(document)
		if e != nil {
			rows.Close()
			return 0, e
		}
		if matches {
			keys = append(keys, key)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, contextassembly.Unavailable
	}
	for _, key := range keys {
		if err = retireSnapshot(ctx, tx, key); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

var _ contextassembly.InvalidationStore = (*Store)(nil)
