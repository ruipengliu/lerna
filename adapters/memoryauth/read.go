package memoryauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type Grants interface {
	ReserveUse(context.Context, string, authorization.GrantPresentation, *wire.AuthorizationAction, uint64) (authorization.UsePermit, error)
	Verify(context.Context, string, authorization.GrantPresentation, *wire.AuthorizationAction) (*wire.GrantRecord, error)
	LookupUse(context.Context, authorization.GrantPresentation) (authorization.UsePermit, error)
}

// Peer is derived by a trusted host from an authenticated transport. Signed
// material or ordinary Memory requests never supply their own peer binding.
type Peer struct{ Audience, Presenter, CertificateSHA256 string }
type ReadPermits struct {
	authority *Authority
	grants    Grants
	peer      Peer
}

func NewReadPermits(authority *Authority, grants Grants, peer Peer) (*ReadPermits, error) {
	digest, e := base64.RawURLEncoding.DecodeString(peer.CertificateSHA256)
	if authority == nil || grants == nil || !valid(peer.Audience) || !valid(peer.Presenter) || e != nil || len(digest) != 32 {
		return nil, memory.Invalid
	}
	return &ReadPermits{authority, grants, peer}, nil
}
func (r *ReadPermits) plan(ctx context.Context, b memory.Binding, in memory.ReadIntent) (authorization.GrantPresentation, *wire.AuthorizationAction, error) {
	p := authorization.GrantPresentation{Namespace: b.Namespace, Subject: b.Subject, Audience: r.peer.Audience, Presenter: r.peer.Presenter, CertificateSHA256: r.peer.CertificateSHA256, OperationID: in.ID, SemanticSHA256: in.SemanticSHA256}
	c, ok := r.authority.collections[key{b.Namespace, in.Collection}]
	if !ok || in.Purpose != c.Purpose || (in.Method != "query" && in.Method != "get") || !contains(c.Processing, b.Location) || !contains(c.Recipients, b.Recipient) {
		return p, nil, memory.Denied
	}
	action := &wire.AuthorizationAction{Resource: c.Resource, Action: "memory." + in.Method, Purpose: c.Purpose, Location: b.Recipient}
	actions := readUseActions(action, b)
	view, e := r.authority.views.ViewActions(ctx, b.Token, actions)
	if e != nil || view.Identity.Namespace != b.Namespace || view.Identity.Subject != b.Subject || len(view.Allowed) != len(actions) {
		return p, nil, memory.Denied
	}
	for _, allowed := range view.Allowed {
		if !allowed {
			return p, nil, memory.Denied
		}
	}
	return p, action, nil
}
func permitID(p authorization.UsePermit) string {
	raw, _ := json.Marshal(p)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (r *ReadPermits) Reserve(ctx context.Context, b memory.Binding, in memory.ReadIntent, material string) (string, error) {
	p, action, e := r.plan(ctx, b, in)
	if e != nil {
		return "", e
	}
	use, e := r.grants.ReserveUse(ctx, material, p, action, 1)
	if e != nil {
		return "", memory.Denied
	}
	return permitID(use), nil
}
func (r *ReadPermits) Validate(ctx context.Context, b memory.Binding, in memory.ReadIntent, material, id string) error {
	p, action, e := r.plan(ctx, b, in)
	if e != nil {
		return e
	}
	record, e := r.grants.Verify(ctx, material, p, action)
	if e != nil {
		return memory.Denied
	}
	use, e := r.grants.LookupUse(ctx, p)
	if e != nil {
		return memory.Denied
	}
	raw, e := (proto.MarshalOptions{Deterministic: true}).Marshal(action)
	if e != nil {
		return memory.Denied
	}
	digest := sha256.Sum256(raw)
	if use.GrantID != record.Id || use.Units != 1 || use.ActionSHA256 != hex.EncodeToString(digest[:]) || permitID(use) != id {
		return memory.Denied
	}
	return nil
}

// Authorize verifies the original signed read authority without allocating a
// use. Context replay must call this even when its body is already retained.
// First disclosure still goes through Reserve and the durable read binding.
func (r *ReadPermits) Authorize(ctx context.Context, b memory.Binding, in memory.ReadIntent, material string) error {
	p, action, e := r.plan(ctx, b, in)
	if e != nil {
		return e
	}
	if _, e = r.grants.Verify(ctx, material, p, action); e != nil {
		return memory.Denied
	}
	return nil
}
