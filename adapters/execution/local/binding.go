// Package local is the bounded local binary capability binding. The
// host constructs the service's authenticated peer and worker bindings.
package local

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type Service interface {
	RequestResourceControl(context.Context, execution.ResourceControlRequest) (execution.ResourceControlReceipt, error)
	GetResourceControl(context.Context, execution.ResourceRef) (execution.ResourceControl, error)
	LookupResourceControl(context.Context, string) (execution.ResourceControlReceipt, error)
	Invoke(context.Context, execution.Request, string) (execution.Receipt, error)
	GetInvocation(context.Context, string) (execution.Record, error)
	Reconcile(context.Context, string) (execution.Record, error)
	RequestCancel(context.Context, execution.CancelRequest) (execution.Receipt, error)
	GetCancel(context.Context, string) (execution.CancelRecord, error)
}
type Binding struct {
	service   Service
	namespace string
}

func Bind(s Service, namespace string) *Binding { return &Binding{s, namespace} }
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
	if req.MessageId == "" || len(req.MessageId) > 128 || req.Namespace != b.namespace {
		e = invalid()
	} else if !taskwire.Known(req.ProtoReflect()) {
		e = &authorization.Error{Code: authorization.Unsupported}
	} else {
		switch v := req.Body.(type) {
		case *wire.CapabilityRequest_Invoke:
			r := executionwire.Decode(v.Invoke.GetInvocation())
			if r.Qualification.Ref.Namespace != b.namespace {
				e = invalid()
				break
			}
			var receipt execution.Receipt
			receipt, e = b.service.Invoke(ctx, r, v.Invoke.GetGrantMaterial())
			out.Body = &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: receipt.OperationID, Revision: receipt.Revision}}
		case *wire.CapabilityRequest_GetInvocation:
			var r execution.Record
			r, e = b.service.GetInvocation(ctx, v.GetInvocation)
			out.Body = &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(r)}
		case *wire.CapabilityRequest_Reconcile:
			var r execution.Record
			_, e = b.service.Reconcile(ctx, v.Reconcile)
			if e == nil {
				r, e = b.service.GetInvocation(ctx, v.Reconcile)
			}
			out.Body = &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(r)}
		case *wire.CapabilityRequest_RequestCancel:
			var receipt execution.Receipt
			receipt, e = b.service.RequestCancel(ctx, execution.CancelRequest{OperationID: v.RequestCancel.GetOperationId(), Invocation: v.RequestCancel.GetInvocationId()})
			out.Body = &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: receipt.OperationID, Revision: receipt.Revision}}
		case *wire.CapabilityRequest_GetCancel:
			var c execution.CancelRecord
			c, e = b.service.GetCancel(ctx, v.GetCancel)
			out.Body = &wire.CapabilityResponse_Cancellation{Cancellation: &wire.CancellationSnapshot{Request: &wire.CancelInvocation{OperationId: c.Request.OperationID, InvocationId: c.Request.Invocation}, Receipt: &wire.InvocationReceipt{OperationId: c.Receipt.OperationID, Revision: c.Receipt.Revision}, Progress: c.Progress, PossiblySent: c.Sent}}
		case *wire.CapabilityRequest_RequestResourceControl:
			in := v.RequestResourceControl
			var r execution.ResourceControlReceipt
			r, e = b.service.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: in.GetOperationId(), Resource: executionwire.ResourceRef(in.GetResource()), Intent: in.GetIntent(), ExpectedVersion: in.GetExpectedVersion()})
			out.Body = &wire.CapabilityResponse_ResourceReceipt{ResourceReceipt: executionwire.ResourceReceipt(r)}
		case *wire.CapabilityRequest_GetResourceControl:
			var r execution.ResourceControl
			r, e = b.service.GetResourceControl(ctx, executionwire.ResourceRef(v.GetResourceControl))
			out.Body = &wire.CapabilityResponse_ResourceControl{ResourceControl: executionwire.ResourceSnapshot(r)}
		case *wire.CapabilityRequest_LookupResourceControl:
			var r execution.ResourceControlReceipt
			r, e = b.service.LookupResourceControl(ctx, v.LookupResourceControl)
			out.Body = &wire.CapabilityResponse_ResourceReceipt{ResourceReceipt: executionwire.ResourceReceipt(r)}
		default:
			e = &authorization.Error{Code: authorization.Unsupported}
		}
	}
	if e != nil {
		code := authorization.Unavailable
		var a *authorization.Error
		if errors.As(e, &a) {
			code = a.Code
		}
		out.Body = &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: string(code)}}
	}
	if proto.Size(out) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	return proto.Marshal(out)
}
