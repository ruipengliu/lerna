package memory

import (
	"context"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type missingMemory interface {
	ValidateMissing(context.Context, memory.Binding, *wire.MemoryRef, string, string, string, int64, memory.MissingChecker) error
}

func (a *Adapter) validateMissing(ctx context.Context, r contextassembly.Request, ref contextassembly.Reference, storage string) error {
	service, ok := a.memory.(missingMemory)
	if !ok || a.config.MissingChecker == nil || a.config.MissingRetainUntil <= 0 {
		return contextassembly.Denied
	}
	for _, target := range []struct{ action, location string }{{"process", r.Location}, {"store", storage}} {
		if e := service.ValidateMissing(ctx, a.config.Binding, wireRef(ref), r.Purpose, target.action, target.location, a.config.MissingRetainUntil, a.config.MissingChecker); e != nil {
			return mapped(e)
		}
	}
	return nil
}
func (a *Adapter) reserveMissing(ctx context.Context, r contextassembly.Request, ref contextassembly.Reference) error {
	permits, ok := a.grants.(memory.ReadPermits)
	if !ok {
		return contextassembly.Denied
	}
	grant := a.authorizations[ref]
	in, e := memory.DescribeGet(a.config.Binding, &wire.MemoryGet{ReadId: grant.ReadID, Ref: wireRef(ref), Revision: ref.Revision, Purpose: r.Purpose})
	if e != nil {
		return contextassembly.Invalid
	}
	use, e := permits.Reserve(ctx, a.config.Binding, in, grant.Material)
	if e != nil {
		return mapped(e)
	}
	return mapped(permits.Validate(ctx, a.config.Binding, in, grant.Material, use))
}

// MissingSourceReference describes authorized absence, never a readable body.
func MissingSourceReference(ref contextassembly.Reference) *wire.ContentSource {
	source := SourceReference(ref)
	source.Kind = "memory-missing"
	return source
}
