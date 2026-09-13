package sqlitecontext

import (
	"context"
	"lerna/contextassembly"
	"strconv"
)

// InvalidateRevision atomically persists the exact fence and erases all matching
// snapshots/checkpoints. Mixed-source snapshots are retired in their entirety.
func (s *Store) InvalidateRevision(ctx context.Context, ref contextassembly.Reference) (int, error) {
	change := contextassembly.SourceInvalidation{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, ThroughRevision: ref.Revision}
	if ref.Revision > 1<<32 || !contextassembly.ValidInvalidation(change) {
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
	revision := strconv.FormatUint(ref.Revision, 10)
	var exists, count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_revision_invalidations WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, ref.Namespace, ref.Collection, ref.Key, revision).Scan(&exists); err != nil {
		return 0, contextassembly.Unavailable
	}
	if exists == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM context_source_invalidations)+(SELECT COUNT(*) FROM context_revision_invalidations)`).Scan(&count); err != nil {
			return 0, contextassembly.Unavailable
		}
		if count >= MaxInvalidations {
			return 0, contextassembly.Capacity
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO context_revision_invalidations VALUES(?,?,?,?)`, ref.Namespace, ref.Collection, ref.Key, revision); err != nil {
			return 0, contextassembly.Unavailable
		}
	}
	affected, err := retireMatchingSnapshots(ctx, tx, ref.Namespace, func(raw []byte) (bool, error) { return contextassembly.SnapshotUsesRevision(raw, ref) })
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, contextassembly.Unavailable
	}
	return affected, nil
}

var _ contextassembly.RevisionInvalidationStore = (*Store)(nil)
