package sqlitememory

import (
	"context"
	"database/sql"
	"lerna/memory"
	"strconv"
)

func recoveryErased(state memory.RecoverySnapshot) map[memory.VersionRef]memory.SourceEvent {
	out := map[memory.VersionRef]memory.SourceEvent{}
	for _, event := range state.Erased {
		out[memory.VersionRef{Ref: event.Ref, Revision: event.Revision}] = event
	}
	return out
}

// The fresh proof may tighten a durable source cutoff, but never forget or
// lower it. Exact erasure facts likewise survive subsequent reconciliation.
func applyRecoveryFences(ctx context.Context, tx *sql.Tx, state memory.RecoverySnapshot) error {
	fences := map[[2]string]uint64{}
	for _, f := range state.SourceFences {
		fences[[2]string{f.Kind, f.Key}] = f.ThroughRevision
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,source_key,revision FROM memory_source_fences WHERE namespace=? LIMIT 513`, state.Scope.Namespace)
	if err != nil {
		return memory.Unavailable
	}
	count := 0
	for rows.Next() {
		count++
		if count > 512 {
			rows.Close()
			return memory.Capacity
		}
		var kind, key, raw string
		if err = rows.Scan(&kind, &key, &raw); err != nil {
			rows.Close()
			return memory.Unavailable
		}
		through, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || through == 0 {
			rows.Close()
			return memory.Unavailable
		}
		if fences[[2]string{kind, key}] < through {
			rows.Close()
			return memory.IdentityConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.Unavailable
	}
	erased := recoveryErased(state)
	rows, err = tx.QueryContext(ctx, `SELECT record_key,revision,position FROM memory_recovery_erased WHERE namespace=? AND collection=? UNION SELECT record_key,revision,position FROM memory_erasure_events WHERE namespace=? AND collection=? LIMIT 513`, state.Scope.Namespace, state.Scope.Collection, state.Scope.Namespace, state.Scope.Collection)
	if err != nil {
		return memory.Unavailable
	}
	count = 0
	for rows.Next() {
		count++
		if count > 512 {
			rows.Close()
			return memory.Capacity
		}
		version := memory.VersionRef{Ref: memory.Ref{Namespace: state.Scope.Namespace, Collection: state.Scope.Collection}}
		var position uint64
		if err = rows.Scan(&version.Ref.Key, &version.Revision, &position); err != nil {
			rows.Close()
			return memory.Unavailable
		}
		if position == 0 || erased[version].Position != position {
			rows.Close()
			return memory.IdentityConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.Unavailable
	}
	for _, f := range state.SourceFences {
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_source_fences VALUES(?,?,?,?) ON CONFLICT(namespace,kind,source_key) DO UPDATE SET revision=excluded.revision`, f.Namespace, f.Kind, f.Key, strconv.FormatUint(f.ThroughRevision, 10)); err != nil {
			return memory.Unavailable
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_source_fences`).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count > 512 {
		return memory.Capacity
	}
	return nil
}
