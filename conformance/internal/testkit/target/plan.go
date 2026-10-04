package target

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	ReceiveOnly            Kind = "receive"
	ApplyReceived          Kind = "apply"
	WriteNormally          Kind = "write"
	DisconnectBeforeCommit Kind = "before_commit"
	DropResponse           Kind = "drop_response"
)

type Step struct {
	ID    string
	Kind  Kind
	Input Request
}
type Plan struct {
	ID       string
	Seed     uint64
	Deadline time.Time
	Steps    []Step
}
type Event struct {
	ID             string
	Step           int
	Kind           Kind
	Outcome, Phase string
}
type PlanState struct {
	Plan   Plan
	Cursor int
	Events []Event
}

var ErrResponseLost = errors.New("injected_response_lost_after_commit")
var ErrDisconnected = errors.New("injected_disconnect_before_commit")
var ErrPlanConflict = errors.New("plan_configuration_conflict")
var ErrOutOfOrder = errors.New("event_out_of_order")
var ErrPlanExpired = errors.New("plan_deadline_expired")

func validatePlan(p Plan) error {
	if !utf8.ValidString(p.ID) || p.ID == "" || len(p.ID) > 128 || len(p.Steps) == 0 || len(p.Steps) > 64 || p.Deadline.IsZero() || p.Deadline.Before(time.Unix(0, math.MinInt64)) || p.Deadline.After(time.Unix(0, math.MaxInt64)) {
		return errors.New("invalid bounded test plan")
	}
	ids := map[string]bool{}
	total := 0
	for _, step := range p.Steps {
		if !utf8.ValidString(step.ID) || step.ID == "" || len(step.ID) > 128 || ids[step.ID] || (step.Kind != ReceiveOnly && step.Kind != ApplyReceived && step.Kind != WriteNormally && step.Kind != DisconnectBeforeCommit && step.Kind != DropResponse) {
			return errors.New("invalid bounded plan event")
		}
		ids[step.ID] = true
		if err := validateRequest(step.Input); err != nil {
			return err
		}
		total += len(step.Input.Data)
	}
	if total > 4*1024*1024 {
		return errors.New("test plan materials exceed finite size")
	}
	return nil
}
func (t *Target) InstallPlan(ctx context.Context, p Plan) (PlanState, error) {
	if err := validatePlan(p); err != nil {
		return PlanState{}, err
	}
	p.Deadline = p.Deadline.UTC()
	encoded, err := json.Marshal(p)
	if err != nil {
		return PlanState{}, err
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return PlanState{}, err
	}
	defer leave()
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return PlanState{}, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, "SELECT digest FROM target_plans WHERE plan_id=?", p.ID).Scan(&stored)
	if err == nil {
		if stored != digest {
			return PlanState{}, ErrPlanConflict
		}
		return readPlan(ctx, tx, p.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PlanState{}, err
	}
	now, _, err := t.window()
	if err != nil {
		return PlanState{}, err
	}
	if !p.Deadline.After(now) || p.Deadline.Sub(now) > 24*time.Hour {
		return PlanState{}, ErrPlanExpired
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO target_plans(plan_id,configuration,digest,cursor) VALUES(?,?,?,0)", p.ID, encoded, digest); err != nil {
		return PlanState{}, err
	}
	if err = tx.Commit(); err != nil {
		return PlanState{}, fmt.Errorf("test plan commit outcome unknown: %w", err)
	}
	return PlanState{Plan: p}, nil
}
func readPlan(ctx context.Context, tx *sql.Tx, id string) (PlanState, error) {
	var out PlanState
	var encoded []byte
	err := tx.QueryRowContext(ctx, "SELECT configuration,cursor FROM target_plans WHERE plan_id=?", id).Scan(&encoded, &out.Cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanState{}, ErrNotFound
	}
	if err != nil {
		return PlanState{}, err
	}
	if err = json.Unmarshal(encoded, &out.Plan); err != nil {
		return PlanState{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT event_id,step,kind,outcome,phase FROM target_plan_events WHERE plan_id=? ORDER BY step", id)
	if err != nil {
		return PlanState{}, err
	}
	for rows.Next() {
		var event Event
		if err = rows.Scan(&event.ID, &event.Step, &event.Kind, &event.Outcome, &event.Phase); err != nil {
			rows.Close()
			return PlanState{}, err
		}
		out.Events = append(out.Events, event)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return PlanState{}, err
	}
	return out, nil
}
func (t *Target) RunEvent(ctx context.Context, id, eventID string) (Event, error) {
	ctx, leave, err := t.enter(ctx)
	if err != nil {
		return Event{}, err
	}
	defer leave()
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	state, err := readPlan(ctx, tx, id)
	if err != nil {
		return Event{}, err
	}
	index := -1
	for i, step := range state.Plan.Steps {
		if step.ID == eventID {
			index = i
			break
		}
	}
	if index < 0 {
		return Event{}, ErrNotFound
	}
	if index < state.Cursor {
		return state.Events[index], eventError(state.Events[index])
	}
	if index > state.Cursor {
		return Event{}, ErrOutOfOrder
	}
	now, _, err := t.window()
	if err != nil {
		return Event{}, err
	}
	if !now.Before(state.Plan.Deadline) {
		return Event{}, ErrPlanExpired
	}
	operation, cancel := context.WithTimeout(ctx, state.Plan.Deadline.Sub(now))
	defer cancel()
	ctx = operation
	step := state.Plan.Steps[index]
	event := Event{ID: step.ID, Step: index, Kind: step.Kind, Outcome: "applied", Phase: "committed"}

	var result Receipt
	switch step.Kind {
	case ReceiveOnly:
		result, err = t.writeTx(ctx, tx, step.Input, true)
	case WriteNormally, DisconnectBeforeCommit, DropResponse:
		result, err = t.writeTx(ctx, tx, step.Input, false)
	case ApplyReceived:
		original, lookupErr := received(ctx, tx, step.Input.Key)
		result, err = applyReceivedTx(ctx, tx, step.Input)
		if lookupErr == nil && original.Value.Version != 0 && err == nil {
			event.Outcome = "already_applied"
		}
	}
	if err != nil {
		outcome := rejectionOutcome(err)
		if outcome == "" {
			return Event{}, err
		}
		event.Outcome = outcome
		event.Phase = "rejected"
	} else if step.Kind != ApplyReceived {
		if err = tx.QueryRowContext(ctx, "SELECT outcome FROM target_receives WHERE original_key=? ORDER BY sequence DESC LIMIT 1", step.Input.Key).Scan(&event.Outcome); err != nil {
			return Event{}, err
		}
		if result.Value.Version == 0 {
			event.Phase = "durably_received"
		}
	}

	if step.Kind == DropResponse && event.Phase != "rejected" {
		event.Outcome = "response_lost"
		event.Phase = "committed_response_lost"
	}
	if step.Kind == DisconnectBeforeCommit && event.Phase != "rejected" {
		// SQL target changes have actually been prepared above, then rolled back.
		// Failure observation/cursor is a separate transaction, not atomic with the
		// rolled-back transaction. A crash in this gap safely retries this rollback.
		if err = tx.Rollback(); err != nil {
			return Event{}, errors.Join(ErrDisconnected, err)
		}
		tx, err = t.db.BeginTx(ctx, nil)
		if err != nil {
			return Event{}, err
		}
		defer tx.Rollback()
		event.Outcome = "disconnected"
		event.Phase = "rolled_back"
	}
	if err = saveEvent(ctx, tx, id, event); err != nil {
		return Event{}, err
	}
	if err = ctx.Err(); err != nil {
		return Event{}, err
	}
	if t.checkpoint != nil {
		if err = t.checkpoint(ctx, "before_commit", event); err != nil {
			return Event{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Event{}, fmt.Errorf("test event commit outcome unknown: %w", err)
	}
	if t.checkpoint != nil {
		if err = t.checkpoint(ctx, "committed_before_reply", event); err != nil {
			return Event{}, err
		}
	}
	return event, eventError(event)
}
func saveEvent(ctx context.Context, tx *sql.Tx, id string, event Event) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO target_plan_events(plan_id,event_id,step,kind,outcome,phase) VALUES(?,?,?,?,?,?)", id, event.ID, event.Step, string(event.Kind), event.Outcome, event.Phase); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE target_plans SET cursor=? WHERE plan_id=? AND cursor=?", event.Step+1, id, event.Step)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("plan cursor changed outside original event")
	}
	return nil
}
func (o *Observer) Plan(ctx context.Context, id string) (PlanState, error) {
	if ctx == nil {
		return PlanState{}, errors.New("context required")
	}
	ctx, cancel := context.WithTimeout(ctx, o.cfg.IOTimeout)
	defer cancel()
	tx, err := o.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PlanState{}, err
	}
	defer tx.Rollback()
	out, err := readPlan(ctx, tx, id)
	if err != nil {
		return PlanState{}, err
	}
	return out, tx.Commit()
}

func rejectionOutcome(err error) string {
	switch {
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrGuaranteeExpired):
		return "guarantee_expired"
	case errors.Is(err, ErrPending):
		return "pending"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	}
	return ""
}
func eventError(event Event) error {
	switch event.Outcome {
	case "disconnected":
		return ErrDisconnected
	case "response_lost":
		return ErrResponseLost
	case "conflict":
		return ErrConflict
	case "guarantee_expired":
		return ErrGuaranteeExpired
	case "pending":
		return ErrPending
	case "not_found":
		return ErrNotFound
	}
	return nil
}
