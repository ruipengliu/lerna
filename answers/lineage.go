package answers

import (
	"context"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

// Lineage contains governed dependencies of the complete output, including
// sources that influenced style or choice even if the model did not cite them.
type Lineage struct {
	Sources     []*wire.ContentSource
	RetainUntil int64
}

// LineageProvider validates the original decision dependencies and their
// permission at the actual output storage location. Returned source references
// are also enforced by the content service's source-policy resolver on every
// subsequent access. A reference alone is never a grant.
type LineageProvider interface {
	Sources(context.Context, tasks.Task, string) (Lineage, error)
}

func (a *ContentAccess) WithLineage(provider LineageProvider) (*ContentAccess, error) {
	if a == nil || provider == nil {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	out := *a
	out.lineage = provider
	return &out, nil
}
