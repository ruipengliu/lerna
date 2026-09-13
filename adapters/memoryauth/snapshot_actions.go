package memoryauth

import (
	"context"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// SnapshotActions supplies the registered collection's extra model-processing
// and storage requirements. The caller still verifies exact record residency,
// source permissions, retention deadline and whether a body was actually saved.
func (r *ReadPermits) SnapshotActions(ctx context.Context, b memory.Binding, in memory.ReadIntent, storage string, retained bool) ([]*wire.AuthorizationAction, error) {
	c, ok := r.authority.collections[key{b.Namespace, in.Collection}]
	if !ok || c.Purpose != in.Purpose || !contains(c.ReferenceStorage, storage) || !contains(c.Recipients, storage) || !contains(c.Processing, b.Recipient) {
		return nil, memory.Denied
	}
	if retained && (!contains(c.Storage, storage) || !contains(c.Processing, storage)) {
		return nil, memory.Denied
	}
	actions := []*wire.AuthorizationAction{{Resource: c.Resource, Action: "memory.process", Purpose: c.Purpose, Location: b.Recipient}, {Resource: c.Resource, Action: "memory.store_reference", Purpose: c.Purpose, Location: storage}, {Resource: c.Resource, Action: "memory.discover", Purpose: c.Purpose, Location: storage}}
	if retained {
		for _, name := range []string{"store", "process", "disclose"} {
			actions = append(actions, &wire.AuthorizationAction{Resource: c.Resource, Action: "memory." + name, Purpose: c.Purpose, Location: storage})
		}
	}
	view, err := r.authority.views.ViewActions(ctx, b.Token, actions)
	if err != nil || view.Identity.Namespace != b.Namespace || view.Identity.Subject != b.Subject || len(view.Allowed) != len(actions) {
		return nil, memory.Denied
	}
	for _, allowed := range view.Allowed {
		if !allowed {
			return nil, memory.Denied
		}
	}
	return actions, nil
}
