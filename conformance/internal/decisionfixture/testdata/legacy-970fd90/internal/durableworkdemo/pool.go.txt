package durableworkdemo

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

var ErrPoolMissing = workpool.ErrMissing
var ErrPoolCapacity = workpool.ErrCapacity
var ErrPoolConfig = workpool.ErrConfig

type LaneLimit = workpool.LaneLimit
type TenantQuota = workpool.TenantQuota
type PoolConfig = workpool.Config
type PoolCursor = workpool.Cursor
type PoolState = workpool.State
type PoolBinding = workpool.Binding
type PoolObservation = workpool.Observation

type MaintenanceJob struct {
	Job             runtime.Job
	ClaimedRevision int64
	LeaseUntil      time.Time
}

func DefaultPool(id contract.ID, members []contract.OwnerRef) PoolConfig {
	return workpool.Default(id, members)
}

// PoolRepository coordinates bounded runtime facts, never other-owner input.
// Every consumer mutation locks its pool before its own input/Job.
type PoolRepository interface {
	PoolScope(context.Context, runtime.Tx) (string, error)
	LockPool(context.Context, runtime.Tx) (PoolState, error)
	InstallPool(context.Context, runtime.Tx, PoolConfig, int64, time.Time) error
	SavePoolCursor(context.Context, runtime.Tx, PoolState, string, PoolCursor) error
	PoolNextWake(context.Context, runtime.Tx, PoolState, string, time.Time, time.Time) (time.Time, error)
	PoolReadyTenants(context.Context, runtime.Tx, PoolState, string, time.Time) (map[contract.ID]time.Time, error)
	PoolCounts(context.Context, runtime.Tx, PoolState, string, contract.ID, time.Time) (int64, int64, int64, error)
	PoolQueue(context.Context, runtime.Tx, PoolState, contract.ID, string) (string, bool, error)
	SetJobLane(context.Context, runtime.Tx, contract.ID, string) error
	PoolPage(context.Context, runtime.Tx, PoolState, string, contract.ID, string, string, time.Time) ([]runtime.Job, string, error)
	RegisterPoolClaim(context.Context, runtime.Tx, PoolState, runtime.Claim) error
	ValidatePoolClaim(context.Context, runtime.Tx, PoolState, runtime.Claim, time.Time) error
	MaintenancePage(context.Context, runtime.Tx, string, string) ([]runtime.Job, string, error)
	LockMaintenanceJob(context.Context, runtime.Tx, runtime.Job) (*MaintenanceJob, error)
	ClosePoolRevision(context.Context, runtime.Tx, runtime.Job, int64, time.Time) (bool, error)
	ObservePool(context.Context, contract.OwnerRef, runtime.Clock) (PoolObservation, error)
}

func poolLock(ctx context.Context, tx runtime.Tx, repository any) (PoolRepository, PoolState, error) {
	repo, ok := repository.(PoolRepository)
	if !ok {
		return nil, PoolState{}, ErrPoolMissing
	}
	state, err := repo.LockPool(ctx, tx)
	return repo, state, err
}
func (s *Service) InstallPool(ctx context.Context, cfg PoolConfig, expected int64) error {
	if !s.PoolControl {
		return publicError("forbidden", nil)
	}
	if err := workContext(ctx); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	repo, ok := s.Repository.(PoolRepository)
	if !ok {
		return ErrPoolMissing
	}
	return s.Runner.Within(ctx, s.Owner, func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		return repo.InstallPool(ctx, tx, cfg, expected, now)
	})
}
func (s *Service) ObservePool(ctx context.Context) (PoolObservation, error) {
	if !s.PoolControl {
		return PoolObservation{}, publicError("forbidden", nil)
	}
	if err := workContext(ctx); err != nil {
		return PoolObservation{}, err
	}
	repo, ok := s.Repository.(PoolRepository)
	if !ok {
		return PoolObservation{}, ErrPoolMissing
	}
	return repo.ObservePool(ctx, s.Owner, s.Clock)
}

// Refresh only after a global lane opportunity is available.
func refreshPoolCursor(ctx context.Context, tx runtime.Tx, repo PoolRepository, state PoolState, lane string, now time.Time) (PoolCursor, error) {
	ready, err := repo.PoolReadyTenants(ctx, tx, state, lane, now)
	if err != nil {
		return PoolCursor{}, err
	}
	return workpool.RefreshCursor(state.Cursors[lane], state.Config.Members, ready, now), nil
}
func rotatePoolCursor(cursor *PoolCursor, success bool) error {
	return workpool.RotateCursor(cursor, success)
}
