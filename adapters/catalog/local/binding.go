// Package local adds discovery to the capability protocol. Execution is
// routed by host-bound authenticated namespace sessions, not caller credentials.
package local

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	"lerna/catalog"
	wire "lerna/gen/harness/v1"
	"lerna/internal/catalogwire"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type Catalog interface {
	List(context.Context, catalog.Query) (catalog.Page, error)
	Search(context.Context, catalog.Query) (catalog.Page, error)
	Describe(context.Context, catalog.Ref) (catalog.Entry, error)
}
type Transport interface {
	Exchange(context.Context, []byte) ([]byte, error)
}
type Binding struct {
	service   Catalog
	namespace string
	execution Transport
}

func Bind(service Catalog, namespace string, execution Transport) *Binding {
	return &Binding{service, namespace, execution}
}
func (b *Binding) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	invalid := func() error { return &authorization.Error{Code: authorization.Invalid} }
	if b.service == nil || len(data) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	req := new(wire.CapabilityRequest)
	if proto.Unmarshal(data, req) != nil {
		return nil, invalid()
	}
	id, e := randomid.New()
	if e != nil {
		return nil, e
	}
	out := &wire.CapabilityResponse{MessageId: id, ReplyTo: req.MessageId, Namespace: req.Namespace, Evidence: "durable_capability_execution"}
	if req.MessageId == "" || len(req.MessageId) > 128 || !taskwire.Known(req.ProtoReflect()) {
		e = invalid()
	} else {
		switch v := req.Body.(type) {
		case *wire.CapabilityRequest_List:
			if req.Namespace != b.namespace {
				e = invalid()
				break
			}
			var p catalog.Page
			p, e = b.service.List(ctx, catalogwire.DecodeQuery(v.List))
			out.Body = &wire.CapabilityResponse_CatalogPage{CatalogPage: catalogwire.Page(p)}
		case *wire.CapabilityRequest_Search:
			if req.Namespace != b.namespace {
				e = invalid()
				break
			}
			var p catalog.Page
			p, e = b.service.Search(ctx, catalogwire.DecodeQuery(v.Search))
			out.Body = &wire.CapabilityResponse_CatalogPage{CatalogPage: catalogwire.Page(p)}
		case *wire.CapabilityRequest_Describe:
			if req.Namespace != b.namespace {
				e = invalid()
				break
			}
			var entry catalog.Entry
			entry, e = b.service.Describe(ctx, catalogwire.DecodeRef(v.Describe))
			if e == nil {
				var raw []byte
				raw, e = json.Marshal(entry)
				out.Body = &wire.CapabilityResponse_CatalogDeclaration{CatalogDeclaration: &wire.CatalogDeclaration{Ref: catalogwire.Ref(entry.Ref), DeclarationJson: raw}}
			}
		default:
			if b.execution != nil {
				return b.execution.Exchange(ctx, data)
			}
			e = &authorization.Error{Code: authorization.Unsupported}
		}
	}
	if e != nil {
		code := authorization.Unavailable
		var auth *authorization.Error
		if errors.As(e, &auth) {
			code = auth.Code
		}
		out.Body = &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: string(code)}}
	}
	if proto.Size(out) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	return proto.Marshal(out)
}
