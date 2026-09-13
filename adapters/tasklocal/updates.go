package tasklocal

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"lerna/tasks"
)

type UpdateService interface {
	SubmitUpdate(context.Context, string, tasks.UpdateRequest) (tasks.InputReceipt, error)
	ProvideInput(context.Context, string, tasks.InputRequest) (tasks.InputReceipt, error)
	AdjustLimits(context.Context, string, tasks.AdjustRequest) (tasks.InputReceipt, error)
	Lookup(context.Context, string, string, string) (tasks.InputReceipt, error)
	Interactions(context.Context, string, tasks.Ref) (tasks.InteractionPage, error)
}

func (b *Binding) WithUpdates(u UpdateService) *Binding { copy := *b; copy.updates = u; return &copy }
func (b *Binding) updateResponse(ctx context.Context, req *wire.TaskRequest) (*wire.TaskResponse, error) {
	unsupported := &authorization.Error{Code: authorization.Unsupported}
	if b.updates == nil {
		return nil, unsupported
	}
	var r tasks.InputReceipt
	var e error
	validRef := func(ref *wire.TaskRef) bool { return ref.GetNamespace() == req.Namespace }
	switch v := req.Body.(type) {
	case *wire.TaskRequest_Update:
		if !validRef(v.Update.GetRef()) {
			return nil, &authorization.Error{Code: authorization.Invalid}
		}
		r, e = b.updates.SubmitUpdate(ctx, b.credential, taskwire.DecodeUpdate(v.Update))
	case *wire.TaskRequest_Input:
		if !validRef(v.Input.GetRef()) {
			return nil, &authorization.Error{Code: authorization.Invalid}
		}
		r, e = b.updates.ProvideInput(ctx, b.credential, taskwire.DecodeInput(v.Input))
	case *wire.TaskRequest_Adjust:
		if !validRef(v.Adjust.GetRef()) {
			return nil, &authorization.Error{Code: authorization.Invalid}
		}
		r, e = b.updates.AdjustLimits(ctx, b.credential, taskwire.DecodeAdjustment(v.Adjust))
	case *wire.TaskRequest_LookupInput:
		r, e = b.updates.Lookup(ctx, b.credential, req.Namespace, v.LookupInput)
	case *wire.TaskRequest_Interactions:
		if !validRef(v.Interactions) {
			return nil, &authorization.Error{Code: authorization.Invalid}
		}
		page, e := b.updates.Interactions(ctx, b.credential, tasks.Ref{Namespace: req.Namespace, TaskID: v.Interactions.GetTaskId()})
		if e != nil {
			return nil, e
		}
		return &wire.TaskResponse{Evidence: "durable_task_inputs", Body: &wire.TaskResponse_Interactions{Interactions: taskwire.EncodeInteractions(page)}}, nil
	default:
		return nil, unsupported
	}
	if e != nil {
		return nil, e
	}
	return &wire.TaskResponse{Evidence: "durable_task_inputs", Body: &wire.TaskResponse_InputReceipt{InputReceipt: taskwire.EncodeInputReceipt(r)}}, nil
}
