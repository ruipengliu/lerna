// Package tasklocal binds a credential to the local binary task protocol.
package tasklocal

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
	"lerna/tasks"
)

type TaskService interface {
	Submit(context.Context, string, tasks.Submission) (tasks.Task, error)
	Get(context.Context, string, tasks.Ref) (tasks.Task, error)
	LookupOperation(context.Context, string, string, string) (tasks.Task, error)
}
type ControlService interface {
	Request(context.Context, string, tasks.ControlRequest) (tasks.ControlReceipt, error)
	Lookup(context.Context, string, string, string) (tasks.ControlReceipt, error)
}
type Binding struct {
	updates    UpdateService
	controls   ControlService
	service    TaskService
	credential string
}

func Bind(service TaskService, credential string) *Binding {
	return &Binding{service: service, credential: credential}
}
func BindManaged(service TaskService, controls ControlService, credential string) *Binding {
	return &Binding{service: service, controls: controls, credential: credential}
}
func (b *Binding) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	invalid := func() error { return &authorization.Error{Code: authorization.Invalid} }
	if b.service == nil || len(data) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	req := new(wire.TaskRequest)
	if err := proto.Unmarshal(data, req); err != nil {
		return nil, invalid()
	}
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	resp := &wire.TaskResponse{MessageId: id, ReplyTo: req.MessageId, Namespace: req.Namespace}
	var task tasks.Task
	var receipt *tasks.ControlReceipt
	if req.MessageId == "" || len(req.MessageId) > 128 || req.Namespace == "" {
		err = invalid()
	} else if !taskwire.Known(req.ProtoReflect()) {
		err = &authorization.Error{Code: authorization.Unsupported}
	} else {
		switch body := req.Body.(type) {
		case *wire.TaskRequest_Submit:
			in := body.Submit
			task, err = b.service.Submit(ctx, b.credential, tasks.Submission{Namespace: req.Namespace, OperationID: in.GetOperationId(), Goal: in.GetGoal(), InputRefs: in.GetInputRefs(), Constraints: tasks.Constraints{MaxSteps: in.GetConstraints().GetMaxSteps(), DeadlineUnix: in.GetConstraints().GetDeadlineUnix(), ModelRequests: in.GetConstraints().GetModelRequests(), ModelTokens: in.GetConstraints().GetModelTokens()}})
		case *wire.TaskRequest_Get:
			if body.Get.GetNamespace() != req.Namespace {
				err = invalid()
			} else {
				task, err = b.service.Get(ctx, b.credential, tasks.Ref{Namespace: req.Namespace, TaskID: body.Get.GetTaskId()})
			}
		case *wire.TaskRequest_LookupOperation:
			task, err = b.service.LookupOperation(ctx, b.credential, req.Namespace, body.LookupOperation)
		case *wire.TaskRequest_Control:
			if b.controls == nil {
				err = &authorization.Error{Code: authorization.Unsupported}
				break
			}
			in := body.Control
			if in.GetRef().GetNamespace() != req.Namespace {
				err = invalid()
				break
			}
			var r tasks.ControlReceipt
			r, err = b.controls.Request(ctx, b.credential, tasks.ControlRequest{OperationID: in.GetOperationId(), Ref: tasks.Ref{Namespace: req.Namespace, TaskID: in.GetRef().GetTaskId()}, ExpectedVersion: in.GetExpectedVersion(), Intent: in.GetIntent(), Reason: in.GetReason()})
			receipt = &r
		case *wire.TaskRequest_LookupControl:
			if b.controls == nil {
				err = &authorization.Error{Code: authorization.Unsupported}
				break
			}
			var r tasks.ControlReceipt
			r, err = b.controls.Lookup(ctx, b.credential, req.Namespace, body.LookupControl)
			receipt = &r
		default:
			var update *wire.TaskResponse
			update, err = b.updateResponse(ctx, req)
			if err == nil {
				resp.Evidence = update.Evidence
				resp.Body = update.Body
			}
		}
	}
	if err != nil {
		code := authorization.Unavailable
		var typed *authorization.Error
		if errors.As(err, &typed) {
			code = typed.Code
		}
		resp.Body = &wire.TaskResponse_Failure{Failure: &wire.AuthorizationFailure{Code: string(code)}}
	} else if receipt != nil {
		resp.Evidence = "durable_task_control"
		resp.Body = &wire.TaskResponse_Control{Control: taskwire.EncodeControl(*receipt)}
	} else if resp.Body == nil {
		resp.Evidence = "durable_task_admission"
		resp.Body = &wire.TaskResponse_Task{Task: taskwire.Encode(task)}
	}
	return proto.Marshal(resp)
}
