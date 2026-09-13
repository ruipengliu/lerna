package memoryauth

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// PolicyAuthority exposes only checks from an existing authorization snapshot.
// It must remain scoped to the outer transaction or source callback.
type PolicyAuthority interface {
	Identity(string) (authorization.Identity, error)
	Authorize(string, *wire.AuthorizationAction) (authorization.Identity, error)
}
type readPolicy struct {
	authority *Authority
	view      PolicyAuthority
}

func (a *Authority) ReadPolicy(view PolicyAuthority) memory.Checker { return &readPolicy{a, view} }
func (p *readPolicy) Check(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, spec *wire.MemorySpec, method string) error {
	if p.authority == nil || p.view == nil {
		return memory.Denied
	}
	switch method {
	case "read", "discover", "retain", "retain-reference":
	default:
		return memory.Denied
	}
	return p.authority.check(ctx, b, ref, spec, method, func(ctx context.Context, token string, actions []*wire.AuthorizationAction) (authorization.ActionView, error) {
		if ctx.Err() != nil {
			return authorization.ActionView{}, memory.Unavailable
		}
		id, e := p.view.Identity(token)
		if e != nil {
			return authorization.ActionView{}, memory.Denied
		}
		out := authorization.ActionView{Identity: id}
		for _, action := range actions {
			current, e := p.view.Authorize(token, action)
			if e != nil || current != id {
				return authorization.ActionView{}, memory.Denied
			}
			out.Allowed = append(out.Allowed, true)
		}
		return out, nil
	})
}
