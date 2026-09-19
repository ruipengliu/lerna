package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"lerna/extraction"
	"lerna/memory"
)

var _ extraction.CandidateLifecycle = (*Store)(nil)

func (s *Store) Retire(ctx context.Context, namespace, subject, operation string) error {
	if !name(namespace) || !name(subject) || !name(operation) {
		return memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_lock SET version=version WHERE id=1`); err != nil {
		return memory.Unavailable
	}
	var oldSubject string
	err = tx.QueryRowContext(ctx, `SELECT subject FROM retired_candidates WHERE namespace=? AND operation=?`, namespace, operation).Scan(&oldSubject)
	if err == nil {
		if oldSubject != subject {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	committed := false
	var invocation string
	err = tx.QueryRowContext(ctx, `SELECT json_extract(document,'$.Subject'),COALESCE(json_extract(document,'$.InvocationSHA256'),'') FROM candidates WHERE namespace=? AND operation=?`, namespace, operation).Scan(&oldSubject, &invocation)
	if err == nil {
		if oldSubject != subject {
			return memory.IdentityConflict
		}
		committed = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	if !committed {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM candidates)+(SELECT count(*) FROM retired_candidates)`).Scan(&count); err != nil {
			return memory.Unavailable
		}
		if count >= maxRecords {
			return memory.Capacity
		}
	}
	if err = retireCandidate(ctx, tx, retirementFact{namespace, operation, subject, invocation, committed}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
func (s *Store) Inspect(ctx context.Context, namespace, operation string) (extraction.CandidateState, error) {
	if !name(namespace) || !name(operation) {
		return extraction.CandidateState{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := extraction.CandidateState{Namespace: namespace, OperationID: operation}
	// One snapshot distinguishes committed bodies from pre-commit retirement.
	err := s.db.QueryRowContext(ctx, `SELECT subject,CASE WHEN reason='' THEN 'retired' ELSE 'unsupported' END,committed,invocation,reason FROM retired_candidates WHERE namespace=? AND operation=? UNION ALL SELECT json_extract(document,'$.Subject'),'retained',1,COALESCE(json_extract(document,'$.InvocationSHA256'),''),'' FROM candidates WHERE namespace=? AND operation=?`, namespace, operation, namespace, operation).Scan(&out.Subject, &out.State, &out.Committed, &out.InvocationSHA256, &out.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return extraction.CandidateState{}, memory.Missing
	}
	if err != nil {
		return extraction.CandidateState{}, memory.Unavailable
	}
	return out, nil
}

// All retirement entry points hold the candidate write lock and select their
// own authorized scope. Keep erasure and original-fact preservation atomic.
type retirementFact struct {
	namespace, operation, subject, invocation string
	committed                                 bool
}

func retireCandidate(ctx context.Context, tx *sql.Tx, f retirementFact) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO retired_candidates(namespace,operation,subject,committed,invocation) VALUES(?,?,?,?,?)`, f.namespace, f.operation, f.subject, f.committed, f.invocation); err != nil {
		return memory.Unavailable
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candidate_saves SET state='retired',document=identity WHERE namespace=? AND candidate=?`, f.namespace, f.operation); err != nil {
		return memory.Unavailable
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE namespace=? AND operation=?`, f.namespace, f.operation); err != nil {
		return memory.Unavailable
	}
	return nil
}
