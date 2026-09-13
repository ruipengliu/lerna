package sdk

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"lerna/tasks"
)

type TaskClient struct {
	transport Transport
	namespace string
}

func NewTaskClient(transport Transport, namespace string) *TaskClient {
	return &TaskClient{transport, namespace}
}
func (c *TaskClient) call(ctx context.Context, in *wire.TaskRequest) (tasks.Task, error) {
	invalid := func() (tasks.Task, error) { return tasks.Task{}, &authorization.Error{Code: authorization.Invalid} }
	out, err := exchangeTask(ctx, c.transport, c.namespace, in)
	if err != nil {
		return tasks.Task{}, err
	}
	task := out.GetTask()
	if task == nil || task.GetRef().GetNamespace() != c.namespace || task.GetRef().GetTaskId() == "" || !tasks.ValidSnapshot(taskwire.Decode(task)) || task.Owner == "" || task.OwnerEpoch != 1 || task.Constraints == nil || out.Evidence != "durable_task_admission" {
		return invalid()
	}
	return taskwire.Decode(task), nil
}
func (c *TaskClient) Submit(ctx context.Context, in tasks.Submission) (tasks.Task, error) {
	if in.Namespace != c.namespace {
		return tasks.Task{}, &authorization.Error{Code: authorization.Denied}
	}
	return c.call(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Submit{Submit: &wire.DurableSubmission{OperationId: in.OperationID, Goal: in.Goal, InputRefs: in.InputRefs, Constraints: &wire.TaskConstraints{MaxSteps: in.Constraints.MaxSteps, DeadlineUnix: in.Constraints.DeadlineUnix, ModelRequests: in.Constraints.ModelRequests, ModelTokens: in.Constraints.ModelTokens}}}})
}
func (c *TaskClient) Get(ctx context.Context, ref tasks.Ref) (tasks.Task, error) {
	if ref.Namespace != c.namespace {
		return tasks.Task{}, &authorization.Error{Code: authorization.Denied}
	}
	out, err := c.call(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_Get{Get: &wire.TaskRef{Namespace: ref.Namespace, TaskId: ref.TaskID}}})
	if err == nil && out.Ref != ref {
		return tasks.Task{}, &authorization.Error{Code: authorization.Invalid}
	}
	return out, err
}
func (c *TaskClient) LookupOperation(ctx context.Context, id string) (tasks.Task, error) {
	return c.call(ctx, &wire.TaskRequest{Body: &wire.TaskRequest_LookupOperation{LookupOperation: id}})
}
