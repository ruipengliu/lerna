package runtime

import (
	"context"
	"time"
)

// ScheduleStore adds short-transaction scheduling to the original Claim store.
// The consumer holds its object lock before calling any mutation here.
type ScheduleStore interface {
	ValidateClaim(context.Context, Tx, Claim, time.Time) error
	ReleaseClaim(context.Context, Tx, Claim, time.Time) error
	DeferClaim(context.Context, Tx, Claim, time.Time, time.Time) error
	NextWake(context.Context, Tx, time.Time, time.Time) (time.Time, error)
	StopRevision(context.Context, Tx, Job, int64) error
	ClaimActive(context.Context, Tx, Job, time.Time) (bool, error)
}

type StepResult struct {
	Processed int
	NextWake  time.Time
	WaitFor   time.Duration
}

// Timer waits outside any database transaction. A test may supply one shared
// controlled clock/timer; production waits use a finite wall timer.
type Timer interface {
	Wait(context.Context, time.Duration) error
}
type WallTimer struct{}

func (WallTimer) Wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 || delay > time.Second {
		return ErrWorkBounds
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
