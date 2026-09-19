package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"

	"lerna/memory"
)

func sanitizeRecovery(ctx context.Context, tx *sql.Tx, state memory.RecoverySnapshot) (int, int, error) {
	fail := func(err error) (int, int, error) { return 0, 0, err }
	scope := state.Scope
	if err := applyRecoveryFences(ctx, tx, state); err != nil {
		return fail(err)
	}
	erased := recoveryErased(state)
	deleted := map[memory.Ref]uint64{}
	for _, v := range state.Deleted {
		deleted[v.Ref] = v.Revision
	}
	comparisons := map[memory.VersionRef]string{}
	for _, record := range state.Records {
		comparisons[record.Version] = record.SHA256
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT record_key FROM memory_operations WHERE namespace=? AND collection=? LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return fail(memory.Unavailable)
	}
	owned := map[memory.Ref]bool{}
	for rows.Next() {
		ref := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}
		if rows.Scan(&ref.Key) != nil || !validRef(ref) {
			rows.Close()
			return fail(memory.Unavailable)
		}
		owned[ref] = true
		if len(owned) > 512 {
			rows.Close()
			return fail(memory.Capacity)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(memory.Unavailable)
	}
	rows, err = tx.QueryContext(ctx, `SELECT record_key,revision FROM memory_recovery_deleted WHERE namespace=? AND collection=? LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return fail(memory.Unavailable)
	}
	fences := 0
	for rows.Next() {
		ref := memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}
		var revision uint64
		if rows.Scan(&ref.Key, &revision) != nil || revision == 0 || deleted[ref] != revision || !owned[ref] {
			rows.Close()
			return fail(memory.IdentityConflict)
		}
		fences++
		if fences > 512 {
			rows.Close()
			return fail(memory.Capacity)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(memory.Unavailable)
	}
	rows, err = tx.QueryContext(ctx, `SELECT record_key,revision,document FROM memory_revisions WHERE namespace=? AND collection=? LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return fail(memory.Unavailable)
	}
	retained, total := 0, 0
	for rows.Next() {
		v := memory.VersionRef{Ref: memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}}
		var raw []byte
		if rows.Scan(&v.Ref.Key, &v.Revision, &raw) != nil || !owned[v.Ref] || v.Revision == 0 || len(raw) > MaxDocument {
			rows.Close()
			return fail(memory.Unavailable)
		}
		total++
		if total > 512 {
			rows.Close()
			return fail(memory.Capacity)
		}
		if end := deleted[v.Ref]; end != 0 {
			if v.Revision >= end {
				rows.Close()
				return fail(memory.IdentityConflict)
			}
			continue
		}
		if erased[v].Position != 0 {
			continue
		}
		if err = sourceAllowed(ctx, tx, scope.Namespace, raw); err != nil {
			rows.Close()
			return fail(err)
		}
		if comparisons[v] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			rows.Close()
			return fail(memory.IdentityConflict)
		}
		retained++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(memory.Unavailable)
	}
	for ref := range owned {
		end := deleted[ref]
		if end == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=?`, scope.Namespace, scope.Collection, ref.Key); err != nil {
			return fail(memory.Unavailable)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_operations SET semantic='' WHERE namespace=? AND collection=? AND record_key=? AND revision<?`, scope.Namespace, scope.Collection, ref.Key, end); err != nil {
			return fail(memory.Unavailable)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_recovery_deleted VALUES(?,?,?,?) ON CONFLICT(namespace,collection,record_key) DO NOTHING`, scope.Namespace, scope.Collection, ref.Key, end); err != nil {
			return fail(memory.Unavailable)
		}
		// Verify erasure in the same transaction before advancing the marker.
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=?)+(SELECT COUNT(*) FROM memory_operations WHERE namespace=? AND collection=? AND record_key=? AND revision<? AND semantic!='')`, scope.Namespace, scope.Collection, ref.Key, scope.Namespace, scope.Collection, ref.Key, end).Scan(&remaining); err != nil || remaining != 0 {
			return fail(memory.Unavailable)
		}
	}
	for version, event := range erased {
		if !owned[version.Ref] {
			continue
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, scope.Namespace, scope.Collection, version.Ref.Key, version.Revision); err != nil {
			return fail(memory.Unavailable)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_operations SET semantic='' WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, scope.Namespace, scope.Collection, version.Ref.Key, version.Revision); err != nil {
			return fail(memory.Unavailable)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_recovery_erased VALUES(?,?,?,?,?) ON CONFLICT(namespace,collection,record_key,revision) DO NOTHING`, scope.Namespace, scope.Collection, version.Ref.Key, version.Revision, event.Position); err != nil {
			return fail(memory.Unavailable)
		}
	}
	var exactCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_recovery_erased`).Scan(&exactCount); err != nil {
		return fail(memory.Unavailable)
	}
	if exactCount > 512 {
		return fail(memory.Capacity)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_recovery_deleted`).Scan(&count); err != nil {
		return fail(memory.Unavailable)
	}
	if count > 512 {
		return fail(memory.Capacity)
	}
	return retained, len(state.Records) - retained, nil
}
