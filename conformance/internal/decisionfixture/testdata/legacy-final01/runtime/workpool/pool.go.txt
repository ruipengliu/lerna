// Package workpool defines finite durable capacity values and pure tenant FIFO operations.
// Consumers own their storage ports, business state, and scheduling decisions.
package workpool

import (
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
	"time"
)

var ErrMissing = errors.New("durable pool configuration missing")
var ErrCapacity = errors.New("durable pool temporarily at capacity")
var ErrConfig = errors.New("durable pool configuration invalid or revision changed")

type LaneLimit struct {
	Lane              string
	Queue, Concurrent int64
}
type TenantQuota struct {
	Tenant     contract.ID
	Lane       string
	Concurrent int64
}
type Config struct {
	ID       contract.ID
	Revision int64
	Members  []contract.OwnerRef
	Limits   []LaneLimit
	Quotas   []TenantQuota
}
type Cursor struct {
	Tenant         int
	Order          []contract.ID
	Waiting        map[contract.ID]time.Time
	LastAllocated  map[contract.ID]int64
	After, Through string
	Sequence       int64
}
type State struct {
	Config  Config
	Cursors map[string]Cursor
}
type Binding struct {
	Scope string
	ID    contract.ID
}
type Observation struct {
	Scope          string
	State          State
	Active, Queued map[string]int64
}

func Default(id contract.ID, members []contract.OwnerRef) Config {
	cfg := Config{ID: id, Members: members, Limits: []LaneLimit{{"ordinary", 64, 4}, {"control", 16, 1}, {"reconciliation", 16, 1}}}
	for _, tenant := range cfg.Tenants() {
		for _, l := range cfg.Limits {
			cfg.Quotas = append(cfg.Quotas, TenantQuota{tenant, l.Lane, l.Concurrent})
		}
	}
	return cfg
}
func (c Config) Validate() error {
	if _, err := contract.Encode(c.ID); err != nil {
		return ErrConfig
	}
	if c.Revision < 0 || len(c.Members) < 1 || len(c.Members) > 64 || len(c.Limits) != 3 {
		return ErrConfig
	}
	seen := map[contract.OwnerRef]bool{}
	for _, m := range c.Members {
		if _, e := contract.Encode(m); e != nil || seen[m] {
			return ErrConfig
		}
		seen[m] = true
	}
	lanes := map[string]bool{}
	for _, l := range c.Limits {
		if (l.Lane != "ordinary" && l.Lane != "control" && l.Lane != "reconciliation") || lanes[l.Lane] || l.Queue < 1 || l.Queue > 4096 || l.Concurrent < 1 || l.Concurrent > 64 {
			return ErrConfig
		}
		lanes[l.Lane] = true
	}
	tenants := c.Tenants()
	if len(c.Quotas) != len(tenants)*3 {
		return ErrConfig
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
			return ErrConfig
		}
		quotaSeen[key] = true
	}
	return nil
}
func (c Config) Quota(tenant contract.ID, lane string) int64 {
	for _, q := range c.Quotas {
		if q.Tenant == tenant && q.Lane == lane {
			return q.Concurrent
		}
	}
	return 0
}
func (c Config) Limit(lane string) (LaneLimit, error) {
	for _, l := range c.Limits {
		if l.Lane == lane {
			return l, nil
		}
	}
	return LaneLimit{}, ErrConfig
}
func (c Config) Tenants() []contract.ID {
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

// RefreshCursor preserves eligible waiters and appends newly eligible members at the tail.
// Call it only after a global lane allocation opportunity is available.
func RefreshCursor(cursor Cursor, declared []contract.OwnerRef, ready map[contract.ID]time.Time, now time.Time) Cursor {
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
	for _, tenant := range (Config{Members: declared}).Tenants() {
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
	return cursor
}
func RotateCursor(cursor *Cursor, success bool) error {
	if len(cursor.Order) == 0 {
		return ErrConfig
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
