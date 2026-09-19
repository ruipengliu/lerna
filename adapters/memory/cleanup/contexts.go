package cleanup

import (
	"context"
	"lerna/contextassembly"
	"lerna/memory"
)

type Contexts struct {
	store contextassembly.InvalidationStore
}

func NewContexts(store contextassembly.InvalidationStore) (*Contexts, error) {
	if store == nil {
		return nil, memory.Invalid
	}
	return &Contexts{store}, nil
}

// Apply erases complete affected snapshots. The storage port atomically pairs
// body removal with durable watermarks; a commit error is not completion. The
// original event can be replayed after an unknown result.
func (c *Contexts) Apply(ctx context.Context, event memory.SourceEvent) (bool, error) {
	if !memory.ValidSourceEvent(event) {
		return false, memory.Invalid
	}
	if event.Kind == memory.SourceErased {
		store, ok := c.store.(contextassembly.RevisionInvalidationStore)
		if !ok {
			return false, memory.Unavailable
		}
		_, err := store.InvalidateRevision(ctx, contextassembly.Reference{Namespace: event.Ref.Namespace, Collection: event.Ref.Collection, Key: event.Ref.Key, Revision: event.Revision})
		return err == nil, err
	}
	change := contextassembly.SourceInvalidation{Namespace: event.Ref.Namespace, Collection: event.Ref.Collection, Key: event.Ref.Key, Missing: true, ThroughRevision: ^uint64(0)}
	if _, err := c.store.InvalidateSource(ctx, change); err != nil {
		return false, err
	}
	if event.Kind != memory.SourceCreated {
		change.Missing = false
		change.ThroughRevision = event.Revision - 1
		if event.Kind == memory.SourceDeleted {
			change.ThroughRevision = ^uint64(0)
		}
		if _, err := c.store.InvalidateSource(ctx, change); err != nil {
			return false, err
		}
	}
	return true, nil
}

var _ memory.SourceSink = (*Contexts)(nil)
