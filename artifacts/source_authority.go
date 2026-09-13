package artifacts

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"sync"
)

// SourceAuthority is read-only and scoped to one source callback. It shares the
// current authorization snapshot; it cannot alter content or operation claims.
// Implementations must not retain it or start asynchronous work with it.
// During storage, successful Authorize checks become required conditions of the
// retained use. Do not probe optional permissions and ignore successful checks.
type SourceAuthority interface {
	Identity(string) (authorization.Identity, error)
	Authorize(string, *wire.AuthorizationAction) (authorization.Identity, error)
}

// AuthorizedSources can check dynamic dependencies without reentering the same
// authorization store. Implementations still own source revision/location checks
// and must not perform external effects inside this retriable callback.
type AuthorizedSources interface {
	CheckAuthorized(context.Context, SourceAuthority, Binding, *wire.ContentSource, string, string, string, int64) error
}
type sourceAuthority struct {
	mu  sync.Mutex
	ctx context.Context
	tx  authorization.ContentTransaction
}

func (v *sourceAuthority) Identity(token string) (authorization.Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tx == nil || v.ctx.Err() != nil {
		return authorization.Identity{}, Error("PERMISSION_DENIED")
	}
	return v.tx.Identity(token)
}
func (v *sourceAuthority) Authorize(token string, a *wire.AuthorizationAction) (authorization.Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tx == nil || v.ctx.Err() != nil {
		return authorization.Identity{}, Error("PERMISSION_DENIED")
	}
	return v.tx.Authorize(token, a)
}
func (v *sourceAuthority) close() { v.mu.Lock(); defer v.mu.Unlock(); v.tx = nil }
func (s *Service) checkSource(ctx context.Context, tx authorization.ContentTransaction, b Binding, source *wire.ContentSource, action, purpose, location string, until int64) error {
	if checker, ok := s.sources.(AuthorizedSources); ok {
		view := &sourceAuthority{ctx: ctx, tx: tx}
		defer view.close()
		return checker.CheckAuthorized(ctx, view, b, source, action, purpose, location, until)
	}
	return s.sources.Check(ctx, source, action, purpose, location, until)
}
