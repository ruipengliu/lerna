package decisionfixture

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ContextInputLockObservation is an acknowledgement of a real held row lock.
// It carries native synchronization facts, never publication permission.
type ContextInputLockObservation struct {
	InputID    string
	BackendPID int
	At         time.Time
}

// HoldContextInputRow holds the original input in the unchanged short fixture
// transaction. It neither updates the input nor renews any business deadline.
func (d *ContextDispatcher) HoldContextInputRow(ctx context.Context, inputID string, held chan<- ContextInputLockObservation, release <-chan struct{}) error {
	if inputID == "" || held == nil || release == nil {
		return errors.New("actual input row lock synchronization required")
	}
	return d.Store.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		if err := d.trusted(ctx, tx); err != nil {
			return err
		}
		observation := ContextInputLockObservation{}
		if err := tx.QueryRowContext(ctx, `SELECT input_id FROM `+d.Store.table("fixture_context_inputs")+` WHERE input_id=$1 FOR UPDATE`, inputID).Scan(&observation.InputID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid(),clock_timestamp()`).Scan(&observation.BackendPID, &observation.At); err != nil {
			return err
		}
		select {
		case held <- observation:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// WaitContextInputRowBlocked observes only PG lock machinery for the two
// already known native backends. It is not a private business-state oracle.
func (d *ContextDispatcher) WaitContextInputRowBlocked(ctx context.Context, contenderPID, blockerPID int) error {
	if contenderPID <= 0 || blockerPID <= 0 || contenderPID == blockerPID {
		return errors.New("distinct known native input lock backends required")
	}
	bounded, cancel := context.WithTimeout(ctx, d.Store.cfg.TransactionTimeout)
	defer cancel()
	for {
		var waiting bool
		if err := d.Store.db.QueryRowContext(bounded, `SELECT $2=ANY(pg_blocking_pids($1)) AND EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, contenderPID, blockerPID).Scan(&waiting); err != nil {
			return err
		}
		if waiting {
			return nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-timer.C:
		case <-bounded.Done():
			timer.Stop()
			return bounded.Err()
		}
	}
}
