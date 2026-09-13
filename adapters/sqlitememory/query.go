package sqlitememory

import (
	"context"
	"encoding/json"
	"lerna/memory"
)

func (s *Store) Scan(ctx context.Context, namespace, collection string) ([]memory.Revision, error) {
	if !label(namespace) || !label(collection) {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	rows, e := s.db.QueryContext(ctx, `SELECT r.record_key,r.revision,r.document FROM memory_revisions r WHERE r.namespace=? AND r.collection=? AND r.revision=(SELECT MAX(n.revision) FROM memory_operations n WHERE n.namespace=r.namespace AND n.collection=r.collection AND n.record_key=r.record_key) ORDER BY r.record_key LIMIT 513`, namespace, collection)
	if e != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	result := []memory.Revision{}
	for rows.Next() {
		r := memory.Revision{Ref: memory.Ref{Namespace: namespace, Collection: collection}}
		if rows.Scan(&r.Ref.Key, &r.Revision, &r.Document) != nil || !validRef(r.Ref) || r.Revision == 0 || len(r.Document) > MaxDocument || !json.Valid(r.Document) {
			return nil, memory.Unavailable
		}
		result = append(result, r)
		if len(result) > MaxRevisions {
			return nil, memory.Capacity
		}
	}
	if rows.Err() != nil {
		return nil, memory.Unavailable
	}
	return result, nil
}
