package sqlitefetch

import (
	"context"
	"database/sql"
	"encoding/json"
	"lerna/answers"
	"lerna/fetch"
)

var _ fetch.OutcomeStore = (*Store)(nil)

func validOutcome(in fetch.AttemptIntent, out fetch.Outcome) bool {
	if out.Requests > in.MaxRequests || (out.Mode != "" && out.Mode != "http" && out.Mode != "fixed-replay") || (out.Mode == "fixed-replay" && out.Requests != 0) {
		return false
	}
	if out.Status == "acquired" {
		if (out.Requests == 0 && out.Mode != "fixed-replay") || len(out.Reference) > 256 {
			return false
		}
		ref, err := answers.ParseReference(out.Reference)
		return err == nil && ref.Namespace == in.Task.Namespace
	}
	if out.Reference != "" {
		return false
	}
	return fetch.IsFailureStatus(out.Status)
}
func lookupOutcome(ctx context.Context, q reader, in fetch.AttemptIntent) (fetch.Outcome, bool, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT outcome FROM fetch_outcomes WHERE namespace=? AND operation=?`, in.Task.Namespace, in.OperationID).Scan(&raw)
	if err == sql.ErrNoRows {
		return fetch.Outcome{}, false, nil
	}
	if err != nil || len(raw) > 1024 {
		return fetch.Outcome{}, false, fetch.Unavailable
	}
	var out fetch.Outcome
	if json.Unmarshal(raw, &out) != nil || !validOutcome(in, out) {
		return fetch.Outcome{}, false, fetch.Unavailable
	}
	return out, true, nil
}
func (s *Store) Outcome(ctx context.Context, namespace, operation string) (fetch.Outcome, bool, error) {
	if !name(namespace) || !name(operation) {
		return fetch.Outcome{}, false, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	in, err := inspect(ctx, s.db, namespace, operation)
	if err != nil {
		return fetch.Outcome{}, false, err
	}
	return lookupOutcome(ctx, s.db, in)
}
func (s *Store) Complete(ctx context.Context, in fetch.AttemptIntent, out fetch.Outcome) error {
	if !valid(in) || !validOutcome(in, out) {
		return fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fetch.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE fetch_lock SET version=version WHERE id=1`); err != nil {
		return fetch.Unavailable
	}
	original, err := inspect(ctx, tx, in.Task.Namespace, in.OperationID)
	if err != nil {
		return err
	}
	if original != in {
		return fetch.IdentityConflict
	}
	old, known, err := lookupOutcome(ctx, tx, in)
	if err != nil {
		return err
	}
	if known {
		if old != out {
			return fetch.IdentityConflict
		}
		return nil
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > 1024 {
		return fetch.Invalid
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fetch_outcomes VALUES(?,?,?)`, in.Task.Namespace, in.OperationID, raw); err != nil {
		return fetch.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return fetch.Unavailable
	}
	return nil
}
