package durableworkdemo

import (
	"context"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

// Step scans a bounded batch and processes each exact revision. All waits and
// computation happen after the transaction releases its connection and Claim.
func (w *Worker) Step(ctx context.Context, worker string, limit int, lease, fallback time.Duration) (runtime.StepResult, error) {
	result := runtime.StepResult{}
	if fallback < time.Millisecond || fallback > time.Second {
		return result, runtime.ErrWorkBounds
	}
	if w.Permissions == nil {
		return result, ErrPolicy
	}
	batch, err := w.Claim(ctx, worker, limit, lease)
	if err != nil {
		return result, err
	}
	for index, work := range batch {
		if err = w.Process(ctx, work); err != nil {
			for _, pending := range batch[index:] {
				w.returnClaim(pending.Claim)
			}
			return result, err
		}
		result.Processed++
	}
	schedule, ok := w.Claims.(runtime.ScheduleStore)
	if !ok {
		return result, ErrPolicy
	}
	err = w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		result.NextWake, err = schedule.NextWake(ctx, tx, now, now.Add(fallback))
		result.WaitFor = result.NextWake.Sub(now)
		return err
	})
	return result, err
}
func (w *Worker) Run(ctx context.Context, worker string, limit int, lease, fallback time.Duration, timer runtime.Timer) error {
	if err := workContext(ctx); err != nil {
		return err
	}
	if timer == nil {
		return runtime.ErrWorkBounds
	}
	for {
		result, err := w.Step(ctx, worker, limit, lease, fallback)
		if err != nil {
			return err
		}
		if err = timer.Wait(ctx, result.WaitFor); err != nil {
			return err
		}
	}
}
func (w *Worker) Process(ctx context.Context, work Work) error {
	if ctx != nil {
		defer func() {
			if ctx.Err() != nil {
				w.returnClaim(work.Claim)
			}
		}()
	}
	state, started, err := w.Start(ctx, work)
	if err != nil || !started {
		return err
	}
	// Pure computation/fixture classification is outside the short transaction.
	if err = ctx.Err(); err != nil {
		return err
	}
	if state.Policy.PermanentReason != "" {
		return w.Finish(ctx, work, "permanent", state.Policy.PermanentReason, nil)
	}
	if state.Attempts <= state.Policy.TransientFailures {
		return w.Finish(ctx, work, "retry", "fixture_transient", nil)
	}
	projection := Project(work)
	return w.Finish(ctx, work, "success", "", &projection)
}
func (w *Worker) Start(ctx context.Context, work Work) (ScheduleState, bool, error) {
	var state ScheduleState
	started := false
	if w.Permissions == nil {
		return state, false, ErrPolicy
	}
	if err := w.validateWork(work); err != nil {
		return state, false, err
	}
	if err := workContext(ctx); err != nil {
		return state, false, err
	}
	repo, ok := w.Repository.(ScheduleRepository)
	if !ok {
		return state, false, ErrPolicy
	}
	schedule, ok := w.Claims.(runtime.ScheduleStore)
	if !ok {
		return state, false, ErrPolicy
	}
	err := w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		now, allowed, err := w.prepareClaim(ctx, tx, work, schedule)
		if err != nil {
			return err
		}
		stored, err := repo.LoadSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision)
		if err != nil {
			return err
		}
		if stored == nil {
			return ErrPolicy
		}
		state = *stored
		if !allowed {
			return w.closeState(ctx, tx, work, &state, "permanent", "forbidden", now, nil)
		}
		if state.Stopped {
			return w.closeState(ctx, tx, work, &state, "stopped", "trusted_control_stop", now, nil)
		}
		if !now.Before(state.Deadline) {
			return w.closeState(ctx, tx, work, &state, "expired", "deadline", now, nil)
		}
		if state.Policy.Gate != nil {
			gate, err := repo.GateRevision(ctx, tx, w.Owner, state.Policy.Gate.ID)
			if err != nil {
				return err
			}
			if gate < state.Policy.Gate.Revision {
				state.Outcome = "waiting"
				state.Reason = "gate_revision_at_least"
				state.Due = boundedDue(now, state.Policy.Recheck, state.Deadline)
				if err = repo.SaveSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision, state); err != nil {
					return err
				}
				return schedule.DeferClaim(ctx, tx, work.Claim, now, state.Due)
			}
		}
		if state.StartEpoch == work.Claim.Epoch {
			started = true
			return nil
		}
		if state.Attempts >= state.Policy.MaxAttempts {
			return w.closeState(ctx, tx, work, &state, "permanent", "attempts_exhausted", now, nil)
		}
		state.Attempts++
		state.StartEpoch = work.Claim.Epoch
		state.Outcome = "running"
		state.Reason = ""
		started = true
		return repo.SaveSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision, state)
	})
	return state, started, err
}

// prepareClaim shares the Start/Finish transaction lock order and authority
// checks. Job acquisition may wait, so both Claim gates must be repeated with
// trusted time after the input and Job locks have been acquired.
func (w *Worker) prepareClaim(ctx context.Context, tx runtime.Tx, work Work, schedule runtime.ScheduleStore) (time.Time, bool, error) {
	poolRepo, pool, err := poolLock(ctx, tx, w.Repository)
	if err != nil {
		return time.Time{}, false, err
	}
	input, err := w.Repository.LockInput(ctx, tx, w.Owner, work.Input.ID)
	if err != nil {
		return time.Time{}, false, err
	}
	if input == nil {
		return time.Time{}, false, runtime.ErrClaim
	}
	now, err := w.Clock.Now(ctx, tx)
	if err != nil {
		return time.Time{}, false, err
	}
	if err = poolRepo.ValidatePoolClaim(ctx, tx, pool, work.Claim, now); err != nil {
		return time.Time{}, false, err
	}
	if err = schedule.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
		return time.Time{}, false, err
	}
	now, err = w.Clock.Now(ctx, tx)
	if err != nil {
		return time.Time{}, false, err
	}
	if err = poolRepo.ValidatePoolClaim(ctx, tx, pool, work.Claim, now); err != nil {
		return time.Time{}, false, err
	}
	if err = schedule.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
		return time.Time{}, false, err
	}
	return now, w.Permissions.Allows(work.Claim.Worker), nil
}

func boundedDue(now time.Time, delay time.Duration, deadline time.Time) time.Time {
	due := now.Add(delay)
	if due.After(deadline) {
		due = deadline
	}
	return due
}
func (w *Worker) Finish(ctx context.Context, work Work, outcome, reason string, projection *Projection) error {
	if err := w.validateWork(work); err != nil {
		return err
	}
	if w.Permissions == nil {
		return ErrPolicy
	}
	if err := workContext(ctx); err != nil {
		return err
	}
	repo, ok := w.Repository.(ScheduleRepository)
	if !ok {
		return ErrPolicy
	}
	schedule, ok := w.Claims.(runtime.ScheduleStore)
	if !ok {
		return ErrPolicy
	}
	return w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		now, allowed, err := w.prepareClaim(ctx, tx, work, schedule)
		if err != nil {
			return err
		}
		state, err := repo.LoadSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision)
		if err != nil {
			return err
		}
		if state == nil || state.Stopped || state.StartEpoch != work.Claim.Epoch {
			return runtime.ErrClaim
		}
		if !allowed {
			outcome = "permanent"
			reason = "forbidden"
			projection = nil
		}
		if !now.Before(state.Deadline) {
			outcome = "expired"
			reason = "deadline"
			projection = nil
		}
		if outcome == "retry" {
			if state.Attempts >= state.Policy.MaxAttempts {
				return w.closeState(ctx, tx, work, state, "permanent", "attempts_exhausted", now, nil)
			}
			delay := state.Policy.BaseBackoff
			for i := int64(1); i < state.Attempts && delay < state.Policy.MaxBackoff; i++ {
				if delay > state.Policy.MaxBackoff/2 {
					delay = state.Policy.MaxBackoff
				} else {
					delay *= 2
				}
			}
			state.Due = boundedDue(now, delay, state.Deadline)
			state.Outcome = "retry"
			state.Reason = reason
			if err = repo.SaveSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision, *state); err != nil {
				return err
			}
			return schedule.DeferClaim(ctx, tx, work.Claim, now, state.Due)
		}
		if outcome != "success" && outcome != "permanent" && outcome != "expired" {
			return ErrPolicy
		}
		return w.closeState(ctx, tx, work, state, outcome, reason, now, projection)
	})
}
func (w *Worker) closeState(ctx context.Context, tx runtime.Tx, work Work, state *ScheduleState, outcome, reason string, now time.Time, projection *Projection) error {
	if outcome == "success" && (projection == nil || projection.InputRevision != work.Claim.ClaimedRevision) {
		return runtime.ErrClaim
	}
	if err := w.Claims.Complete(ctx, tx, work.Claim, now); err != nil {
		return err
	}
	state.Outcome = outcome
	state.Reason = reason
	repo := w.Repository.(ScheduleRepository)
	if err := repo.SaveSchedule(ctx, tx, w.Owner, work.Input.ID, work.Claim.ClaimedRevision, *state); err != nil {
		return err
	}
	if outcome == "success" {
		return w.Repository.SaveProjection(ctx, tx, w.Owner, work.Input.ID, *projection)
	}
	return nil
}

func (w *Worker) validateWork(work Work) error {
	c := work.Claim
	if c.Object.TenantID != w.Owner.TenantID || c.Object.OwnerID != w.Owner.OwnerID || c.Object.Kind != "durable_work" || c.Phase != "project" || c.Object.ID != work.Input.ID || c.ClaimedRevision != work.Input.Revision {
		return runtime.ErrClaim
	}
	return nil
}

// Process shutdown returns database authority best-effort. Failure preserves
// the original durable responsibility for finite lease takeover.
func (w *Worker) returnClaim(claim runtime.Claim) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	schedule, ok := w.Claims.(runtime.ScheduleStore)
	if !ok {
		return
	}
	_ = w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		_, _, err := poolLock(ctx, tx, w.Repository)
		if err != nil {
			return err
		}
		_, err = w.Repository.LockInput(ctx, tx, w.Owner, claim.Object.ID)
		if err != nil {
			return err
		}
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		return schedule.ReleaseClaim(ctx, tx, claim, now)
	})
}
