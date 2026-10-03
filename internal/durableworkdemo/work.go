package durableworkdemo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

// WorkRepository is owned by the project consumer; it is not required by the
// admission-only Host or adapters that have not implemented work yet.
type WorkRepository interface {
	TryLockInput(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (*Input, error)
	LockInput(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (*Input, error)
	SaveProjection(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, Projection) error
}
type Projection struct {
	InputRevision int64
	TextDigest    string
}
type Work struct {
	Claim runtime.Claim
	Input Input
}
type Worker struct {
	Owner       contract.OwnerRef
	Runner      runtime.TxRunner
	Claims      runtime.ClaimStore
	Repository  WorkRepository
	Clock       runtime.Clock
	Permissions *WorkerPermissions
}

func workContext(ctx context.Context) error {
	if ctx == nil {
		return runtime.ErrWorkBounds
	}
	if _, finite := ctx.Deadline(); !finite {
		return runtime.ErrWorkBounds
	}
	return ctx.Err()
}

func (w *Worker) Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]Work, error) {
	return w.claimLanes(ctx, worker, limit, lease, []string{"ordinary", "control", "reconciliation"}, nil)
}
func (w *Worker) claimLanes(ctx context.Context, worker string, limit int, lease time.Duration, lanes []string, binding *PoolBinding) ([]Work, error) {
	if err := workContext(ctx); err != nil {
		return nil, err
	}
	if worker == "" || len(worker) > 128 || limit < 1 || limit > 64 || lease < time.Millisecond || lease > 5*time.Minute {
		return nil, runtime.ErrWorkBounds
	}
	if !w.Permissions.Allows(worker) {
		return nil, publicError("forbidden", nil)
	}
	var batch []Work
	err := w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		repo, pool, err := poolLock(ctx, tx, w.Repository)
		if err != nil {
			return err
		}
		if binding != nil {
			scope, err := repo.PoolScope(ctx, tx)
			if err != nil {
				return err
			}
			if scope != binding.Scope || pool.Config.ID != binding.ID {
				return runtime.ErrScope
			}
		}
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		tenants := pool.Config.Tenants()
		budget := limit
		for _, lane := range lanes {
			limits, err := pool.Config.Limit(lane)
			if err != nil {
				return err
			}
			checked := 0
			wrapped := false
			for len(batch) < limit && budget > 0 && checked < len(tenants) {
				active, _, _, err := repo.PoolCounts(ctx, tx, pool, lane, w.Owner.TenantID, now)
				if err != nil {
					return err
				}
				if active >= limits.Concurrent {
					break
				}
				cursor, err := refreshPoolCursor(ctx, tx, repo, pool, lane, now)
				if err != nil {
					return err
				}
				if len(cursor.Order) == 0 {
					if err = repo.SavePoolCursor(ctx, tx, pool, lane, cursor); err != nil {
						return err
					}
					break
				}
				tenant := cursor.Order[0]
				hadProgress := cursor.After != ""
				candidates, through, err := repo.PoolPage(ctx, tx, pool, lane, tenant, cursor.After, cursor.Through, now)
				if err != nil {
					return err
				}
				cursor.Through = through
				claimed := false
				otherOwner := false
				for _, job := range candidates {
					if budget == 0 {
						break
					}
					budget--
					candidateOwner := contract.OwnerRef{TenantID: job.Object.TenantID, OwnerID: job.Object.OwnerID}
					if candidateOwner != w.Owner {
						otherOwner = true
						break
					}
					cursor.After = job.DueAt.UTC().Format(time.RFC3339Nano) + "|" + string(job.ID)
					if job.Phase != "project" || job.Object.Kind != "durable_work" {
						continue
					}
					input, err := w.Repository.TryLockInput(ctx, tx, w.Owner, job.Object.ID)
					if err != nil {
						return err
					}
					if input == nil {
						continue
					}
					now, err = w.Clock.Now(ctx, tx)
					if err != nil {
						return err
					}
					now = now.UTC().Truncate(time.Microsecond)
					claim, err := w.Claims.Claim(ctx, tx, job, worker, now, now.Add(lease))
					if err != nil {
						return err
					}
					if claim == nil {
						continue
					}
					if input.Revision != claim.ClaimedRevision {
						return runtime.ErrClaim
					}
					schedules, ok := w.Repository.(ScheduleRepository)
					if !ok {
						return ErrPolicy
					}
					state, err := schedules.LoadSchedule(ctx, tx, w.Owner, input.ID, claim.ClaimedRevision)
					if err != nil {
						return err
					}
					if state == nil {
						policy := LegacyPolicy()
						policy.Lane = lane
						if err = schedules.BindSchedule(ctx, tx, w.Owner, input.ID, claim.ClaimedRevision, ScheduleState{Source: "legacy-adoption", Policy: policy, AdoptedAt: now, Deadline: now.Add(policy.ExecutionLimit), Due: now}); err != nil {
							return err
						}
					}
					if err = repo.RegisterPoolClaim(ctx, tx, pool, *claim); err != nil {
						return err
					}
					batch = append(batch, Work{Claim: *claim, Input: *input})
					claimed = true
					break
				}
				if claimed || (!otherOwner && len(candidates) < 64 && budget > 0) {
					if err = rotatePoolCursor(&cursor, claimed); err != nil {
						return err
					}
					if claimed {
						checked = 0
					} else {
						if len(cursor.Order) == 1 && len(candidates) == 0 && hadProgress && !wrapped {
							wrapped = true
							checked = 0
						} else {
							checked++
						}
					}
				}
				if err = repo.SavePoolCursor(ctx, tx, pool, lane, cursor); err != nil {
					return err
				}
				if otherOwner || (!claimed && len(candidates) == 64) {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// Project hashes the exact UTF-8 bytes, without holding a Tx or changing Input.
func Project(work Work) Projection {
	digest := sha256.Sum256([]byte(work.Input.Text))
	return Projection{InputRevision: work.Input.Revision, TextDigest: "sha256:" + hex.EncodeToString(digest[:])}
}

// Complete is the strict success convenience entry. Start must already have
// been durably confirmed for this original Claim; completion never starts work.
func (w *Worker) Complete(ctx context.Context, claim runtime.Claim, projection Projection) error {
	return w.Finish(ctx, Work{Claim: claim, Input: Input{ID: claim.Object.ID, Revision: projection.InputRevision}}, "success", "", &projection)
}

func (w *Worker) Renew(ctx context.Context, claim runtime.Claim, lease time.Duration) (runtime.Claim, error) {
	if err := workContext(ctx); err != nil {
		return runtime.Claim{}, err
	}
	if lease < time.Millisecond || lease > 5*time.Minute {
		return runtime.Claim{}, runtime.ErrWorkBounds
	}
	if claim.Object.TenantID != w.Owner.TenantID || claim.Object.OwnerID != w.Owner.OwnerID || claim.Object.Kind != "durable_work" || claim.Phase != "project" {
		return runtime.Claim{}, runtime.ErrClaim
	}
	if !w.Permissions.Allows(claim.Worker) {
		return runtime.Claim{}, runtime.ErrClaim
	}
	var renewed runtime.Claim
	err := w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		poolRepo, pool, err := poolLock(ctx, tx, w.Repository)
		if err != nil {
			return err
		}
		input, err := w.Repository.LockInput(ctx, tx, w.Owner, claim.Object.ID)
		if err != nil {
			return err
		}
		if input == nil {
			return runtime.ErrClaim
		}
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		if err = poolRepo.ValidatePoolClaim(ctx, tx, pool, claim, now); err != nil {
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
		if err = schedule.ValidateClaim(ctx, tx, claim, now); err != nil {
			return err
		}
		now, err = w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = schedule.ValidateClaim(ctx, tx, claim, now); err != nil {
			return err
		}
		state, err := repo.LoadSchedule(ctx, tx, w.Owner, claim.Object.ID, claim.ClaimedRevision)
		if err != nil {
			return err
		}
		if state == nil || state.Stopped || !now.Before(state.Deadline) {
			return runtime.ErrClaim
		}
		until := boundedDue(now, lease, state.Deadline)
		renewed, err = w.Claims.Renew(ctx, tx, claim, now, until)
		return err
	})
	if err != nil {
		return runtime.Claim{}, err
	}
	return renewed, nil
}
