// Package extraction defines the material-to-candidate seam. Extraction is not
// authorization, persistent retention, or admission of a Memory write.
package extraction

import (
	"context"
	wire "lerna/gen/harness/v1"
)

// Material is supplied by an authenticated source reader. Speaker is verified
// provenance, never inferred from instructions or identity claims in Text.
// The host checks current read/processing permission before supplying material.
type Material struct {
	Source  *wire.MemorySource
	Speaker string
	Text    string
	Choice  *Choice
}

// Choice is an observed selection supplied by the trusted source reader, not
// parsed from natural-language claims. Event identities share the reader scope.
type Choice struct{ Event, Task, Format string }

type Input struct {
	About     string
	Materials []Material
}

// Candidate is an uncommitted interpretation. Its sources are immutable copies.
// Before retaining, saving or disclosing it, the host must intersect all source
// constraints and check the separate current permissions for those effects.
type Candidate struct {
	About, Kind, Attribute, Value, Conditions string
	Sources                                   []*wire.MemorySource
	Confidence                                *wire.MemoryConfidence
}
type Result struct {
	Candidates []Candidate
	// Reason explains a non-producing result without reproducing source text.
	Reason string
}
type Extractor interface {
	Extract(context.Context, Input) (Result, error)
}
