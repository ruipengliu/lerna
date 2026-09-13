package memoryauth

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// CheckMissing uses explicit collection metadata policy. No invented MemorySpec
// or source body is used to authorize an absence claim.
func (a *Authority) CheckMissing(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, purpose, action string, until int64) error {
	return a.checkMissing(ctx, b, ref, purpose, action, until, a.views.ViewActions)
}
func (a *Authority) checkMissing(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, purpose, action string, until int64, viewActions func(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)) error {
	if ref == nil || ref.Namespace != b.Namespace || !valid(ref.Key) || b.Recipient != b.Location {
		return memory.Denied
	}
	p, ok := a.collections[key{b.Namespace, ref.Collection}]
	if !ok || p.Purpose != purpose || p.MissingMetadataUntil <= 0 || until <= 0 || until > p.MissingMetadataUntil || !contains(p.Recipients, b.Location) {
		return memory.Denied
	}
	names := []string{"discover"}
	switch action {
	case "store", "retain":
		if !contains(p.ReferenceStorage, b.Location) {
			return memory.Denied
		}
		names = append(names, "store_reference")
	case "process":
		if !contains(p.Processing, b.Location) {
			return memory.Denied
		}
		names = append(names, "process", "disclose")
	case "disclose":
		names = append(names, "disclose")
	case "discover":
	default:
		return memory.Denied
	}
	actions := make([]*wire.AuthorizationAction, 0, len(names))
	for _, name := range names {
		actions = append(actions, &wire.AuthorizationAction{Resource: p.Resource, Action: "memory." + name, Purpose: purpose, Location: b.Location})
	}
	view, err := viewActions(ctx, b.Token, actions)
	if err != nil || view.Identity.Namespace != b.Namespace || view.Identity.Subject != b.Subject || len(view.Allowed) != len(actions) {
		return memory.Denied
	}
	for _, allowed := range view.Allowed {
		if !allowed {
			return memory.Denied
		}
	}
	return nil
}
func (p *readPolicy) CheckMissing(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, purpose, action string, until int64) error {
	if p.authority == nil || p.view == nil {
		return memory.Denied
	}
	return p.authority.checkMissing(ctx, b, ref, purpose, action, until, func(ctx context.Context, token string, actions []*wire.AuthorizationAction) (authorization.ActionView, error) {
		if ctx.Err() != nil {
			return authorization.ActionView{}, memory.Unavailable
		}
		id, err := p.view.Identity(token)
		if err != nil {
			return authorization.ActionView{}, memory.Denied
		}
		out := authorization.ActionView{Identity: id}
		for _, action := range actions {
			got, err := p.view.Authorize(token, action)
			if err != nil || got != id {
				return authorization.ActionView{}, memory.Denied
			}
			out.Allowed = append(out.Allowed, true)
		}
		return out, nil
	})
}

var _ memory.MissingChecker = (*Authority)(nil)
var _ memory.MissingChecker = (*readPolicy)(nil)
