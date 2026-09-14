package tasks

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"lerna/authorization"
	"reflect"
)

type HandoffRoute struct{ Activation, Receipt HandoffEnvelope }
type ParentRoute struct {
	Owner  string
	Epoch  uint64
	Peer   authorization.GrantPresentation
	Digest string
}
type ParentRoutePolicy func(context.Context, ChildIntent, authorization.GrantPresentation) error

// Route binds the activated task owner to its exact authenticated presentation
// at a child endpoint. Host policy must verify this owner-to-node mapping.
func (p *HandoffPort) Route(ctx context.Context, ref Ref, op string, peer authorization.GrantPresentation) (HandoffRoute, error) {
	h, e := p.Status(ctx, ref, op)
	if e != nil {
		return HandoffRoute{}, e
	}
	r, e := p.authorizedRun(ctx, ref)
	if e != nil {
		return HandoffRoute{}, e
	}
	cert, certErr := base64.RawURLEncoding.Strict().DecodeString(peer.CertificateSHA256)
	if h.Phase != "ACTIVE" || r.Task.Owner != h.Request.Target || r.Task.OwnerEpoch != h.Epoch+1 || peer.Namespace != ref.Namespace || peer.Subject != r.Task.Subject || !name(peer.Presenter) || !name(peer.Audience) || certErr != nil || len(cert) != 32 {
		return HandoffRoute{}, failure(authorization.Denied)
	}
	if e = p.check(ctx, "route", r, peer.Presenter); e != nil {
		return HandoffRoute{}, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current := j.Handoffs[ref.TaskID]
		if current.Phase != "ACTIVE" || current.Request.OperationID != op {
			return failure(authorization.Conflict)
		}
		return p.authorize(tx, j.Runs[ref.TaskID])
	})
	if e != nil {
		return HandoffRoute{}, e
	}
	m := handoffMessageOf(h, "route")
	m.Peer = peer
	receipt, e := p.sign(m)
	return HandoffRoute{h.Activation, receipt}, e
}
func verifyParentRoute(in HandoffRoute, keys map[string]ed25519.PublicKey) (handoffMessage, error) {
	var a, r handoffMessage
	if len(in.Activation.Body) > 3<<20 || len(in.Receipt.Body) > 3<<20 || json.Unmarshal(in.Activation.Body, &a) != nil || json.Unmarshal(in.Receipt.Body, &r) != nil {
		return r, failure(authorization.Invalid)
	}
	source, target := keys[a.Source], keys[a.Request.Target]
	if len(source) != ed25519.PublicKeySize || len(target) != ed25519.PublicKeySize || !ed25519.Verify(source, in.Activation.Body, in.Activation.Signature) || !ed25519.Verify(target, in.Receipt.Body, in.Receipt.Signature) || a.Kind != "activate" || r.Kind != "route" || a.Epoch == 0 || a.Epoch == ^uint64(0) || a.Request != r.Request || a.Source != r.Source || a.Epoch != r.Epoch || a.Digest != r.Digest {
		return r, failure(authorization.Denied)
	}
	return r, nil
}
func (c *ChildPort) WithParentRoutes(keys map[string]ed25519.PublicKey, policy ParentRoutePolicy) (*ChildPort, error) {
	if policy == nil || len(keys) == 0 || len(keys) > 32 {
		return nil, failure(authorization.Invalid)
	}
	copy := *c
	copy.routeKeys = map[string]ed25519.PublicKey{}
	copy.routePolicy = policy
	for name, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			return nil, failure(authorization.Invalid)
		}
		copy.routeKeys[name] = append([]byte(nil), key...)
	}
	return &copy, nil
}

// FollowParent persists the signed ownership route before allowing the new peer
// to operate on existing child identities. Original grants and intents remain.
func (c *ChildPort) FollowParent(ctx context.Context, proof HandoffRoute, peer authorization.GrantPresentation) (*ChildPort, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.IOTimeout)
	defer cancel()
	if c.routePolicy == nil {
		return nil, failure(authorization.Unsupported)
	}
	m, e := verifyParentRoute(proof, c.routeKeys)
	if e != nil {
		return nil, e
	}
	if m.Peer != peer || peer.Namespace != c.service.config.Namespace || m.Request.Ref.Namespace != peer.Namespace {
		return nil, failure(authorization.Denied)
	}
	var selected []RunSnapshot
	e = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		for _, r := range j.Runs {
			if r.Parent == nil || r.Parent.Parent != m.Request.Ref {
				continue
			}
			id, e := tx.Authorize(c.token, r.Task.Resource, "task.read")
			if e != nil {
				return e
			}
			if id.Subject != r.Task.Subject {
				return failure(authorization.Denied)
			}
			owner, epoch := r.Parent.ParentOwner, r.Parent.ParentEpoch
			if r.ParentRoute != nil {
				owner, epoch = r.ParentRoute.Owner, r.ParentRoute.Epoch
			}
			if !(owner == m.Source && epoch == m.Epoch) && !(owner == m.Request.Target && epoch == m.Epoch+1 && r.ParentRoute != nil && r.ParentRoute.Peer == peer && r.ParentRoute.Digest == m.Digest) {
				return failure(authorization.IdentityConflict)
			}
			selected = append(selected, r)
		}
		if len(selected) == 0 {
			return failure(authorization.NotFound)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	for _, r := range selected {
		if e = c.routePolicy(ctx, *r.Parent, peer); e != nil {
			return nil, e
		}
	}
	e = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		for _, r := range selected {
			current := j.Runs[r.Task.Ref.TaskID]
			if current.Task.Version != r.Task.Version || !reflect.DeepEqual(current.ParentRoute, r.ParentRoute) {
				return failure(authorization.Conflict)
			}
			if _, e := tx.Authorize(c.token, r.Task.Resource, "task.read"); e != nil {
				return e
			}
			current.ParentRoute = &ParentRoute{Owner: m.Request.Target, Epoch: m.Epoch + 1, Peer: peer, Digest: m.Digest}
			j.Runs[r.Task.Ref.TaskID] = current
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return c.BindPeer(peer)
}
