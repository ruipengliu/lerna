package sqlite

import (
	"context"
	"lerna/memory"
)

// ReadEvents classifies the immutable operation history using the deletion
// tombstone committed in the same transaction. It never decodes old bodies.
func (s *Store) ReadEvents(ctx context.Context, namespace, collection string, after uint64, limit int) ([]memory.SourceEvent, error) {
	if !label(namespace) || !label(collection) || after > 1<<63-1 || limit < 1 || limit > 32 {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT o.record_key,o.revision,o.position,
 CASE WHEN t.operation IS NOT NULL THEN 'deleted' WHEN o.revision=1 THEN 'created' ELSE 'corrected' END
 FROM memory_operations o LEFT JOIN memory_tombstones t ON t.namespace=o.namespace AND t.collection=o.collection AND t.record_key=o.record_key AND t.operation=o.operation
 WHERE o.namespace=? AND o.collection=? AND o.position>? UNION ALL SELECT record_key,revision,position,'erased' FROM memory_erasure_events WHERE namespace=? AND collection=? AND position>? ORDER BY position LIMIT ?`, namespace, collection, after, namespace, collection, after, limit)
	if err != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	events := []memory.SourceEvent{}
	previous := after
	for rows.Next() {
		e := memory.SourceEvent{Ref: memory.Ref{Namespace: namespace, Collection: collection}}
		if err = rows.Scan(&e.Ref.Key, &e.Revision, &e.Position, &e.Kind); err != nil || !validRef(e.Ref) || e.Revision == 0 || e.Revision > 1<<32 || e.Position <= previous {
			return nil, memory.Unavailable
		}
		events = append(events, e)
		previous = e.Position
	}
	if rows.Err() != nil {
		return nil, memory.Unavailable
	}
	return events, nil
}

var _ memory.SourceEvents = (*Store)(nil)
