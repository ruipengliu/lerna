package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"lerna/memory"
)

func (s *Store) RecoverySnapshot(ctx context.Context, scope memory.RecoveryScope, after uint64) (memory.RecoverySnapshot, error) {
	if !memory.ValidRecoveryScope(scope) || after > 1<<32 {
		return memory.RecoverySnapshot{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	defer tx.Rollback()
	out := memory.RecoverySnapshot{Scope: scope, After: after}
	// Check at the same transaction snapshot as the inventory. Only a normal
	// current Store may serve authority state, not the quarantined Restore handle.
	var quarantined bool
	if err = tx.QueryRowContext(ctx, `SELECT quarantined FROM memory_recovery WHERE id=1`).Scan(&quarantined); err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	if quarantined {
		return memory.RecoverySnapshot{}, memory.Quarantined
	}
	var count uint64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0),COUNT(*) FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=?)`, scope.Namespace, scope.Collection, scope.Namespace, scope.Collection).Scan(&out.Position, &count); err != nil || count != out.Position {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	if after > out.Position {
		return memory.RecoverySnapshot{}, memory.Conflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT record_key,revision,document FROM memory_revisions WHERE namespace=? AND collection=? ORDER BY record_key,revision LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	for rows.Next() {
		version := memory.VersionRef{Ref: memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}}
		var raw []byte
		if err = rows.Scan(&version.Ref.Key, &version.Revision, &raw); err != nil || !validRef(version.Ref) || version.Revision == 0 || version.Revision > 1<<32 || len(raw) > MaxDocument || !json.Valid(raw) {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Unavailable
		}
		out.Records = append(out.Records, memory.RecoveryRecord{Version: version, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))})
		if len(out.Records) > MaxRevisions {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Capacity
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	rows, err = tx.QueryContext(ctx, `SELECT record_key,revision FROM memory_tombstones WHERE namespace=? AND collection=? ORDER BY record_key LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	for rows.Next() {
		version := memory.VersionRef{Ref: memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}}
		if err = rows.Scan(&version.Ref.Key, &version.Revision); err != nil || !validRef(version.Ref) || version.Revision == 0 || version.Revision > 1<<32 {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Unavailable
		}
		out.Deleted = append(out.Deleted, version)
		if len(out.Deleted) > MaxRevisions {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Capacity
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	rows, err = tx.QueryContext(ctx, `SELECT operation,subject,semantic,record_key,revision,position FROM memory_operations WHERE namespace=? AND collection=? AND position>? AND position<=? ORDER BY position LIMIT 512`, scope.Namespace, scope.Collection, after, min(out.Position, after+512))
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	for rows.Next() {
		receipt := memory.Receipt{Ref: memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}}
		if err = rows.Scan(&receipt.OperationID, &receipt.Subject, &receipt.SemanticSHA256, &receipt.Ref.Key, &receipt.Revision, &receipt.Position); err != nil || !validRef(receipt.Ref) || receipt.Revision == 0 || receipt.Revision > 1<<32 || !label(receipt.OperationID) || !label(receipt.Subject) {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Unavailable
		}
		out.Operations = append(out.Operations, receipt)
		if len(out.Operations) > MaxRevisions {
			rows.Close()
			return memory.RecoverySnapshot{}, memory.Capacity
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	if err = readRecoveryErasures(ctx, tx, &out); err != nil {
		return memory.RecoverySnapshot{}, err
	}
	if !memory.ValidRecoverySnapshot(out) {
		return memory.RecoverySnapshot{}, memory.Unavailable
	}
	return out, nil
}

var _ memory.RecoveryStateSource = (*Store)(nil)
