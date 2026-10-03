package development

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"strings"
	"time"
)

// ClassifiedWorkerConfig固定此进程可领取的JobKinds，不授予物理目标所有权。
type ClassifiedWorkerConfig struct {
	PoolID      string   `json:"pool_id"`
	JobKinds    []string `json:"job_kinds"`
	Concurrency int      `json:"concurrency"`
}

func (a *App) classifiedWorker() (runtime.Worker, error) {
	c := a.Config.WorkerPool
	if c == nil || !api.ValidID(c.PoolID) || len(c.JobKinds) == 0 || len(c.JobKinds) > 256 || c.Concurrency < 1 || c.Concurrency > 64 {
		return runtime.Worker{}, api.E("invalid_request", "explicit_worker_pool_required")
	}
	seen := map[string]bool{}
	for _, kind := range c.JobKinds {
		if seen[kind] || strings.HasPrefix(kind, "execution.") {
			return runtime.Worker{}, api.E("forbidden", "worker_pool_target_or_duplicate_kind")
		}
		if _, ok := a.Registry.Job(kind); !ok {
			return runtime.Worker{}, api.E("unsupported", "worker_pool_unknown_job_kind")
		}
		seen[kind] = true
	}
	return runtime.Worker{Store: a.Store, Registry: a.Registry, Scopes: []runtime.Scope{a.Scope}, Kinds: append([]string{}, c.JobKinds...), Concurrency: c.Concurrency, Lease: 30 * time.Second, Poll: 25 * time.Millisecond}, nil
}
