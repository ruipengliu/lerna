package sqlitememory

import (
	"context"
	"encoding/hex"
	"lerna/memory"
)

func (s *Store) Delete(ctx context.Context, in memory.Deletion) (memory.Receipt, error) {
	hash, err := hex.DecodeString(in.SemanticSHA256)
	if !validRef(in.Ref) || !label(in.OperationID) || !label(in.Subject) || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != in.SemanticSHA256 || in.Expected == 0 || in.Expected >= 1<<32 {
		return memory.Receipt{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	old, err := lookup(ctx, tx, in.Ref.Namespace, in.OperationID)
	if err == nil {
		if old.Subject != in.Subject || old.Ref != in.Ref || old.SemanticSHA256 != in.SemanticSHA256 || old.Revision != in.Expected+1 {
			return memory.Receipt{}, memory.IdentityConflict
		}
		var matches int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_tombstones WHERE namespace=? AND collection=? AND record_key=? AND revision=? AND operation=?`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Key, old.Revision, in.OperationID).Scan(&matches); err != nil {
			return memory.Receipt{}, memory.Unavailable
		}
		if matches != 1 {
			return memory.Receipt{}, memory.IdentityConflict
		}
		return old, nil
	}
	if err != memory.Missing {
		return memory.Receipt{}, err
	}
	var latest uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM memory_operations WHERE namespace=? AND collection=? AND record_key=?`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Key).Scan(&latest); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if latest != in.Expected {
		return memory.Receipt{}, memory.Conflict
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_tombstones`).Scan(&count); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if count >= MaxRevisions {
		return memory.Receipt{}, memory.Capacity
	}
	var position uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=?)`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Namespace, in.Ref.Collection).Scan(&position); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_tombstones VALUES(?,?,?,?,?)`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Key, in.Expected+1, in.OperationID); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=?`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Key); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	// Historical acceptance is retained, but not the erased payload's digest.
	if _, err = tx.ExecContext(ctx, `UPDATE memory_operations SET semantic='' WHERE namespace=? AND collection=? AND record_key=?`, in.Ref.Namespace, in.Ref.Collection, in.Ref.Key); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_operations VALUES(?,?,?,?,?,?,?,?)`, in.Ref.Namespace, in.OperationID, in.Subject, in.SemanticSHA256, in.Ref.Collection, in.Ref.Key, in.Expected+1, position); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Receipt{}, memory.Unavailable
	}
	return memory.Receipt{OperationID: in.OperationID, Subject: in.Subject, SemanticSHA256: in.SemanticSHA256, Ref: in.Ref, Revision: in.Expected + 1, Position: position}, nil
}

var _ memory.DeletionStore = (*Store)(nil)
