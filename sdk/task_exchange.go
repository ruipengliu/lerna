package sdk

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

func exchangeTask(ctx context.Context, transport Transport, namespace string, in *wire.TaskRequest) (*wire.TaskResponse, error) {
	invalid := func() (*wire.TaskResponse, error) { return nil, &authorization.Error{Code: authorization.Invalid} }
	if transport == nil || namespace == "" {
		return invalid()
	}
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	in.MessageId = id
	in.Namespace = namespace
	if proto.Size(in) > protocol.MaxMessageBytes {
		return invalid()
	}
	data, err := proto.Marshal(in)
	if err != nil {
		return invalid()
	}
	data, err = transport.Exchange(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(data) > protocol.MaxMessageBytes {
		return invalid()
	}
	out := new(wire.TaskResponse)
	if err := proto.Unmarshal(data, out); err != nil {
		return invalid()
	}
	if out.MessageId == "" || out.MessageId == id || out.ReplyTo != id || out.Namespace != namespace || !taskwire.Known(out.ProtoReflect()) {
		return invalid()
	}
	if f := out.GetFailure(); f != nil {
		switch code := authorization.Code(f.Code); code {
		case authorization.Unauthenticated, authorization.Denied, authorization.Invalid, authorization.Unsupported, authorization.Conflict, authorization.IdentityConflict, authorization.Expired, authorization.NotFound, authorization.TimeUntrusted, authorization.Unavailable, authorization.OutcomeUnknown:
			return nil, &authorization.Error{Code: code}
		default:
			return invalid()
		}
	}
	return out, nil
}
