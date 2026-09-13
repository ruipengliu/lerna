package sqlitememory

import (
	"context"
	"database/sql"
	"google.golang.org/protobuf/encoding/protojson"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"strconv"
)

func revisionSources(document []byte) ([]*wire.MemorySource, error) {
	record := new(wire.MemoryRecord)
	// Store is trusted and also serves body-agnostic contract fixtures. Unknown
	// fields are irrelevant here; any declared source must still decode exactly.
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(document, record); err != nil {
		return nil, memory.Invalid
	}
	sources := record.GetSpec().GetSources()
	for _, s := range sources {
		if s.GetRef() == nil || !label(s.Ref.Kind) || !label(s.Ref.Key) || s.Ref.Revision == 0 {
			return nil, memory.Invalid
		}
	}
	return sources, nil
}
func sourceAllowed(ctx context.Context, tx *sql.Tx, namespace string, document []byte) error {
	sources, err := revisionSources(document)
	if err != nil {
		return err
	}
	for _, s := range sources {
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT revision FROM memory_source_fences WHERE namespace=? AND kind=? AND source_key=?`, namespace, s.Ref.Kind, s.Ref.Key).Scan(&raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return memory.Unavailable
		}
		through, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || through == 0 {
			return memory.Unavailable
		}
		if s.Ref.Revision <= through {
			return memory.Denied
		}
	}
	return nil
}
func (s *Store) EraseSource(ctx context.Context, in memory.SourceErasure) (int, error) {
	if !label(in.Namespace) || !label(in.Kind) || !label(in.Key) || in.ThroughRevision == 0 {
		return 0, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return 0, memory.Unavailable
	}
	var old string
	err = tx.QueryRowContext(ctx, `SELECT revision FROM memory_source_fences WHERE namespace=? AND kind=? AND source_key=?`, in.Namespace, in.Kind, in.Key).Scan(&old)
	through := in.ThroughRevision
	if err == nil {
		n, e := strconv.ParseUint(old, 10, 64)
		if e != nil || n == 0 {
			return 0, memory.Unavailable
		}
		if n > through {
			through = n
		}
	} else if err == sql.ErrNoRows {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_source_fences`).Scan(&count); e != nil {
			return 0, memory.Unavailable
		}
		if count >= MaxRevisions {
			return 0, memory.Capacity
		}
	} else {
		return 0, memory.Unavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_source_fences VALUES(?,?,?,?) ON CONFLICT(namespace,kind,source_key) DO UPDATE SET revision=excluded.revision`, in.Namespace, in.Kind, in.Key, strconv.FormatUint(through, 10)); err != nil {
		return 0, memory.Unavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT collection,record_key,revision,document FROM memory_revisions WHERE namespace=?`, in.Namespace)
	if err != nil {
		return 0, memory.Unavailable
	}
	var affected []memory.VersionRef
	count := 0
	for rows.Next() {
		count++
		if count > MaxRevisions {
			rows.Close()
			return 0, memory.Capacity
		}
		v := memory.VersionRef{Ref: memory.Ref{Namespace: in.Namespace}}
		var raw []byte
		if err = rows.Scan(&v.Ref.Collection, &v.Ref.Key, &v.Revision, &raw); err != nil {
			rows.Close()
			return 0, memory.Unavailable
		}
		sources, e := revisionSources(raw)
		if e != nil {
			rows.Close()
			return 0, memory.Unavailable
		}
		for _, source := range sources {
			if source.Ref.Kind == in.Kind && source.Ref.Key == in.Key && source.Ref.Revision <= through {
				affected = append(affected, v)
				break
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, memory.Unavailable
	}
	for _, v := range affected {
		var position uint64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=?)`, v.Ref.Namespace, v.Ref.Collection, v.Ref.Namespace, v.Ref.Collection).Scan(&position); err != nil {
			return 0, memory.Unavailable
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memory_erasure_events VALUES(?,?,?,?,?)`, v.Ref.Namespace, v.Ref.Collection, v.Ref.Key, v.Revision, position); err != nil {
			return 0, memory.Unavailable
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, v.Ref.Namespace, v.Ref.Collection, v.Ref.Key, v.Revision); err != nil {
			return 0, memory.Unavailable
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_operations SET semantic='' WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, v.Ref.Namespace, v.Ref.Collection, v.Ref.Key, v.Revision); err != nil {
			return 0, memory.Unavailable
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, memory.Unavailable
	}
	return len(affected), nil
}

var _ memory.SourceErasureStore = (*Store)(nil)
