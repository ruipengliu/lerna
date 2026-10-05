package durableworkdemo

import (
	"context"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"sync"
	"time"
)

// PoolWorker gives declared owners independent lane service opportunities.
// Selection is advisory; the selected owner's transaction rechecks the same
// persistent cursor and quota before creating an actual Claim.
type PoolWorker struct {
	Timer   runtime.Timer
	anchor  *Service
	workers map[contract.OwnerRef]*Worker
}
type Dispatch struct {
	Worker *Worker
	Work   Work
}

func NewPoolWorker(anchor *Service, workers []*Worker) (*PoolWorker, error) {
	if anchor == nil || anchor.Runner == nil || anchor.Clock == nil || len(workers) < 1 || len(workers) > 64 {
		return nil, ErrPoolConfig
	}
	if _, ok := anchor.Repository.(PoolRepository); !ok {
		return nil, ErrPoolMissing
	}
	pool := &PoolWorker{Timer: runtime.WallTimer{}, anchor: anchor, workers: map[contract.OwnerRef]*Worker{}}
	for _, worker := range workers {
		if worker == nil || worker.Permissions == nil || pool.workers[worker.Owner] != nil {
			return nil, ErrPoolConfig
		}
		pool.workers[worker.Owner] = worker
	}
	return pool, nil
}
func (p *PoolWorker) nextOwner(ctx context.Context, lane string) (contract.OwnerRef, PoolBinding, bool, error) {
	var owner contract.OwnerRef
	var binding PoolBinding
	found := false
	err := p.anchor.Runner.Within(ctx, p.anchor.Owner, func(ctx context.Context, tx runtime.Tx) error {
		repo, state, err := poolLock(ctx, tx, p.anchor.Repository)
		if err != nil {
			return err
		}
		binding.ID = state.Config.ID
		binding.Scope, err = repo.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		limits, err := state.Config.Limit(lane)
		if err != nil {
			return err
		}
		tenants := state.Config.Tenants()
		now, err := p.anchor.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		active, _, _, err := repo.PoolCounts(ctx, tx, state, lane, p.anchor.Owner.TenantID, now)
		if err != nil {
			return err
		}
		if active >= limits.Concurrent {
			return nil
		}
		cursor, err := refreshPoolCursor(ctx, tx, repo, state, lane, now)
		if err != nil {
			return err
		}
		for checked := 0; checked < len(tenants) && len(cursor.Order) > 0; checked++ {
			tenant := cursor.Order[0]
			page, through, err := repo.PoolPage(ctx, tx, state, lane, tenant, cursor.After, cursor.Through, now)
			if err != nil {
				return err
			}
			cursor.Through = through
			if len(page) > 0 {
				owner = contract.OwnerRef{TenantID: page[0].Object.TenantID, OwnerID: page[0].Object.OwnerID}
				if p.workers[owner] == nil {
					return ErrPoolConfig
				}
				found = true
				return repo.SavePoolCursor(ctx, tx, state, lane, cursor)
			}
			if err = rotatePoolCursor(&cursor, false); err != nil {
				return err
			}
		}
		return repo.SavePoolCursor(ctx, tx, state, lane, cursor)

	})
	return owner, binding, found, err
}
func (p *PoolWorker) ClaimLane(ctx context.Context, lane, worker string, lease time.Duration) (*Dispatch, error) {
	if err := workContext(ctx); err != nil {
		return nil, err
	}
	for tries := 0; tries < 64; tries++ {
		owner, binding, found, err := p.nextOwner(ctx, lane)
		if err != nil || !found {
			return nil, err
		}
		selected := p.workers[owner]
		batch, err := selected.claimLanes(ctx, worker, 1, lease, []string{lane}, &binding)
		if err != nil {
			return nil, err
		}
		if len(batch) > 0 {
			return &Dispatch{selected, batch[0]}, nil
		}
	}
	return nil, nil
}
func (p *PoolWorker) StepLane(ctx context.Context, lane, worker string, lease time.Duration) (bool, error) {
	if err := p.Maintain(ctx); err != nil {
		return false, err
	}
	dispatch, err := p.ClaimLane(ctx, lane, worker, lease)
	if err != nil || dispatch == nil {
		return false, err
	}
	return true, dispatch.Worker.Process(ctx, dispatch.Work)
}

// NextWake reads only declared pool scheduling facts. Already-due work that
// cannot acquire quota/locks waits for the finite fallback instead of spinning.
func (p *PoolWorker) NextWake(ctx context.Context, lane string, fallback time.Duration) (runtime.StepResult, error) {
	result := runtime.StepResult{}
	if err := workContext(ctx); err != nil {
		return result, err
	}
	if fallback < time.Millisecond || fallback > time.Second {
		return result, runtime.ErrWorkBounds
	}
	err := p.anchor.Runner.Within(ctx, p.anchor.Owner, func(ctx context.Context, tx runtime.Tx) error {
		repo, pool, err := poolLock(ctx, tx, p.anchor.Repository)
		if err != nil {
			return err
		}
		now, err := p.anchor.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		result.NextWake, err = repo.PoolNextWake(ctx, tx, pool, lane, now, now.Add(fallback))
		result.WaitFor = result.NextWake.Sub(now)
		return err
	})
	return result, err
}

// Run owns one independently cancellable loop per lane. It never waits for
// ordinary computation before allowing control/reconciliation service.
func (p *PoolWorker) Run(ctx context.Context, worker string, lease, fallback time.Duration) error {
	if err := workContext(ctx); err != nil {
		return err
	}
	if p.Timer == nil || fallback < time.Millisecond || fallback > time.Second {
		return runtime.ErrWorkBounds
	}
	running, cancel := context.WithCancel(ctx)
	defer cancel()
	outcomes := make(chan error, 3)
	var wg sync.WaitGroup
	for _, lane := range []string{"ordinary", "control", "reconciliation"} {
		wg.Add(1)
		go func(lane string) {
			defer wg.Done()
			for {
				processed, err := p.StepLane(running, lane, worker, lease)
				// A completed/deferred dispatch is bounded real progress. Drain
				// another opportunity before parking; an empty/blocked step parks.
				if err == nil && processed {
					continue
				}
				if err == nil {
					var result runtime.StepResult
					result, err = p.NextWake(running, lane, fallback)
					if err == nil {
						err = p.Timer.Wait(running, result.WaitFor)
					}
				}
				if err != nil {
					outcomes <- fmt.Errorf("pool %s lane: %w", lane, err)
					return
				}
			}
		}(lane)
	}
	joined := []error{<-outcomes}
	cancel()
	wg.Wait()
	close(outcomes)
	for outcome := range outcomes {
		joined = append(joined, outcome)
	}
	if callerErr := ctx.Err(); callerErr != nil {
		joined = append(joined, fmt.Errorf("pool caller context: %w", callerErr))
	}
	return errors.Join(joined...)
}
