package auth

import (
	"context"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func (a *Authority) deletionAction(b memory.Binding, ref *wire.MemoryRef, purpose string) (*wire.AuthorizationAction, error) {
	if ref == nil || ref.Namespace != b.Namespace || !valid(ref.Key) {
		return nil, memory.Denied
	}
	p, ok := a.collections[key{b.Namespace, ref.Collection}]
	if !ok || purpose != p.Purpose || !contains(p.Storage, b.Location) || b.Recipient != b.Location {
		return nil, memory.Denied
	}
	return &wire.AuthorizationAction{Resource: p.Resource, Action: "memory.delete", Purpose: p.Purpose, Location: b.Location}, nil
}
func (a *Authority) CheckDeletion(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, purpose string) error {
	action, err := a.deletionAction(b, ref, purpose)
	if err != nil {
		return err
	}
	view, err := a.views.ViewActions(ctx, b.Token, []*wire.AuthorizationAction{action})
	if err != nil || view.Identity.Subject != b.Subject || view.Identity.Namespace != b.Namespace || len(view.Allowed) != 1 || !view.Allowed[0] {
		return memory.Denied
	}
	return nil
}
func (a *Authority) AdmitDeletion(ctx context.Context, b memory.Binding, id, semantic string, ref *wire.MemoryRef, purpose string) (int64, error) {
	if err := a.CheckDeletion(ctx, b, ref, purpose); err != nil {
		return 0, err
	}
	action, err := a.deletionAction(b, ref, purpose)
	if err != nil {
		return 0, err
	}
	admission, err := a.views.ReserveMemoryOperation(ctx, b.Token, id, semantic, action)
	if err != nil {
		return 0, admissionError(err)
	}
	if admission.Subject != b.Subject || admission.SemanticSHA256 != semantic {
		return 0, memory.Denied
	}
	return admission.ExpiresUnixNano, nil
}

var _ memory.DeletionAuthority = (*Authority)(nil)
