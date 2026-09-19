package sqlite

import (
	"context"
	"database/sql"
	"lerna/memory"
	"strconv"
)

func readRecoveryErasures(ctx context.Context, tx *sql.Tx, out *memory.RecoverySnapshot) error {
	scope := out.Scope
	rows, err := tx.QueryContext(ctx, `SELECT record_key,revision,position FROM memory_erasure_events WHERE namespace=? AND collection=? ORDER BY position LIMIT 513`, scope.Namespace, scope.Collection)
	if err != nil {
		return memory.Unavailable
	}
	for rows.Next() {
		e := memory.SourceEvent{Ref: memory.Ref{Namespace: scope.Namespace, Collection: scope.Collection}, Kind: memory.SourceErased}
		if err = rows.Scan(&e.Ref.Key, &e.Revision, &e.Position); err != nil {
			rows.Close()
			return memory.Unavailable
		}
		out.Erased = append(out.Erased, e)
		if len(out.Erased) > MaxRevisions {
			rows.Close()
			return memory.Capacity
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return memory.Unavailable
	}
	rows, err = tx.QueryContext(ctx, `SELECT kind,source_key,revision FROM memory_source_fences WHERE namespace=? ORDER BY kind,source_key LIMIT 513`, scope.Namespace)
	if err != nil {
		return memory.Unavailable
	}
	defer rows.Close()
	for rows.Next() {
		f := memory.SourceErasure{Namespace: scope.Namespace}
		var raw string
		if err = rows.Scan(&f.Kind, &f.Key, &raw); err != nil {
			return memory.Unavailable
		}
		f.ThroughRevision, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return memory.Unavailable
		}
		out.SourceFences = append(out.SourceFences, f)
		if len(out.SourceFences) > MaxRevisions {
			return memory.Capacity
		}
	}
	if rows.Err() != nil {
		return memory.Unavailable
	}
	return nil
}
