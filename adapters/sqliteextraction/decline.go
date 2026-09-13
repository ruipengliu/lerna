package sqliteextraction

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"lerna/extraction"
	"lerna/memory"
)

var _ extraction.DeclineStore = (*Store)(nil)

func (s *Store) Decline(ctx context.Context, fact extraction.CandidateState) error {
	hash, err := hex.DecodeString(fact.InvocationSHA256)
	if !name(fact.Namespace) || !name(fact.Subject) || !name(fact.OperationID) || fact.State != "unsupported" || fact.Committed || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != fact.InvocationSHA256 {
		return memory.Invalid
	}
	switch fact.Reason {
	case "unsupported", "insufficient_evidence", "inconsistent_evidence":
	default:
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
	var subject, invocation, reason string
	var committed bool
	err = tx.QueryRowContext(ctx, `SELECT subject,committed,invocation,reason FROM retired_candidates WHERE namespace=? AND operation=?`, fact.Namespace, fact.OperationID).Scan(&subject, &committed, &invocation, &reason)
	if err == nil {
		if subject != fact.Subject || committed || invocation != fact.InvocationSHA256 || reason != fact.Reason {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM candidates WHERE namespace=? AND operation=?`, fact.Namespace, fact.OperationID).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count != 0 {
		return memory.IdentityConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM candidates)+(SELECT count(*) FROM retired_candidates)`).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count >= maxRecords {
		return memory.Capacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO retired_candidates(namespace,operation,subject,committed,invocation,reason) VALUES(?,?,?,0,?,?)`, fact.Namespace, fact.OperationID, fact.Subject, fact.InvocationSHA256, fact.Reason); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
