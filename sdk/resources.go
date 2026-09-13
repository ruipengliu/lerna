package sdk

import (
	"context"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
)

func (c *CapabilityClient) RequestResourceControl(ctx context.Context, in execution.ResourceControlRequest) (execution.ResourceControlReceipt, error) {
	if in.Resource.Namespace != c.namespace {
		return execution.ResourceControlReceipt{}, &authorization.Error{Code: authorization.Denied}
	}
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_RequestResourceControl{RequestResourceControl: &wire.RequestResourceControl{OperationId: in.OperationID, Resource: executionwire.Resource(in.Resource), Intent: in.Intent, ExpectedVersion: in.ExpectedVersion}}})
	if e != nil {
		return execution.ResourceControlReceipt{}, e
	}
	return c.resourceReceipt(out.GetResourceReceipt(), in.OperationID)
}
func (c *CapabilityClient) resourceReceipt(r *wire.ResourceControlReceipt, op string) (execution.ResourceControlReceipt, error) {
	ref := executionwire.ResourceRef(r.GetResource())
	if r.GetOperationId() != op || r.GetVersion() < 2 || ref.Namespace != c.namespace || ref.Kind == "" || ref.Key == "" {
		return execution.ResourceControlReceipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	return execution.ResourceControlReceipt{OperationID: op, Resource: ref, Version: r.Version}, nil
}
func (c *CapabilityClient) LookupResourceControl(ctx context.Context, op string) (execution.ResourceControlReceipt, error) {
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_LookupResourceControl{LookupResourceControl: op}})
	if e != nil {
		return execution.ResourceControlReceipt{}, e
	}
	return c.resourceReceipt(out.GetResourceReceipt(), op)
}
func (c *CapabilityClient) GetResourceControl(ctx context.Context, ref execution.ResourceRef) (execution.ResourceControl, error) {
	if ref.Namespace != c.namespace {
		return execution.ResourceControl{}, &authorization.Error{Code: authorization.Denied}
	}
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_GetResourceControl{GetResourceControl: executionwire.Resource(ref)}})
	if e != nil {
		return execution.ResourceControl{}, e
	}
	v := out.GetResourceControl()
	r := executionwire.ResourceRef(v.GetResource())
	if r.Namespace != c.namespace || r.Kind == "" || r.Key == "" || v.GetVersion() < 1 || (v.GetIntent() != "TAKEOVER" && v.GetIntent() != "RESUME") || (v.GetProgress() != "ACCEPTED" && v.GetProgress() != "APPLIED") || v.GetBlocking() > 2048 || v.GetChecks() > 16 || len(v.GetLimitation()) > 64 {
		return execution.ResourceControl{}, &authorization.Error{Code: authorization.Invalid}
	}
	return execution.ResourceControl{Resource: r, Version: v.Version, Intent: v.Intent, Progress: v.Progress, ResourceVersion: v.ResourceVersion, Blocking: int(v.Blocking), Checks: v.Checks, Limitation: v.Limitation}, nil
}
