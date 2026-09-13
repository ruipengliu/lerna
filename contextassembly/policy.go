package contextassembly

import "context"

// SnapshotPolicy binds the original use before persistence and rechecks its
// durable validity on release. Implementations must not allocate new read IDs.
type SnapshotPolicy interface {
	PrepareSnapshot(context.Context, Request, []Dependency) error
	ValidateSnapshot(context.Context, Request) error
}

func NewWithPolicy(store Store, facts Facts, memories Memories, policy SnapshotPolicy) (*Assembler, error) {
	if policy == nil {
		return nil, Invalid
	}
	a, err := New(store, facts, memories)
	if err != nil {
		return nil, err
	}
	a.policy = policy
	return a, nil
}

func (a *Assembler) validatePolicy(ctx context.Context, r Request, d document) error {
	if d.PolicyBound != (a.policy != nil) {
		return Invalidated
	}
	if a.policy != nil {
		return a.policy.ValidateSnapshot(ctx, r)
	}
	return nil
}
