package durableworkdemo

import "sync"

// WorkerPermissions is an explicitly injected trusted operator allowlist.
// It is separate from record subjects and demonstration fixture outcomes.
type WorkerPermissions struct {
	mu      sync.RWMutex
	allowed map[string]bool
}

func NewWorkerPermissions(workers []string) (*WorkerPermissions, error) {
	if len(workers) < 1 || len(workers) > 64 {
		return nil, ErrPolicy
	}
	p := &WorkerPermissions{allowed: make(map[string]bool)}
	for _, worker := range workers {
		if worker == "" || len(worker) > 128 || p.allowed[worker] {
			return nil, ErrPolicy
		}
		p.allowed[worker] = true
	}
	return p, nil
}
func (p *WorkerPermissions) Allows(worker string) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.allowed[worker]
}
func (p *WorkerPermissions) Revoke(worker string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.allowed, worker)
}
