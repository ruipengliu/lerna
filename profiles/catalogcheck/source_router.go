package catalogcheck

import (
	"context"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"sync"
)

// sourceRouter is private reference-host configuration. It installs the complete
// immutable Memory resolver before personalized execution begins; wire callers
// cannot change it. Existing content adapters retain this same governed service.
type sourceRouter struct {
	mu     sync.RWMutex
	base   artifacts.Sources
	memory artifacts.AuthorizedSources
}

func (r *sourceRouter) bind(m artifacts.AuthorizedSources) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory = m
}
func (r *sourceRouter) Check(ctx context.Context, s *wire.ContentSource, a, p, l string, u int64) error {
	return r.base.Check(ctx, s, a, p, l, u)
}
func (r *sourceRouter) CheckAuthorized(ctx context.Context, v artifacts.SourceAuthority, b artifacts.Binding, s *wire.ContentSource, a, p, l string, u int64) error {
	r.mu.RLock()
	m := r.memory
	r.mu.RUnlock()
	if m != nil {
		return m.CheckAuthorized(ctx, v, b, s, a, p, l, u)
	}
	return r.base.Check(ctx, s, a, p, l, u)
}
