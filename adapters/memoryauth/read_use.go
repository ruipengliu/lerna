package memoryauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// ReadUse describes an already allocated, currently verified original read.
// It grants no new rights. A retained-use registration must recheck this permit
// against its authority snapshot before persisting the new dependency.
type ReadUse struct {
	Permit  authorization.UsePermit
	Actions []*wire.AuthorizationAction
	Expires int64
}

// OriginalUse never calls ReserveUse and never substitutes a read identity.
// Collection resources and locations come from the existing trusted read plan.
func (r *ReadPermits) OriginalUse(ctx context.Context, b memory.Binding, in memory.ReadIntent, material string) (ReadUse, error) {
	p, action, err := r.plan(ctx, b, in)
	if err != nil {
		return ReadUse{}, err
	}
	record, err := r.grants.Verify(ctx, material, p, action)
	if err != nil || record == nil || record.Spec == nil || record.Spec.Scope == nil {
		return ReadUse{}, memory.Denied
	}
	permit, err := r.grants.LookupUse(ctx, p)
	if err != nil {
		return ReadUse{}, memory.Denied
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(action)
	if err != nil {
		return ReadUse{}, memory.Invalid
	}
	hash := sha256.Sum256(raw)
	if permit.GrantID != record.Id || permit.Units != 1 || permit.ActionSHA256 != hex.EncodeToString(hash[:]) {
		return ReadUse{}, memory.Denied
	}
	return ReadUse{Permit: permit, Actions: readUseActions(action, b), Expires: record.Spec.Scope.ExpiresUnix}, nil
}

// Share the exact required checks with plan so proof extraction cannot silently
// drift from authorization as the read contract evolves.
func readUseActions(action *wire.AuthorizationAction, b memory.Binding) []*wire.AuthorizationAction {
	return []*wire.AuthorizationAction{proto.Clone(action).(*wire.AuthorizationAction),
		{Resource: action.Resource, Action: "memory.process", Purpose: action.Purpose, Location: b.Location},
		{Resource: action.Resource, Action: "memory.discover", Purpose: action.Purpose, Location: b.Recipient},
		{Resource: action.Resource, Action: "memory.disclose", Purpose: action.Purpose, Location: b.Recipient},
	}
}
