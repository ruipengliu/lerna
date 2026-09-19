package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"strconv"
)

var _ extraction.CandidateInvalidator = (*Store)(nil)

func (s *Store) CheckSource(ctx context.Context, namespace string, ref *wire.ContentSource) error {
	if !name(namespace) || ref == nil || !name(ref.Kind) || !name(ref.Key) || ref.Revision == 0 {
		return memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM candidate_source_fences WHERE namespace=? AND kind=? AND source_key=?`, namespace, ref.Kind, ref.Key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return memory.Unavailable
	}
	through, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || through == 0 {
		return memory.Unavailable
	}
	if ref.Revision <= through {
		return memory.Denied
	}
	return nil
}

func sourceFence(ctx context.Context, tx *sql.Tx, namespace, kind, key string) (uint64, error) {
	var text string
	err := tx.QueryRowContext(ctx, `SELECT revision FROM candidate_source_fences WHERE namespace=? AND kind=? AND source_key=?`, namespace, kind, key).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, memory.Unavailable
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil || n == 0 {
		return 0, memory.Unavailable
	}
	return n, nil
}

func (s *Store) InvalidateSource(ctx context.Context, in extraction.SourceInvalidation) (int, error) {
	if !name(in.Namespace) || !name(in.Kind) || !name(in.Key) || in.ThroughRevision == 0 {
		return 0, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_lock SET version=version WHERE id=1`); err != nil {
		return 0, memory.Unavailable
	}
	previous, err := sourceFence(ctx, tx, in.Namespace, in.Kind, in.Key)
	if err != nil {
		return 0, err
	}
	if previous == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM candidate_source_fences`).Scan(&count); err != nil {
			return 0, memory.Unavailable
		}
		if count >= maxRecords {
			return 0, memory.Capacity
		}
	}
	through := max(previous, in.ThroughRevision)
	if _, err = tx.ExecContext(ctx, `INSERT INTO candidate_source_fences(namespace,kind,source_key,revision) VALUES(?,?,?,?) ON CONFLICT(namespace,kind,source_key) DO UPDATE SET revision=excluded.revision`, in.Namespace, in.Kind, in.Key, strconv.FormatUint(through, 10)); err != nil {
		return 0, memory.Unavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT document FROM candidates WHERE namespace=?`, in.Namespace)
	if err != nil {
		return 0, memory.Unavailable
	}
	var affected []extraction.CandidateRecord
	count := 0
	for rows.Next() {
		count++
		var raw []byte
		if err = rows.Scan(&raw); err != nil || len(raw) > maxDocument || count > maxRecords {
			rows.Close()
			return 0, memory.Unavailable
		}
		var r extraction.CandidateRecord
		if json.Unmarshal(raw, &r) != nil || r.Namespace != in.Namespace {
			rows.Close()
			return 0, memory.Unavailable
		}
		if _, err = encode(r); err != nil {
			rows.Close()
			return 0, memory.Unavailable
		}
		for _, source := range r.Candidate.Sources {
			if source.Ref.Kind == in.Kind && source.Ref.Key == in.Key && source.Ref.Revision <= through {
				affected = append(affected, r)
				break
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, memory.Unavailable
	}
	for _, r := range affected {
		if err = retireCandidate(ctx, tx, retirementFact{r.Namespace, r.OperationID, r.Subject, r.InvocationSHA256, true}); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, memory.Unavailable
	}
	return len(affected), nil
}
