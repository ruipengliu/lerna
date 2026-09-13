// Package extractioncleanup connects durable source fences to owned cleanup
// ports. It grants no source disclosure or peer deletion authority.
package extractioncleanup

import (
	"context"
	"lerna/artifacts"
	"lerna/extraction"
	"lerna/memory"
)

type ContentStore interface {
	InvalidateSource(context.Context, artifacts.SourceInvalidation) (artifacts.InvalidationStatus, error)
	Clean(context.Context) error
}
type Content struct{ store ContentStore }

func NewContent(store ContentStore) (*Content, error) {
	if store == nil {
		return nil, memory.Invalid
	}
	return &Content{store}, nil
}
func (c *Content) ApplySource(ctx context.Context, in extraction.SourceInvalidation) (bool, error) {
	event := artifacts.SourceInvalidation{Namespace: in.Namespace, Kind: in.Kind, Key: in.Key, ThroughRevision: in.ThroughRevision}
	if _, err := c.store.InvalidateSource(ctx, event); err != nil {
		return false, err
	}
	if err := c.store.Clean(ctx); err != nil {
		return false, err
	}
	status, err := c.store.InvalidateSource(ctx, event)
	return err == nil && status.Cleaning == 0, err
}

var _ extraction.SourceCleanupSink = (*Content)(nil)
