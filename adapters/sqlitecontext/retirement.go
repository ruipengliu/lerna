package sqlitecontext

import (
	"context"
	"database/sql"
	"lerna/contextassembly"
)

const identityCount = `SELECT COUNT(*) FROM (SELECT namespace,task_id,decision FROM context_snapshots UNION SELECT namespace,task_id,decision FROM context_retired)`

func (s *Store) Retire(ctx context.Context, key contextassembly.Key) error {
	if !validKey(key) {
		return contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contextassembly.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE context_lock SET version=version WHERE id=1`); err != nil {
		return contextassembly.Unavailable
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_snapshots WHERE namespace=? AND task_id=? AND decision=?) OR EXISTS(SELECT 1 FROM context_retired WHERE namespace=? AND task_id=? AND decision=?)`, key.Namespace, key.TaskID, key.Decision, key.Namespace, key.TaskID, key.Decision).Scan(&exists); err != nil {
		return contextassembly.Unavailable
	}
	if !exists {
		var count int
		if tx.QueryRowContext(ctx, identityCount).Scan(&count) != nil {
			return contextassembly.Unavailable
		}
		if count >= MaxSnapshots {
			return contextassembly.Capacity
		}
	}
	if err = retireSnapshot(ctx, tx, key); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return contextassembly.Unavailable
	}
	return nil
}

var _ contextassembly.RetirementStore = (*Store)(nil)

// retireSnapshot owns all erasure effects for one identity in the caller's
// existing write transaction. Capacity, source watermarks and commit stay with
// the caller. Reapplying retirement preserves the permanent identity fence.
func retireSnapshot(ctx context.Context, tx *sql.Tx, key contextassembly.Key) error {
	var err error
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO context_retired VALUES(?,?,?)`, key.Namespace, key.TaskID, key.Decision); err != nil {
		return contextassembly.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `UPDATE context_snapshots SET subject='',semantic='',document='{}' WHERE namespace=? AND task_id=? AND decision=?`, key.Namespace, key.TaskID, key.Decision); err != nil {
		return contextassembly.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM context_checkpoints WHERE namespace=? AND task_id=? AND decision=?`, key.Namespace, key.TaskID, key.Decision); err != nil {
		return contextassembly.Unavailable
	}
	return nil
}
