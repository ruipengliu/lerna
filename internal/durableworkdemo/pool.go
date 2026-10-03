package durableworkdemo

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
	"time"
)

var ErrPoolMissing = errors.New("durable pool configuration missing")
var ErrPoolCapacity = errors.New("durable pool temporarily at capacity")
var ErrPoolConfig = errors.New("durable pool configuration invalid or revision changed")

type LaneLimit struct {
	Lane              string
	Queue, Concurrent int64
}
type TenantQuota struct {
	Tenant     contract.ID
	Lane       string
	Concurrent int64
}
type PoolConfig struct {
	ID       contract.ID
	Revision int64
	Members  []contract.OwnerRef
	Limits   []LaneLimit
	Quotas   []TenantQuota
}
type PoolCursor struct {
	Tenant         int
	Order          []contract.ID
	Waiting        map[contract.ID]time.Time
	LastAllocated  map[contract.ID]int64
	After, Through string
	Sequence       int64
}
type PoolState struct {
	Config  PoolConfig
	Cursors map[string]PoolCursor
}
type MaintenanceJob struct {
	Job             runtime.Job
	ClaimedRevision int64
	LeaseUntil      time.Time
}
type PoolBinding struct {
	Scope string
	ID    contract.ID
}
type PoolObservation struct {
	Scope          string
	State          PoolState
	Active, Queued map[string]int64
}

func DefaultPool(id contract.ID, members []contract.OwnerRef) PoolConfig {
	cfg := PoolConfig{ID: id, Members: members, Limits: []LaneLimit{{"ordinary", 64, 4}, {"control", 16, 1}, {"reconciliation", 16, 1}}}
	for _, tenant := range cfg.Tenants() {
		for _, l := range cfg.Limits {
			cfg.Quotas = append(cfg.Quotas, TenantQuota{tenant, l.Lane, l.Concurrent})
		}
	}
	return cfg
}
func (c PoolConfig) Validate() error {
	if _, err := contract.Encode(c.ID); err != nil {
		return ErrPoolConfig
	}
	if c.Revision < 0 || len(c.Members) < 1 || len(c.Members) > 64 || len(c.Limits) != 3 {
		return ErrPoolConfig
	}
	seen := map[contract.OwnerRef]bool{}
	for _, m := range c.Members {
		if _, e := contract.Encode(m); e != nil || seen[m] {
			return ErrPoolConfig
		}
		seen[m] = true
	}
	lanes := map[string]bool{}
	for _, l := range c.Limits {
		if (l.Lane != "ordinary" && l.Lane != "control" && l.Lane != "reconciliation") || lanes[l.Lane] || l.Queue < 1 || l.Queue > 4096 || l.Concurrent < 1 || l.Concurrent > 64 {
			return ErrPoolConfig
		}
		lanes[l.Lane] = true
	}
	tenants := c.Tenants()
	if len(c.Quotas) != len(tenants)*3 {
		return ErrPoolConfig
	}
	quotaSeen := map[string]bool{}
	for _, q := range c.Quotas {
		l, err := c.Limit(q.Lane)
		known := false
		for _, tenant := range tenants {
			if tenant == q.Tenant {
				known = true
			}
		}
		key := string(q.Tenant) + "/" + q.Lane
		if err != nil || !known || quotaSeen[key] || q.Concurrent < 0 || q.Concurrent > l.Concurrent {
			return ErrPoolConfig
		}
		quotaSeen[key] = true
	}
	return nil
}
func (c PoolConfig) Quota(tenant contract.ID, lane string) int64 {
	for _, q := range c.Quotas {
		if q.Tenant == tenant && q.Lane == lane {
			return q.Concurrent
		}
	}
	return 0
}
func (c PoolConfig) Limit(lane string) (LaneLimit, error) {
	for _, l := range c.Limits {
		if l.Lane == lane {
			return l, nil
		}
	}
	return LaneLimit{}, ErrPoolConfig
}
func (c PoolConfig) Tenants() []contract.ID {
	seen := map[contract.ID]bool{}
	var ids []contract.ID
	for _, m := range c.Members {
		if !seen[m.TenantID] {
			seen[m.TenantID] = true
			ids = append(ids, m.TenantID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// PoolRepository coordinates bounded runtime facts, never other-owner input.
// Every consumer mutation locks its pool before its own input/Job.
type PoolRepository interface {
	PoolScope(context.Context, runtime.Tx) (string, error)
	LockPool(context.Context, runtime.Tx) (PoolState, error)
	InstallPool(context.Context, runtime.Tx, PoolConfig, int64, time.Time) error
	SavePoolCursor(context.Context, runtime.Tx, PoolState, string, PoolCursor) error
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

// Refresh only after a global lane opportunity is available. Saturation pauses
// the existing FIFO; newly/re-eligible tenants join its current tail.
func refreshPoolCursor(ctx context.Context, tx runtime.Tx, repo PoolRepository, state PoolState, lane string, now time.Time) (PoolCursor, error) {
	ready, err := repo.PoolReadyTenants(ctx, tx, state, lane, now)
	if err != nil {
		return PoolCursor{}, err
	}
	cursor := state.Cursors[lane]
	previous := ""
	if len(cursor.Order) > 0 {
		previous = string(cursor.Order[0])
	}
	order := []contract.ID{}
	seen := map[contract.ID]bool{}
	for _, tenant := range cursor.Order {
		if _, eligible := ready[tenant]; eligible && !seen[tenant] {
			seen[tenant] = true
			order = append(order, tenant)
		}
	}
	if cursor.Waiting == nil {
		cursor.Waiting = map[contract.ID]time.Time{}
	}
	if cursor.LastAllocated == nil {
		cursor.LastAllocated = map[contract.ID]int64{}
	}
	members := map[contract.ID]bool{}
	var newTenants []contract.ID
	for _, tenant := range state.Config.Tenants() {
		members[tenant] = true
		if _, eligible := ready[tenant]; eligible && !seen[tenant] {
			newTenants = append(newTenants, tenant)
		}
	}
	sort.Slice(newTenants, func(i, j int) bool {
		if ready[newTenants[i]].Equal(ready[newTenants[j]]) {
			return newTenants[i] < newTenants[j]
		}
		return ready[newTenants[i]].Before(ready[newTenants[j]])
	})
	for _, tenant := range newTenants {
		order = append(order, tenant)
		cursor.Waiting[tenant] = now
	}
	for tenant := range cursor.Waiting {
		if _, eligible := ready[tenant]; !eligible {
			delete(cursor.Waiting, tenant)
		}
	}
	for tenant := range cursor.LastAllocated {
		if !members[tenant] {
			delete(cursor.LastAllocated, tenant)
		}
	}
	cursor.Order = order
	if len(order) == 0 || string(order[0]) != previous {
		cursor.After = ""
		cursor.Through = ""
	}
	return cursor, nil
}
func rotatePoolCursor(cursor *PoolCursor, success bool) error {
	if len(cursor.Order) == 0 {
		return ErrPoolConfig
	}
	tenant := cursor.Order[0]
	cursor.Order = append(cursor.Order[1:], tenant)
	cursor.After = ""
	cursor.Through = ""
	if success {
		if cursor.Sequence == 9223372036854775807 {
			return runtime.ErrWorkBounds
		}
		cursor.Sequence++
		cursor.LastAllocated[tenant] = cursor.Sequence
	}
	return nil
}
