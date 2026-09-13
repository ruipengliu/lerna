package memory

import "context"

// SourceErasure is authoritative host input, not a peer deletion permission.
// It removes whole revisions containing the affected source, not selected fields.
type SourceErasure struct {
	Namespace, Kind, Key string
	ThroughRevision      uint64
}

// SourceErasureStore atomically fences future writes, removes affected revision
// bodies/comparison hashes and records exact revision events. Its result counts
// newly erased local bodies only, never downstream cleanup acknowledgements.
type SourceErasureStore interface {
	EraseSource(context.Context, SourceErasure) (int, error)
}

// ErasedRevisionStore proves one exact event against durable erasure state
// before a consumer destroys the corresponding admission comparison material.
type ErasedRevisionStore interface {
	ErasedRevisionOperation(context.Context, SourceEvent) (Receipt, error)
}
