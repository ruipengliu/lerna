package durable

import (
	"context"
	"errors"
	"time"
)

func (t *Tx) Raise(key JobKey, source string, due time.Time) (Job, error) {
	if err := t.enter(); err != nil {
		return Job{}, err
	}
	defer t.leave()
	if err := t.jobOrder(key); err != nil {
		return Job{}, err
	}
	if !validName(source) || due.IsZero() {
		return Job{}, t.fail(ErrPrecondition)
	}
	j, err := t.session.Raise(t.ctx, t.scope, key, source, NewID("job"), due.UnixMilli())
	if err != nil {
		return Job{}, t.fail(err)
	}
	t.raised[key] = source
	t.wake = true
	return j, nil
}
func (t *Tx) Hint(key JobKey, due time.Time) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if err := t.jobOrder(key); err != nil {
		return err
	}
	if due.IsZero() {
		return t.fail(ErrPrecondition)
	}
	if err := t.session.Hint(t.ctx, t.scope, key, due.UnixMilli()); err != nil {
		return t.fail(err)
	}
	t.wake = true
	return nil
}
func (t *Tx) guard(claim Claim) (Job, error) {
	if claim.Scope() != t.scope || claim.JobID() == "" {
		return Job{}, t.fail(ErrClaim)
	}
	if err := t.jobOrder(claim.Key()); err != nil {
		return Job{}, err
	}
	j, err := t.session.LockJob(t.ctx, t.scope, claim.JobID())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			err = ErrClaim
		}
		return Job{}, t.fail(err)
	}
	now, err := t.session.Now(t.ctx)
	if err != nil {
		return Job{}, t.fail(err)
	}
	if j.State != "leased" || j.HolderID != claim.HolderID() || j.LeaseEpoch != claim.Epoch() || j.LeaseUntil <= now {
		return Job{}, t.fail(ErrClaim)
	}
	if j.WorkRevision < claim.ObservedRevision() {
		return Job{}, t.fail(ErrInvariant)
	}
	t.guarded[j.ID] = j.LeaseUntil
	return j, nil
}
func (t *Tx) Guard(claim Claim) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	_, err := t.guard(claim)
	return err
}
func (t *Tx) Finish(claim Claim, disposition Disposition) (string, error) {
	if err := t.enter(); err != nil {
		return "", err
	}
	defer t.leave()
	j, err := t.guard(claim)
	if err != nil {
		return "", err
	}
	if disposition.State != "done" && disposition.State != "waiting" {
		return "", t.fail(ErrPrecondition)
	}
	if j.WorkRevision > claim.ObservedRevision() {
		j.State = "ready"
		t.wake = true
	} else {
		j.State = disposition.State
		if j.State == "waiting" {
			now, err := t.session.Now(t.ctx)
			if err != nil {
				return "", t.fail(err)
			}
			if disposition.Reason == "" || disposition.DueAt.UnixMilli() <= now || disposition.DueAt.UnixMilli() > now+int64(time.Hour/time.Millisecond) {
				return "", t.fail(ErrPrecondition)
			}
			j.DueAt = disposition.DueAt.UnixMilli()
			j.WaitReason = disposition.Reason
		}
	}
	j.HolderID = ""
	j.LeaseUntil = 0
	if err := t.session.UpdateJob(t.ctx, j); err != nil {
		return "", t.fail(err)
	}
	return j.State, nil
}
func (t *Tx) DeleteDone(key JobKey, responsibilitiesClosed bool) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if err := t.jobOrder(key); err != nil {
		return err
	}
	if !responsibilitiesClosed {
		return t.fail(ErrPrecondition)
	}
	return t.fail(t.session.DeleteDone(t.ctx, t.scope, key))
}

// Claim returns only committed or independently confirmed candidate snapshots.
func (e *Engine) Claim(ctx context.Context, scope Scope, kind, holder string, limit int, lease time.Duration) ([]Claim, Result) {
	if !e.kinds[kind] || !idPattern.MatchString(holder) || limit < 1 || limit > 32 || lease < 100*time.Millisecond || lease > 30*time.Second {
		return nil, Result{RolledBack, 0, ErrPrecondition}
	}
	var candidates []Job
	result := e.Within(ctx, scope, nil, func(t *Tx) error {
		candidates = nil
		jobs, err := t.session.Candidates(t.ctx, scope, kind, limit)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			now, err := t.session.Now(t.ctx)
			if err != nil {
				return err
			}
			j, err = t.session.Lease(t.ctx, j, holder, now+lease.Milliseconds())
			if err != nil {
				return err
			}
			candidates = append(candidates, j)
		}
		return nil
	})
	var confirmed []Claim
	if result.Outcome == Committed {
		for _, j := range candidates {
			confirmed = append(confirmed, Claim{j, j.WorkRevision})
		}
	}
	if result.Outcome == CommitUnknown {
		for _, candidate := range candidates {
			var ok bool
			check := e.Within(ctx, scope, nil, func(t *Tx) error {
				j, err := t.session.LockJob(t.ctx, scope, candidate.ID)
				if err != nil {
					return err
				}
				now, err := t.session.Now(t.ctx)
				if err != nil {
					return err
				}
				ok = j.State == "leased" && j.HolderID == candidate.HolderID && j.LeaseEpoch == candidate.LeaseEpoch && j.LeaseUntil > now
				return nil
			})
			if check.Outcome == Committed && ok {
				confirmed = append(confirmed, Claim{candidate, candidate.WorkRevision})
			}
		}
	}
	e.claims.Add(int64(len(confirmed)))
	return confirmed, result
}
func (e *Engine) Renew(ctx context.Context, claim Claim, extension time.Duration) (time.Time, Result) {
	if extension < 100*time.Millisecond || extension > 30*time.Second {
		return time.Time{}, Result{RolledBack, 0, ErrPrecondition}
	}
	var until int64
	result := e.Within(ctx, claim.Scope(), nil, func(t *Tx) error {
		j, err := t.guard(claim)
		if err != nil {
			return err
		}
		now, err := t.session.Now(t.ctx)
		if err != nil {
			return err
		}
		until = max(j.LeaseUntil, now+extension.Milliseconds())
		j.LeaseUntil = until
		return t.session.UpdateJob(t.ctx, j)
	})
	if result.Outcome != Committed {
		return time.Time{}, result
	}
	return time.UnixMilli(until).UTC(), result
}
