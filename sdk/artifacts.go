package sdk

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type ContentClient struct {
	transport Transport
	namespace string
}

func NewContentClient(t Transport, namespace string) *ContentClient {
	return &ContentClient{t, namespace}
}

// Call accepts the fixed content request contract. Credentials and actual
// processing/recipient locations belong to the trusted transport binding.
func (c *ContentClient) Call(ctx context.Context, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	if in == nil || c.transport == nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	in = proto.Clone(in).(*wire.ContentRequest)
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	in.MessageId = id
	in.Namespace = c.namespace
	if proto.Size(in) > protocol.MaxMessageBytes {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	data, err := proto.Marshal(in)
	if err != nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	data, err = c.transport.Exchange(ctx, data)
	if err != nil {
		return nil, err
	}
	out := new(wire.ContentResponse)
	if len(data) > protocol.MaxMessageBytes || proto.Unmarshal(data, out) != nil || !taskwire.Known(out.ProtoReflect()) || out.Namespace != c.namespace || out.ReplyTo != id || out.MessageId == "" || out.MessageId == id {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	if out.Code != "" {
		if out.Record != nil || len(out.Data) != 0 || out.OperationId != "" {
			return nil, artifacts.Error("INVALID_ARGUMENT")
		}
		switch out.Code {
		case "UNAUTHENTICATED", "PERMISSION_DENIED", "INVALID_ARGUMENT", "UNSUPPORTED", "VERSION_CONFLICT", "IDENTITY_CONFLICT", "ADMISSION_EXPIRED", "NOT_FOUND", "TIME_UNTRUSTED", "UNAVAILABLE", "OUTCOME_UNKNOWN", "CONTENT_UNAVAILABLE", "CONTENT_MISSING", "CONTENT_CORRUPT", "CAPACITY_EXCEEDED":
			return nil, artifacts.Error(out.Code)
		default:
			return nil, artifacts.Error("INVALID_ARGUMENT")
		}
	}
	r := out.Record
	if r == nil || r.Ref == nil || r.Ref.Namespace != c.namespace || r.Ref.Key == "" || r.Ref.Revision != 1 || r.Spec == nil || r.LifecycleRevision == 0 || out.OperationId != in.OperationId {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	if in.Ref != nil && !proto.Equal(in.Ref, r.Ref) {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	switch r.State {
	case "available", "expired", "cleaning", "cleaned", "missing", "corrupt", "unavailable":
	default:
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	if in.Method == "READ" {
		if len(out.Data) != int(in.Limit) || r.State != "available" {
			return nil, artifacts.Error("INVALID_ARGUMENT")
		}
	} else if len(out.Data) != 0 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return out, nil
}
