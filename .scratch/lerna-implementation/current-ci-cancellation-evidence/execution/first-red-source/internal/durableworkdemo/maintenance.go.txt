package durableworkdemo

import (
	"context"
	"github.com/ruipengliu/lerna/runtime"
)

// maintain closes exact expired/stopped responsibility without authorizing
// computation or allocating a new execution Claim. Its caller is the explicit
// pool control assembly; regular worker eligibility is not control authority.
func (w *Worker) maintain(ctx context.Context, binding PoolBinding) error {
	return w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		repo, pool, err := poolLock(ctx, tx, w.Repository)
		if err != nil {
			return err
		}
		scope, err := repo.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		if scope != binding.Scope || pool.Config.ID != binding.ID {
			return runtime.ErrScope
		}
		schedules, ok := w.Repository.(ScheduleRepository)
		if !ok {
			return ErrPolicy
		}
		key := "maintenance:" + string(w.Owner.TenantID) + "/" + string(w.Owner.OwnerID)
		cursor := pool.Cursors[key]
		page, through, err := repo.MaintenancePage(ctx, tx, cursor.After, cursor.Through)
		if err != nil {
			return err
		}
		cursor.Through = through
		for _, candidate := range page {
			cursor.After = string(candidate.ID)
			input, err := w.Repository.TryLockInput(ctx, tx, w.Owner, candidate.Object.ID)
			if err != nil {
				return err
			}
			if input == nil {
				continue
			}
			current, err := repo.LockMaintenanceJob(ctx, tx, candidate)
			if err != nil {
				return err
			}
			if current == nil || current.Job.State == "done" {
				continue
			}
			now, err := w.Clock.Now(ctx, tx)
			if err != nil {
				return err
			}
			revisions := []int64{current.ClaimedRevision, current.Job.WorkRevision}
			for index, revision := range revisions {
				if revision < 1 || revision <= current.Job.CompletedRevision || (index == 1 && revision == current.ClaimedRevision) {
					continue
				}
				state, err := schedules.LoadSchedule(ctx, tx, w.Owner, input.ID, revision)
				if err != nil {
					return err
				}
				if state == nil {
					continue
				}
				close := state.Outcome == "expired" || state.Outcome == "stopped" || state.Outcome == "permanent"
				if state.Outcome != "success" && state.Outcome != "permanent" && !close {
					if state.Stopped {
						state.Outcome = "stopped"
						state.Reason = "trusted_control_stop"
						close = true
					} else if !now.Before(state.Deadline) {
						state.Outcome = "expired"
						state.Reason = "deadline"
						close = true
					} else if state.Attempts >= state.Policy.MaxAttempts && !(current.ClaimedRevision == revision && now.Before(current.LeaseUntil)) {
						state.Outcome = "permanent"
						state.Reason = "attempts_exhausted"
						close = true
					}
					if close {
						if err = schedules.SaveSchedule(ctx, tx, w.Owner, input.ID, revision, *state); err != nil {
							return err
						}
					}
				}
				if close {
					closed, err := repo.ClosePoolRevision(ctx, tx, current.Job, revision, now)
					if err != nil {
						return err
					}
					if closed {
						current.Job.CompletedRevision = revision
						current.ClaimedRevision = 0
					}
				}
			}
		}
		if len(page) < 64 {
			cursor.After = ""
			cursor.Through = ""
		}
		return repo.SavePoolCursor(ctx, tx, pool, key, cursor)
	})
}
func (p *PoolWorker) Maintain(ctx context.Context) error {
	if !p.anchor.PoolControl {
		return publicError("forbidden", nil)
	}
	if err := workContext(ctx); err != nil {
		return err
	}
	var selected *Worker
	var binding PoolBinding
	err := p.anchor.Runner.Within(ctx, p.anchor.Owner, func(ctx context.Context, tx runtime.Tx) error {
		repo, pool, err := poolLock(ctx, tx, p.anchor.Repository)
		if err != nil {
			return err
		}
		binding.ID = pool.Config.ID
		binding.Scope, err = repo.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		cursor := pool.Cursors["maintenance-members"]
		cursor.Tenant %= len(pool.Config.Members)
		owner := pool.Config.Members[cursor.Tenant]
		selected = p.workers[owner]
		if selected == nil {
			return ErrPoolConfig
		}
		cursor.Tenant = (cursor.Tenant + 1) % len(pool.Config.Members)
		return repo.SavePoolCursor(ctx, tx, pool, "maintenance-members", cursor)
	})
	if err != nil {
		return err
	}
	return selected.maintain(ctx, binding)
}
