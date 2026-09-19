// Package sourceguard shares persistent source invalidation between
// extraction and Memory. The underlying provider still enforces current file,
// policy and residency constraints; a fence never grants provider access.
package sourceguard

import (
	"context"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type Provider interface {
	Read(context.Context, []*wire.ContentSource, string, string) ([]extraction.Material, []extraction.Restrictions, error)
	Validate(context.Context, []*wire.ContentSource, extraction.Restrictions) error
	Check(context.Context, *wire.ContentSource, string, string, string, int64) error
}
type Guard struct {
	provider  Provider
	fence     extraction.SourceFence
	namespace string
}

func New(provider Provider, fence extraction.SourceFence, namespace string) (*Guard, error) {
	if provider == nil || fence == nil || namespace == "" || len(namespace) > 256 {
		return nil, memory.Invalid
	}
	return &Guard{provider, fence, namespace}, nil
}
func (g *Guard) check(ctx context.Context, refs []*wire.ContentSource) error {
	if len(refs) == 0 || len(refs) > 16 {
		return memory.Invalid
	}
	for _, ref := range refs {
		if err := g.fence.CheckSource(ctx, g.namespace, ref); err != nil {
			return err
		}
	}
	return nil
}
func (g *Guard) Read(ctx context.Context, refs []*wire.ContentSource, location, purpose string) ([]extraction.Material, []extraction.Restrictions, error) {
	if err := g.check(ctx, refs); err != nil {
		return nil, nil, err
	}
	materials, bounds, err := g.provider.Read(ctx, refs, location, purpose)
	if err != nil {
		return nil, nil, err
	}
	if err = g.check(ctx, refs); err != nil {
		return nil, nil, err
	}
	return materials, bounds, nil
}
func (g *Guard) Validate(ctx context.Context, refs []*wire.ContentSource, bounds extraction.Restrictions) error {
	if err := g.check(ctx, refs); err != nil {
		return err
	}
	if err := g.provider.Validate(ctx, refs, bounds); err != nil {
		return err
	}
	return g.check(ctx, refs)
}
func (g *Guard) Check(ctx context.Context, ref *wire.ContentSource, action, purpose, location string, until int64) error {
	refs := []*wire.ContentSource{ref}
	if err := g.check(ctx, refs); err != nil {
		return err
	}
	if err := g.provider.Check(ctx, ref, action, purpose, location, until); err != nil {
		return err
	}
	return g.check(ctx, refs)
}
