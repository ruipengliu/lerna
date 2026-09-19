package policy

import (
	"context"
	"lerna/authorization"
	"lerna/contextassembly"
)

type Requirements interface {
	SnapshotRequirements(context.Context, contextassembly.Request, []contextassembly.Dependency) (authorization.UseSpec, error)
}

type Bound struct {
	adapter      *Adapter
	token        string
	requirements Requirements
}

func (a *Adapter) Bind(token string, r Requirements) (*Bound, error) {
	if token == "" || len(token) > 256 || r == nil {
		return nil, contextassembly.Invalid
	}
	return &Bound{a, token, r}, nil
}
func (b *Bound) PrepareSnapshot(ctx context.Context, r contextassembly.Request, deps []contextassembly.Dependency) error {
	// A memory-free context has no Memory retention obligation. Facts retain
	// their own current authorization checks in the assembler.
	if len(r.Candidates) == 0 && len(deps) == 0 {
		return nil
	}
	spec, err := b.requirements.SnapshotRequirements(ctx, r, deps)
	if err != nil {
		return err
	}
	return b.adapter.Prepare(ctx, b.token, r.Key, spec)
}
func (b *Bound) ValidateSnapshot(ctx context.Context, r contextassembly.Request) error {
	if len(r.Candidates) == 0 {
		return nil
	}
	return b.adapter.Validate(ctx, r.Key)
}

var _ contextassembly.SnapshotPolicy = (*Bound)(nil)
