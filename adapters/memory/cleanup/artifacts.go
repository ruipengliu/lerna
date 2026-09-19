// Package cleanup connects authoritative Memory events to controlled
// cleanup ports. It does not grant peer access or claim remote propagation.
package cleanup

import (
	"context"
	contextmemory "lerna/adapters/context/memory"
	"lerna/artifacts"
	"lerna/contextassembly"
	"lerna/memory"
)

type ArtifactStore interface {
	InvalidateSource(context.Context, artifacts.SourceInvalidation) (artifacts.InvalidationStatus, error)
	Clean(context.Context) error
}

type Artifacts struct{ store ArtifactStore }

func NewArtifacts(store ArtifactStore) (*Artifacts, error) {
	if store == nil {
		return nil, memory.Invalid
	}
	return &Artifacts{store}, nil
}

// Apply first persists all invalidations, then performs one bounded cleanup
// batch. A missing-record claim is permanently obsolete once the identity has
// existed; deleted record identities likewise cannot be reused.
func (a *Artifacts) Apply(ctx context.Context, event memory.SourceEvent) (bool, error) {
	if !memory.ValidSourceEvent(event) {
		return false, memory.Invalid
	}
	if event.Kind == memory.SourceErased {
		store, ok := a.store.(artifacts.RevisionInvalidationStore)
		if !ok {
			return false, memory.Unavailable
		}
		ref := contextmemory.SourceReference(contextassembly.Reference{Namespace: event.Ref.Namespace, Collection: event.Ref.Collection, Key: event.Ref.Key, Revision: event.Revision})
		in := artifacts.SourceRevisionInvalidation{Namespace: event.Ref.Namespace, Kind: ref.Kind, Key: ref.Key, Revision: ref.Revision}
		if _, err := store.InvalidateRevision(ctx, in); err != nil {
			return false, err
		}
		if err := a.store.Clean(ctx); err != nil {
			return false, err
		}
		status, err := store.InvalidateRevision(ctx, in)
		return err == nil && status.Cleaning == 0, err
	}
	ref := contextassembly.Reference{Namespace: event.Ref.Namespace, Collection: event.Ref.Collection, Key: event.Ref.Key, Revision: event.Revision}
	missing := contextmemory.MissingSourceReference(ref)
	changes := []artifacts.SourceInvalidation{{Namespace: ref.Namespace, Kind: missing.Kind, Key: missing.Key, ThroughRevision: ^uint64(0)}}
	if event.Kind != memory.SourceCreated {
		source := contextmemory.SourceReference(ref)
		through := event.Revision - 1
		if event.Kind == memory.SourceDeleted {
			through = ^uint64(0)
		}
		changes = append(changes, artifacts.SourceInvalidation{Namespace: ref.Namespace, Kind: source.Kind, Key: source.Key, ThroughRevision: through})
	}
	for _, change := range changes {
		if _, err := a.store.InvalidateSource(ctx, change); err != nil {
			return false, err
		}
	}
	if err := a.store.Clean(ctx); err != nil {
		return false, err
	}
	complete := true
	for _, change := range changes {
		status, err := a.store.InvalidateSource(ctx, change)
		if err != nil {
			return false, err
		}
		complete = complete && status.Cleaning == 0
	}
	return complete, nil
}

var _ memory.SourceSink = (*Artifacts)(nil)
