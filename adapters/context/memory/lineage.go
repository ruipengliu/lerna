package memory

import (
	"context"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"time"
)

// Derive checks the original signed read and the actual output storage policy.
// It reuses the bound read identity to retrieve authorized retention metadata;
// it never mints a replacement read permit. No Memory body leaves this method.
func (a *Adapter) Derive(ctx context.Context, r contextassembly.Request, dep contextassembly.Dependency, storage string) (*wire.ContentSource, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !label(storage) {
		return nil, 0, contextassembly.Invalid
	}
	if dep.State == "missing" {
		e := a.Validate(ctx, r, dep.Reference, false)
		if e != contextassembly.Missing {
			if e == nil {
				e = contextassembly.Invalidated
			}
			return nil, 0, e
		}
		if e = a.reserveMissing(ctx, r, dep.Reference); e != nil {
			return nil, 0, e
		}
		if e = a.validateMissing(ctx, r, dep.Reference, storage); e != nil {
			return nil, 0, e
		}
		return MissingSourceReference(dep.Reference), a.config.MissingRetainUntil, nil
	}
	switch dep.State {
	case "selected", "inapplicable", "trimmed":
	default:
		// A missing source needs a metadata-specific output policy. It cannot be
		// silently omitted or represented as a readable Memory body source.
		return nil, 0, contextassembly.Denied
	}
	ref := dep.Reference
	if e := a.Validate(ctx, r, ref, false); e != nil {
		return nil, 0, e
	}
	if e := a.memory.ValidateRetention(ctx, a.config.Binding, wireRef(ref), ref.Revision, r.Purpose, storage); e != nil {
		return nil, 0, mapped(e)
	}
	grant := a.authorizations[ref]
	result, e := a.reader.Get(ctx, a.config.Binding, &wire.MemoryGet{ReadId: grant.ReadID, Ref: wireRef(ref), Revision: ref.Revision, Purpose: r.Purpose}, grant.Material)
	if e != nil {
		return nil, 0, mapped(e)
	}
	if result == nil || result.Coverage != "complete" || len(result.Records) != 1 {
		return nil, 0, contextassembly.Invalidated
	}
	record := result.Records[0]
	if record == nil || record.Ref == nil || record.Ref.Namespace != ref.Namespace || record.Ref.Collection != ref.Collection || record.Ref.Key != ref.Key || record.Revision != ref.Revision || record.Spec == nil || record.Spec.Purpose != r.Purpose {
		return nil, 0, contextassembly.Invalidated
	}
	if e = a.Validate(ctx, r, ref, false); e != nil {
		return nil, 0, e
	}
	if e = a.memory.ValidateRetention(ctx, a.config.Binding, wireRef(ref), ref.Revision, r.Purpose, storage); e != nil {
		return nil, 0, mapped(e)
	}
	return SourceReference(ref), record.Spec.RetainUntil, nil
}
