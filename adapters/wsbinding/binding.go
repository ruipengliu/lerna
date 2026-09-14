package wsbinding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/cataloglocal"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
)

// Binding is installed by the trusted host for an exact peer certificate and
// subject. Disclose must check current destination/source policy and permission
// for the requested data, including queue delay. It is mandatory, not a default
// allow hook. Domain services retain their own authorization and credentials.
// All supplied services must honor context cancellation.
type Binding struct {
	Journal       *Journal
	Authorization *authorization.OfflineView
	// Retain checks permission and source policy for persisting this request,
	// including its node-bound grant material. Required for reliable delivery.
	Retain    func(context.Context, authorization.GrantPresentation, *wire.CapabilityRequest) error
	Peer      authorization.GrantPresentation
	Catalog   cataloglocal.Catalog
	Execution *execution.Service
	Disclose  func(context.Context, authorization.GrantPresentation, *wire.CapabilityRequest) error
}
type Resolve func(context.Context, authorization.GrantPresentation) (*Binding, error)

func method(r *wire.CapabilityRequest) string {
	switch r.GetBody().(type) {
	case *wire.CapabilityRequest_AuthorizationSync:
		return "authorization.sync.v1"
	case *wire.CapabilityRequest_List, *wire.CapabilityRequest_Search, *wire.CapabilityRequest_Describe:
		return "catalog.read.v1"
	case *wire.CapabilityRequest_GetInvocation:
		return "invocation.read.v1"
	}
	return ""
}
func (b *Binding) query(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	if b == nil || b.Peer != p || b.Disclose == nil || r.Namespace != p.Namespace {
		return nil, failure(authorization.Denied)
	}
	if method(r) == "" {
		return nil, failure(authorization.Unsupported)
	}
	if err := b.Disclose(ctx, p, r); err != nil {
		return nil, err
	}
	var out *wire.CapabilityResponse
	if op := r.GetGetInvocation(); op != "" {
		if b.Execution == nil {
			return nil, failure(authorization.Unsupported)
		}
		if err := b.Execution.MatchPeer(p); err != nil {
			return nil, err
		}
		v, err := b.Execution.ReadRemoteInvocation(ctx, op)
		if err != nil {
			return nil, err
		}
		out = &wire.CapabilityResponse{Body: &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(v)}}
	} else if method(r) == "authorization.sync.v1" {
		if b.Authorization == nil {
			return nil, failure(authorization.Unsupported)
		}
		if err := b.Authorization.MatchRecipient(p); err != nil {
			return nil, err
		}
		var q authorization.OfflineQuery
		d := json.NewDecoder(bytes.NewReader(r.GetAuthorizationSync()))
		d.DisallowUnknownFields()
		if len(r.GetAuthorizationSync()) > 4096 || d.Decode(&q) != nil {
			return nil, failure(authorization.Invalid)
		}
		page, err := b.Authorization.Read(ctx, q)
		if err != nil {
			return nil, err
		}
		out = &wire.CapabilityResponse{Body: &wire.CapabilityResponse_AuthorizationPage{AuthorizationPage: page}}
	} else if method(r) == "catalog.read.v1" {
		raw, err := proto.Marshal(r)
		if err != nil {
			return nil, err
		}
		raw, err = cataloglocal.Bind(b.Catalog, p.Namespace, nil).Exchange(ctx, raw)
		if err != nil {
			return nil, err
		}
		out = new(wire.CapabilityResponse)
		if err = proto.Unmarshal(raw, out); err != nil {
			return nil, err
		}
	} else {
		return nil, failure(authorization.Invalid)
	}
	if err := b.Disclose(ctx, p, r); err != nil {
		return nil, err
	}
	return out, nil
}
func code(err error) string {
	var a *authorization.Error
	if errors.As(err, &a) {
		return string(a.Code)
	}
	return string(authorization.Unavailable)
}
