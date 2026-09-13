package sdk

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"lerna/tasks"
)

type UpdateClient struct {
	transport Transport
	namespace string
}

func NewUpdateClient(t Transport, namespace string) *UpdateClient { return &UpdateClient{t, namespace} }
func (c *UpdateClient) receipt(ctx context.Context, in *wire.TaskRequest, op string, ref tasks.Ref, kind, interaction string) (tasks.InputReceipt, error) {
	out, e := exchangeTask(ctx, c.transport, c.namespace, in)
	if e != nil {
		return tasks.InputReceipt{}, e
	}
	v := taskwire.DecodeInputReceipt(out.GetInputReceipt())
	if out.Evidence != "durable_task_inputs" || v.OperationID != op || v.Ref.Namespace != c.namespace || v.Ref.TaskID == "" || v.Version == 0 || (ref != (tasks.Ref{}) && v.Ref != ref) || (kind != "" && v.Kind != kind) || (kind == "reply" && v.InteractionID != interaction) {
		return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	switch v.Kind {
	case "append", "revise", "limits":
		if v.InteractionID != "" {
			return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Invalid}
		}
	case "reply":
		if v.InteractionID == "" {
			return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Invalid}
		}
	default:
		return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Unsupported}
	}
	return v, nil
}
func (c *UpdateClient) SubmitUpdate(ctx context.Context, r tasks.UpdateRequest) (tasks.InputReceipt, error) {
	if r.Ref.Namespace != c.namespace {
		return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Denied}
	}
	return c.receipt(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Update{Update: taskwire.EncodeUpdate(r)}}, r.OperationID, r.Ref, r.Intent, "")
}
func (c *UpdateClient) ProvideInput(ctx context.Context, r tasks.InputRequest) (tasks.InputReceipt, error) {
	if r.Ref.Namespace != c.namespace {
		return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Denied}
	}
	return c.receipt(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Input{Input: taskwire.EncodeInput(r)}}, r.OperationID, r.Ref, "reply", r.InteractionID)
}
func (c *UpdateClient) AdjustLimits(ctx context.Context, r tasks.AdjustRequest) (tasks.InputReceipt, error) {
	if r.Ref.Namespace != c.namespace {
		return tasks.InputReceipt{}, &authorization.Error{Code: authorization.Denied}
	}
	return c.receipt(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Adjust{Adjust: taskwire.EncodeAdjustment(r)}}, r.OperationID, r.Ref, "limits", "")
}
func (c *UpdateClient) Lookup(ctx context.Context, op string) (tasks.InputReceipt, error) {
	return c.receipt(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_LookupInput{LookupInput: op}}, op, tasks.Ref{}, "", "")
}
func (c *UpdateClient) Interactions(ctx context.Context, ref tasks.Ref) (tasks.InteractionPage, error) {
	if ref.Namespace != c.namespace {
		return tasks.InteractionPage{}, &authorization.Error{Code: authorization.Denied}
	}
	out, e := exchangeTask(ctx, c.transport, c.namespace, &wire.TaskRequest{Body: &wire.TaskRequest_Interactions{Interactions: &wire.TaskRef{Namespace: ref.Namespace, TaskId: ref.TaskID}}})
	if e != nil {
		return tasks.InteractionPage{}, e
	}
	v := taskwire.DecodeInteractions(out.GetInteractions())
	if out.Evidence != "durable_task_inputs" || v.Ref != ref || v.Version == 0 || !tasks.ValidInteractions(v) {
		return tasks.InteractionPage{}, &authorization.Error{Code: authorization.Invalid}
	}
	return v, nil
}
