package cleanup

import (
	"context"
	"lerna/extraction"
	"lerna/memory"
)

// Memory is a trusted host consumer of authoritative source fences. Completion
// means local revision bodies were erased and exact events durably recorded;
// it does not acknowledge downstream consumers or turn an unrelated Delete
// request into a committed operation.
type Memory struct{ store memory.SourceErasureStore }

func NewMemory(store memory.SourceErasureStore) (*Memory, error) {
	if store == nil {
		return nil, memory.Invalid
	}
	return &Memory{store}, nil
}
func (m *Memory) ApplySource(ctx context.Context, in extraction.SourceInvalidation) (bool, error) {
	_, err := m.store.EraseSource(ctx, memory.SourceErasure{Namespace: in.Namespace, Kind: in.Kind, Key: in.Key, ThroughRevision: in.ThroughRevision})
	return err == nil, err
}

var _ extraction.SourceCleanupSink = (*Memory)(nil)
