package sdk

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type CapabilityClient struct {
	transport Transport
	namespace string
}

func NewCapabilityClient(t Transport, ns string) *CapabilityClient { return &CapabilityClient{t, ns} }
func (c *CapabilityClient) exchange(ctx context.Context, in *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	invalid := func() (*wire.CapabilityResponse, error) {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	if c.transport == nil || c.namespace == "" {
		return invalid()
	}
	id, e := randomid.New()
	if e != nil {
		return nil, e
	}
	in.MessageId = id
	in.Namespace = c.namespace
	if proto.Size(in) > protocol.MaxMessageBytes {
		return invalid()
	}
	data, e := proto.Marshal(in)
	if e != nil {
		return invalid()
	}
	data, e = c.transport.Exchange(ctx, data)
	if e != nil {
		return nil, e
	}
	out := new(wire.CapabilityResponse)
	if len(data) > protocol.MaxMessageBytes || proto.Unmarshal(data, out) != nil || !taskwire.Known(out.ProtoReflect()) || out.MessageId == "" || out.MessageId == id || out.ReplyTo != id || out.Namespace != c.namespace || out.Evidence != "durable_capability_execution" {
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
func (c *CapabilityClient) Invoke(ctx context.Context, r execution.Request, material string) (execution.Receipt, error) {
	if r.Qualification.Ref.Namespace != c.namespace {
		return execution.Receipt{}, &authorization.Error{Code: authorization.Denied}
	}
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: executionwire.Encode(r), GrantMaterial: material}}})
	if e != nil {
		return execution.Receipt{}, e
	}
	v := out.GetReceipt()
	if v.GetOperationId() != r.OperationID || v.GetRevision() != 1 {
		return execution.Receipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	return execution.Receipt{OperationID: v.OperationId, Revision: v.Revision}, nil
}
func (c *CapabilityClient) read(ctx context.Context, op string, reconcile bool) (execution.Record, error) {
	in := &wire.CapabilityRequest{Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: op}}
	if reconcile {
		in.Body = &wire.CapabilityRequest_Reconcile{Reconcile: op}
	}
	out, e := c.exchange(ctx, in)
	if e != nil {
		return execution.Record{}, e
	}
	r := executionwire.Record(out.GetSnapshot())
	if r.Request.OperationID != op || r.Request.Qualification.Ref.Namespace != c.namespace || r.Revision < 1 || r.Applied > r.Revision || !execution.ValidRecord(r) {
		return execution.Record{}, &authorization.Error{Code: authorization.Invalid}
	}
	return r, nil
}
func (c *CapabilityClient) GetInvocation(ctx context.Context, op string) (execution.Record, error) {
	return c.read(ctx, op, false)
}
func (c *CapabilityClient) Reconcile(ctx context.Context, op string) (execution.Record, error) {
	return c.read(ctx, op, true)
}
func (c *CapabilityClient) RequestCancel(ctx context.Context, in execution.CancelRequest) (execution.Receipt, error) {
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_RequestCancel{RequestCancel: &wire.CancelInvocation{OperationId: in.OperationID, InvocationId: in.Invocation}}})
	if e != nil {
		return execution.Receipt{}, e
	}
	v := out.GetReceipt()
	if v.GetOperationId() != in.OperationID || v.GetRevision() != 1 {
		return execution.Receipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	return execution.Receipt{OperationID: v.OperationId, Revision: v.Revision}, nil
}
func (c *CapabilityClient) GetCancel(ctx context.Context, op string) (execution.CancelRecord, error) {
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_GetCancel{GetCancel: op}})
	if e != nil {
		return execution.CancelRecord{}, e
	}
	v := out.GetCancellation()
	if v.GetRequest().GetOperationId() != op || v.GetReceipt().GetOperationId() != op || v.GetReceipt().GetRevision() != 1 || v.GetRequest().GetInvocationId() == "" {
		return execution.CancelRecord{}, &authorization.Error{Code: authorization.Invalid}
	}
	switch v.Progress {
	case "ACCEPTED", "UNKNOWN", "REQUESTED", "STOPPED", "NOT_PREVENTED", "UNSUPPORTED":
	default:
		return execution.CancelRecord{}, &authorization.Error{Code: authorization.Invalid}
	}
	return execution.CancelRecord{Request: execution.CancelRequest{OperationID: op, Invocation: v.Request.InvocationId}, Receipt: execution.Receipt{OperationID: op, Revision: 1}, Progress: v.Progress, Sent: v.PossiblySent}, nil
}
