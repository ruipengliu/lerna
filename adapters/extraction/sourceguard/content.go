package sourceguard

import (
	"context"
	"errors"
	"lerna/artifacts"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// ContentPolicy applies the same permanent source fence to metadata and body
// outlets without reading source files. Current Content policy/authorization
// remains independently required. Trusted InvalidateSource/Clean ports handle
// erasure separately; a denied release is not a cleanup acknowledgment.
type ContentPolicy struct {
	provider  artifacts.Sources
	fence     extraction.SourceFence
	namespace string
}

func NewContentPolicy(provider artifacts.Sources, fence extraction.SourceFence, namespace string) (*ContentPolicy, error) {
	if provider == nil || fence == nil || namespace == "" || len(namespace) > 256 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return &ContentPolicy{provider, fence, namespace}, nil
}
func (p *ContentPolicy) check(ctx context.Context, source *wire.ContentSource) error {
	err := p.fence.CheckSource(ctx, p.namespace, source)
	if err == nil {
		return nil
	}
	if errors.Is(err, memory.Denied) || errors.Is(err, memory.ReplayUnavailable) {
		return artifacts.Error("PERMISSION_DENIED")
	}
	if errors.Is(err, memory.Invalid) {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	return artifacts.Error("UNAVAILABLE")
}
func (p *ContentPolicy) Check(ctx context.Context, source *wire.ContentSource, action, purpose, location string, until int64) error {
	if err := p.check(ctx, source); err != nil {
		return err
	}
	if err := p.provider.Check(ctx, source, action, purpose, location, until); err != nil {
		return err
	}
	return p.check(ctx, source)
}
func (p *ContentPolicy) CheckAuthorized(ctx context.Context, view artifacts.SourceAuthority, b artifacts.Binding, source *wire.ContentSource, action, purpose, location string, until int64) error {
	if b.Namespace != p.namespace || view == nil {
		return artifacts.Error("PERMISSION_DENIED")
	}
	if err := p.check(ctx, source); err != nil {
		return err
	}
	var err error
	if authorized, ok := p.provider.(artifacts.AuthorizedSources); ok {
		err = authorized.CheckAuthorized(ctx, view, b, source, action, purpose, location, until)
	} else {
		err = p.provider.Check(ctx, source, action, purpose, location, until)
	}
	if err != nil {
		return err
	}
	return p.check(ctx, source)
}

var _ artifacts.Sources = (*ContentPolicy)(nil)
var _ artifacts.AuthorizedSources = (*ContentPolicy)(nil)
