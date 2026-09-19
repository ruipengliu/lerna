// Package router routes binary requests through a host-authenticated
// finite directory of namespace sessions. Resolve must retain historical exact
// versions; a caller never supplies a token or arbitrary backend address.
package router

import (
	"context"
	"google.golang.org/protobuf/proto"
	executionlocal "lerna/adapters/execution/local"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/protocol"
)

type Resolver interface {
	Resolve(context.Context, string) ([]Bound, error)
}
type Bound struct {
	Ref     catalog.Ref
	Service executionlocal.Service
}
type Router struct {
	Resolver Resolver
	Store    catalog.VersionLock
}
type guarded struct {
	executionlocal.Service
	admission catalog.Admission
}

func (g guarded) Invoke(c context.Context, r execution.Request, m string) (execution.Receipt, error) {
	return g.admission.Invoke(c, r, m)
}
func (r Router) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	invalid := func() error { return &authorization.Error{Code: authorization.Invalid} }
	if r.Resolver == nil || r.Store == nil || len(data) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	req := new(wire.CapabilityRequest)
	if proto.Unmarshal(data, req) != nil || len(req.Namespace) == 0 || len(req.Namespace) > 128 {
		return nil, invalid()
	}
	entries, e := r.Resolver.Resolve(ctx, req.Namespace)
	if e != nil {
		return nil, e
	}
	if len(entries) == 0 || len(entries) > 32 {
		return nil, invalid()
	}
	var selected *Bound
	if in := req.GetInvoke(); in != nil {
		v := in.GetInvocation()
		for i := range entries {
			ref := entries[i].Ref
			if ref.Namespace == req.Namespace && ref.Name == v.GetCapability() && ref.Version == v.GetVersion() && ref.Implementation == v.GetImplementation() && ref.ImplementationVersion == v.GetImplementationVersion() && ref.Digest == v.GetDescriptorSha256() {
				selected = &entries[i]
				break
			}
		}
	} else {
		for i := range entries {
			var err error
			switch v := req.Body.(type) {
			case *wire.CapabilityRequest_GetInvocation:
				_, err = entries[i].Service.GetInvocation(ctx, v.GetInvocation)
			case *wire.CapabilityRequest_Reconcile:
				_, err = entries[i].Service.GetInvocation(ctx, v.Reconcile)
			case *wire.CapabilityRequest_RequestCancel:
				_, err = entries[i].Service.GetInvocation(ctx, v.RequestCancel.GetInvocationId())
			case *wire.CapabilityRequest_GetCancel:
				_, err = entries[i].Service.GetCancel(ctx, v.GetCancel)
			// Resource control is shared within this trusted namespace authority.
			case *wire.CapabilityRequest_GetResourceControl, *wire.CapabilityRequest_RequestResourceControl, *wire.CapabilityRequest_LookupResourceControl:
				err = nil
			default:
				return nil, &authorization.Error{Code: authorization.Unsupported}
			}
			if err == nil {
				selected = &entries[i]
				break
			}
			if !authorization.Is(err, authorization.Denied) && !authorization.Is(err, authorization.NotFound) {
				return nil, err
			}
		}
	}
	if selected == nil {
		return nil, &authorization.Error{Code: authorization.Denied}
	}
	s := guarded{selected.Service, catalog.Admission{Store: r.Store, Executor: selected.Service, Ref: selected.Ref}}
	return executionlocal.Bind(s, req.Namespace).Exchange(ctx, data)
}
