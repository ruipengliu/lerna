package sqlite

import (
	"context"
	"lerna/extraction"
	"lerna/memory"
)

var _ extraction.CandidateExpiry = (*Store)(nil)

func (s *Store) RetireExpired(ctx context.Context, namespace, subject string, cutoff int64, limit int) (int, error) {
	if !name(namespace) || !name(subject) || cutoff <= 0 || limit < 1 || limit > 16 {
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
	rows, err := tx.QueryContext(ctx, `SELECT operation,COALESCE(json_extract(document,'$.InvocationSHA256'),'') FROM candidates WHERE namespace=? AND json_extract(document,'$.Subject')=? AND json_extract(document,'$.Restrictions.RetainUntil')<=? ORDER BY json_extract(document,'$.Restrictions.RetainUntil'),operation LIMIT ?`, namespace, subject, cutoff, limit)
	if err != nil {
		return 0, memory.Unavailable
	}
	type fact struct{ operation, invocation string }
	var expired []fact
	for rows.Next() {
		var f fact
		if err = rows.Scan(&f.operation, &f.invocation); err != nil {
			rows.Close()
			return 0, memory.Unavailable
		}
		expired = append(expired, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, memory.Unavailable
	}
	for _, f := range expired {
		if err = retireCandidate(ctx, tx, retirementFact{namespace, f.operation, subject, f.invocation, true}); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, memory.Unavailable
	}
	return len(expired), nil
}
