package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func triggerEventKey(e *wire.ContentSource) (string, error) {
	if e == nil || e.Kind == "" || len(e.Kind) > 128 || !name(e.Key) || e.Revision == 0 || len(e.ProtoReflect().GetUnknown()) != 0 {
		return "", memory.Invalid
	}
	raw, err := json.Marshal([]any{e.Kind, e.Key, e.Revision})
	if err != nil {
		return "", memory.Invalid
	}
	return string(raw), nil
}
func (s *Store) ReserveTriggerRound(ctx context.Context, ns, subject, id string, now int64, r extraction.TriggerRound) error {
	key, err := triggerEventKey(r.Event)
	q := r.Submission
	if err != nil || !name(ns) || !name(subject) || !name(id) || now <= 0 || q.Namespace != ns || !name(q.OperationID) || q.Goal == "" || len(q.Goal) > 1024 || len(q.InputRefs) != 1 || q.InputRefs[0] == "" || len(q.InputRefs[0]) > 2048 || q.Constraints.MaxSteps == 0 || q.Constraints.ModelRequests != 0 || q.Constraints.ModelTokens != 0 {
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
	var owner, state string
	var document []byte
	err = tx.QueryRowContext(ctx, `SELECT subject,state,document FROM extraction_triggers WHERE namespace=? AND trigger_id=?`, ns, id).Scan(&owner, &state, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Missing
	}
	if err != nil {
		return memory.Unavailable
	}
	if owner != subject {
		return memory.Denied
	}
	if state != "active" {
		return memory.ReplayUnavailable
	}
	var registration extraction.TriggerRecord
	if len(document) > maxDocument || json.Unmarshal(document, &registration) != nil || !extraction.ValidTriggerSpec(registration.Spec) {
		return memory.Unavailable
	}
	allowed := false
	for _, scope := range registration.Spec.Sources {
		if scope.Kind == r.Event.Kind && scope.Key == r.Event.Key {
			allowed = true
			break
		}
	}
	if !allowed {
		return memory.Denied
	}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT document FROM trigger_rounds WHERE namespace=? AND trigger_id=? AND event_key=?`, ns, id, key).Scan(&old)
	if err == nil {
		if string(old) != string(raw) {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	if registration.Spec.ExpiresUnix <= now || q.Constraints.DeadlineUnix <= now || q.Constraints.DeadlineUnix > registration.Spec.ExpiresUnix || uint64(q.Constraints.MaxSteps) > registration.Spec.MaxSteps {
		return memory.Denied
	}
	through, err := sourceFence(ctx, tx, ns, r.Event.Kind, r.Event.Key)
	if err != nil {
		return err
	}
	if r.Event.Revision <= through {
		return memory.ReplayUnavailable
	}
	var used, total, reused int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM trigger_rounds WHERE namespace=? AND trigger_id=?`, ns, id).Scan(&used); err != nil {
		return memory.Unavailable
	}
	if uint64(used) >= registration.Spec.MaxRounds {
		return memory.Capacity
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM trigger_rounds`).Scan(&total); err != nil {
		return memory.Unavailable
	}
	if total >= maxRecords {
		return memory.Capacity
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM trigger_rounds WHERE namespace=? AND submit_operation=?`, ns, q.OperationID).Scan(&reused); err != nil {
		return memory.Unavailable
	}
	if reused != 0 {
		return memory.IdentityConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO trigger_rounds(namespace,trigger_id,event_key,subject,submit_operation,document) VALUES(?,?,?,?,?,?)`, ns, id, key, subject, q.OperationID, raw); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
func (s *Store) GetTriggerRound(ctx context.Context, ns, subject, id string, event *wire.ContentSource) (extraction.TriggerRound, error) {
	key, err := triggerEventKey(event)
	if err != nil || !name(ns) || !name(subject) || !name(id) {
		return extraction.TriggerRound{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var raw []byte
	var owner, operation string
	err = s.db.QueryRowContext(ctx, `SELECT subject,submit_operation,document FROM trigger_rounds WHERE namespace=? AND trigger_id=? AND event_key=?`, ns, id, key).Scan(&owner, &operation, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return extraction.TriggerRound{}, memory.Missing
	}
	if err != nil || len(raw) > maxDocument {
		return extraction.TriggerRound{}, memory.Unavailable
	}
	if owner != subject {
		return extraction.TriggerRound{}, memory.Denied
	}
	var r extraction.TriggerRound
	if json.Unmarshal(raw, &r) != nil || r.Submission.Namespace != ns || r.Submission.OperationID != operation {
		return extraction.TriggerRound{}, memory.Unavailable
	}
	actual, err := triggerEventKey(r.Event)
	if err != nil || actual != key {
		return extraction.TriggerRound{}, memory.Unavailable
	}
	return r, nil
}
