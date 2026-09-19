// Package local binds trusted identity and placement outside the wire payload.
package local

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Binding struct {
	service  Content
	identity artifacts.Binding
}

func Bind(s Content, b artifacts.Binding) *Binding { return &Binding{s, b} }
func (b *Binding) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	if b.service == nil || len(data) > protocol.MaxMessageBytes {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	in := new(wire.ContentRequest)
	if proto.Unmarshal(data, in) != nil || !taskwire.Known(in.ProtoReflect()) || in.MessageId == "" || len(in.MessageId) > 128 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	if in.Namespace != b.identity.Namespace {
		return nil, artifacts.Error("PERMISSION_DENIED")
	}
	id, err := randomid.New()
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	out, err := b.service.Call(ctx, b.identity, in)
	if err != nil {
		out = &wire.ContentResponse{Code: artifacts.Code(err)}
	}
	if out == nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	out.MessageId = id
	out.ReplyTo = in.MessageId
	out.Namespace = in.Namespace
	if proto.Size(out) > protocol.MaxMessageBytes {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	return proto.Marshal(out)
}
