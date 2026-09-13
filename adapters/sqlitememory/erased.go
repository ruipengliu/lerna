package sqlitememory

import (
	"context"
	"database/sql"
	"lerna/memory"
)

func (s *Store) ErasedOperations(ctx context.Context, ref memory.Ref, deletion uint64) ([]memory.Receipt, error) {
	if !validRef(ref) || deletion < 2 || deletion > MaxRevisions+1 {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	// Each record retains all revisions until deletion, so its erased write
	// count is bounded by MaxRevisions. Match the permanent deletion in the same
	// query snapshot; a caller-provided event alone cannot prove erasure.
	rows, err := s.db.QueryContext(ctx, `SELECT o.operation,o.subject,o.semantic,o.revision,o.position FROM memory_operations o JOIN memory_tombstones t ON o.namespace=t.namespace AND o.collection=t.collection AND o.record_key=t.record_key WHERE o.namespace=? AND o.collection=? AND o.record_key=? AND t.revision=? AND o.revision<t.revision ORDER BY o.revision LIMIT ?`, ref.Namespace, ref.Collection, ref.Key, deletion, MaxRevisions+1)
	if err != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	var out []memory.Receipt
	for rows.Next() {
		r := memory.Receipt{Ref: ref}
		if rows.Scan(&r.OperationID, &r.Subject, &r.SemanticSHA256, &r.Revision, &r.Position) != nil || !label(r.OperationID) || !label(r.Subject) || r.SemanticSHA256 != "" || r.Revision != uint64(len(out)+1) || len(out) >= MaxRevisions {
			return nil, memory.Unavailable
		}
		out = append(out, r)
	}
	if rows.Err() != nil {
		return nil, memory.Unavailable
	}
	if uint64(len(out)) != deletion-1 {
		return nil, memory.Conflict
	}
	return out, nil
}

var _ memory.ErasedOperationStore = (*Store)(nil)

func (s *Store) ErasedRevisionOperation(ctx context.Context, event memory.SourceEvent) (memory.Receipt, error) {
	if !memory.ValidSourceEvent(event) || event.Kind != memory.SourceErased {
		return memory.Receipt{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	r := memory.Receipt{Ref: event.Ref}
	err := s.db.QueryRowContext(ctx, `SELECT o.operation,o.subject,o.semantic,o.revision,o.position FROM memory_operations o JOIN memory_erasure_events e ON o.namespace=e.namespace AND o.collection=e.collection AND o.record_key=e.record_key AND o.revision=e.revision WHERE e.namespace=? AND e.collection=? AND e.record_key=? AND e.revision=? AND e.position=?`, event.Ref.Namespace, event.Ref.Collection, event.Ref.Key, event.Revision, event.Position).Scan(&r.OperationID, &r.Subject, &r.SemanticSHA256, &r.Revision, &r.Position)
	if err == sql.ErrNoRows {
		return memory.Receipt{}, memory.Conflict
	}
	if err != nil || !label(r.OperationID) || !label(r.Subject) || r.SemanticSHA256 != "" || r.Revision != event.Revision || r.Position == 0 || r.Position >= event.Position {
		return memory.Receipt{}, memory.Unavailable
	}
	return r, nil
}

var _ memory.ErasedRevisionStore = (*Store)(nil)
