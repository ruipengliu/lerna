package extraction

import (
	"context"
	wire "lerna/gen/harness/v1"
)

// SourceFence guards new uses independently of a provider's current mutable
// configuration. It is trusted host input and grants no source access itself.
type SourceFence interface {
	CheckSource(context.Context, string, *wire.ContentSource) error
}

// SourceInvalidation is a trusted authoritative event, not content supplied by
// an extractor. Every dependent candidate through this revision is retired as
// a whole; a later policy restoration does not reactivate its original use.
type SourceInvalidation struct {
	Namespace, Kind, Key string
	ThroughRevision      uint64
}

type CandidateInvalidator interface {
	// InvalidateSource persists a monotonic source fence and atomically removes
	// affected candidate/save-intent bodies. The count is newly retired local
	// candidates, not confirmation of cleanup by Memory or other consumers.
	InvalidateSource(context.Context, SourceInvalidation) (int, error)
}
