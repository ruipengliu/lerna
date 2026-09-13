package sqliteextraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"lerna/extraction"
	"lerna/memory"
)

func (s *Store) RegisterTrigger(ctx context.Context, r extraction.TriggerRecord) error {
	if !name(r.Namespace) || !name(r.Subject) || !name(r.Location) || !name(r.Purpose) || r.State != "active" || !extraction.ValidTriggerSpec(r.Spec) {
		return memory.Invalid
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > maxDocument {
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
	var old []byte
	var subject, state string
	err = tx.QueryRowContext(ctx, `SELECT subject,state,document FROM extraction_triggers WHERE namespace=? AND trigger_id=?`, r.Namespace, r.Spec.ID).Scan(&subject, &state, &old)
	if err == nil {
		if subject != r.Subject {
			return memory.Denied
		}
		if state == "cancelled" {
			return memory.ReplayUnavailable
		}
		if string(old) != string(raw) {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM extraction_triggers`).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count >= 64 {
		return memory.Capacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO extraction_triggers(namespace,trigger_id,subject,state,document) VALUES(?,?,?,'active',?)`, r.Namespace, r.Spec.ID, r.Subject, raw); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
func (s *Store) GetTrigger(ctx context.Context, namespace, subject, id string) (extraction.TriggerRecord, error) {
	if !name(namespace) || !name(subject) || !name(id) {
		return extraction.TriggerRecord{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var raw []byte
	var owner, state string
	err := s.db.QueryRowContext(ctx, `SELECT subject,state,document FROM extraction_triggers WHERE namespace=? AND trigger_id=?`, namespace, id).Scan(&owner, &state, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return extraction.TriggerRecord{}, memory.Missing
	}
	if err != nil || len(raw) > maxDocument {
		return extraction.TriggerRecord{}, memory.Unavailable
	}
	if owner != subject {
		return extraction.TriggerRecord{}, memory.Denied
	}
	var r extraction.TriggerRecord
	if json.Unmarshal(raw, &r) != nil || r.Namespace != namespace || r.Subject != subject || r.Spec.ID != id || !extraction.ValidTriggerSpec(r.Spec) || (state != "active" && state != "cancelled") {
		return extraction.TriggerRecord{}, memory.Unavailable
	}
	r.State = state
	return r, nil
}
func (s *Store) CancelTrigger(ctx context.Context, namespace, subject, id string) error {
	if !name(namespace) || !name(subject) || !name(id) {
		return memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE extraction_triggers SET state='cancelled' WHERE namespace=? AND subject=? AND trigger_id=?`, namespace, subject, id)
	if err != nil {
		return memory.Unavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return memory.Unavailable
	}
	if count != 1 {
		return memory.Denied
	}
	return nil
}
