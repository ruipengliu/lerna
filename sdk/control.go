package sdk

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"lerna/tasks"
)

type ControlClient struct {
	transport Transport
	namespace string
}

func NewControlClient(t Transport, namespace string) *ControlClient {
	return &ControlClient{t, namespace}
}
func (c *ControlClient) call(ctx context.Context, in *wire.TaskRequest) (tasks.ControlReceipt, error) {
	invalid := func() (tasks.ControlReceipt, error) {
		return tasks.ControlReceipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	out, err := exchangeTask(ctx, c.transport, c.namespace, in)
	if err != nil {
		return tasks.ControlReceipt{}, err
	}
	r := taskwire.DecodeControl(out.GetControl())
	if out.Evidence != "durable_task_control" || r.OperationID == "" || r.Ref.Namespace != c.namespace || r.Ref.TaskID == "" || r.Version == 0 {
		return invalid()
	}
	switch r.Intent {
	case "RUN", "PAUSE", "CANCEL":
	default:
		return invalid()
	}
	switch r.Outcome {
	case "accepted", "already_COMPLETED", "already_FAILED", "already_CANCELLED":
	default:
		return invalid()
	}
	return r, nil
}
func (c *ControlClient) Request(ctx context.Context, r tasks.ControlRequest) (tasks.ControlReceipt, error) {
	if r.Ref.Namespace != c.namespace {
		return tasks.ControlReceipt{}, &authorization.Error{Code: authorization.Denied}
	}
	out, err := c.call(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Control{Control: &wire.ControlRequest{OperationId: r.OperationID, Ref: &wire.TaskRef{Namespace: r.Ref.Namespace, TaskId: r.Ref.TaskID}, ExpectedVersion: r.ExpectedVersion, Intent: r.Intent, Reason: r.Reason}}})
	if err == nil && (out.OperationID != r.OperationID || out.Ref != r.Ref || out.Intent != r.Intent) {
		return tasks.ControlReceipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	return out, err
}
func (c *ControlClient) Lookup(ctx context.Context, id string) (tasks.ControlReceipt, error) {
	out, err := c.call(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_LookupControl{LookupControl: id}})
	if err == nil && out.OperationID != id {
		return tasks.ControlReceipt{}, &authorization.Error{Code: authorization.Invalid}
	}
	return out, err
}
