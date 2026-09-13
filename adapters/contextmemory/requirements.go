package contextmemory

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/memoryauth"
	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type readUses interface {
	OriginalUse(context.Context, memory.Binding, memory.ReadIntent, string) (memoryauth.ReadUse, error)
	SnapshotActions(context.Context, memory.Binding, memory.ReadIntent, string, bool) ([]*wire.AuthorizationAction, error)
}

// SnapshotRequirements describes the exact governed selections already made;
// repeated reads reconcile original bindings, never allocate replacement IDs.
func (a *Adapter) SnapshotRequirements(ctx context.Context, r contextassembly.Request, deps []contextassembly.Dependency) (authorization.UseSpec, error) {
	if !a.scope(r) || len(deps) == 0 || len(deps) > 16 || len(deps) != len(r.Candidates) {
		return authorization.UseSpec{}, contextassembly.Invalid
	}
	uses, ok := a.grants.(readUses)
	if !ok {
		return authorization.UseSpec{}, contextassembly.Unavailable
	}
	out := authorization.UseSpec{}
	deadline := func(at int64) {
		if at > 0 && (out.Until == 0 || at < out.Until) {
			out.Until = at
		}
	}
	for i, dep := range deps {
		switch dep.State {
		case "selected", "missing", "inapplicable", "trimmed":
		default:
			return authorization.UseSpec{}, contextassembly.Invalid
		}
		if dep.Reference != r.Candidates[i].Reference || dep.Retained && dep.State != "selected" {
			return authorization.UseSpec{}, contextassembly.Invalid
		}
		err := a.Validate(ctx, r, dep.Reference, dep.Retained)
		if dep.State == "missing" {
			if err != contextassembly.Missing {
				return authorization.UseSpec{}, contextassembly.Denied
			}
		} else if err != nil {
			return authorization.UseSpec{}, err
		}
		grant := a.authorizations[dep.Reference]
		get := &wire.MemoryGet{ReadId: grant.ReadID, Ref: wireRef(dep.Reference), Revision: dep.Reference.Revision, Purpose: r.Purpose}
		intent, err := memory.DescribeGet(a.config.Binding, get)
		if err != nil {
			return authorization.UseSpec{}, contextassembly.Invalid
		}
		proof, err := uses.OriginalUse(ctx, a.config.Binding, intent, grant.Material)
		if err != nil {
			return authorization.UseSpec{}, mapped(err)
		}
		deadline(proof.Expires)
		out.Permits = append(out.Permits, proof.Permit)
		extra, err := uses.SnapshotActions(ctx, a.config.Binding, intent, r.Storage, dep.Retained)
		if err != nil {
			return authorization.UseSpec{}, mapped(err)
		}
		for _, action := range append(proof.Actions, extra...) {
			found := false
			for _, old := range out.Actions {
				if proto.Equal(old, action) {
					found = true
					break
				}
			}
			if !found {
				out.Actions = append(out.Actions, proto.Clone(action).(*wire.AuthorizationAction))
			}
		}
		if len(out.Actions) > 16 {
			return authorization.UseSpec{}, contextassembly.Capacity
		}
		if dep.State == "missing" {
			deadline(a.config.MissingRetainUntil)
			continue
		}
		result, err := a.reader.Get(ctx, a.config.Binding, get, grant.Material)
		if err != nil {
			return authorization.UseSpec{}, mapped(err)
		}
		if result == nil || len(result.Records) != 1 || result.Coverage != "complete" {
			return authorization.UseSpec{}, contextassembly.Invalidated
		}
		record := result.Records[0]
		if record == nil || record.Ref == nil || !proto.Equal(record.Ref, get.Ref) || record.Revision != get.Revision || record.Spec == nil || record.Spec.Purpose != r.Purpose || record.Spec.RetainUntil <= 0 {
			return authorization.UseSpec{}, contextassembly.Invalidated
		}
		deadline(record.Spec.RetainUntil)
		if record.Spec.ValidUntil != nil {
			deadline(*record.Spec.ValidUntil)
		}
	}
	if out.Until <= 0 {
		return authorization.UseSpec{}, contextassembly.Invalid
	}
	return out, nil
}
