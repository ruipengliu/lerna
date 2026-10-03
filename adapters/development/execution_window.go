package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type startControlCommand struct {
	RequestDigest string      `json:"request_digest"`
	Command       api.Command `json:"command"`
}

// 原 Prepared Attempt 的独立控制请求在发送前固定，失答后只查/重传它。
// 原 Invoke 的首个窗口、UseReceipt 和业务 Deadline 均不改写。
func (e executionAuthority) PrepareControlWindow(ctx context.Context, scope runtime.Scope, request execution.StartRequest) (api.ControlSnapshot, error) {
	if !api.Equal(scope, e.a.Scope) || request.Invoke.TaskRef.OwnerID != scope.OwnerID || !api.ValidID(request.AttemptID) {
		return api.ControlSnapshot{}, api.E("unsupported", "current_control_source_not_configured")
	}
	input := task.ControlWindowInput{TaskID: request.Invoke.TaskRef.ObjectID, ExpectedGoalRevision: request.Invoke.GoalRevision, ExpectedControlRevision: request.Invoke.ControlRevision, ReceiverID: scope.OwnerID, OperationRef: scope.Ref(request.Invoke.OperationID, 1)}
	digest, err := api.Digest(struct {
		Input     task.ControlWindowInput `json:"input"`
		Principal runtime.Auth            `json:"principal"`
		Deadline  string                  `json:"deadline"`
	}{input, request.Auth, request.Invoke.Deadline})
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	var frozen startControlCommand
	status, err := e.a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "platform.start_control_commands", request.AttemptID, &frozen)
		if err == nil {
			if frozen.RequestDigest != digest {
				return api.E("idempotency_conflict", "original_start_control_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		frozen = startControlCommand{RequestDigest: digest, Command: api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: stableID("command", "start-control/"+request.AttemptID), TargetID: input.TaskID, Method: "task.control_window", ExpiresAt: request.Invoke.Deadline, Payload: api.Raw(input)}}
		return tx.Create(ctx, "platform.start_control_commands", request.AttemptID, request.Invoke.OperationID, frozen)
	})
	if status == runtime.CommitUnknown {
		return api.ControlSnapshot{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	receipt, err := e.a.Dispatcher.Command(ctx, e.a.ServiceAuth, api.Raw(frozen.Command))
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	if receipt.Error != nil {
		return api.ControlSnapshot{}, receipt.Error
	}
	if receipt.Stage != "applied" {
		return api.ControlSnapshot{}, api.E("dependency_unavailable", "original_start_control_pending")
	}
	var window api.ControlSnapshot
	if err = api.Decode(receipt.Output, &window); err != nil {
		return api.ControlSnapshot{}, err
	}
	if err = (executionBridge{e.a}).Control(ctx, scope, scope.OwnerID, window); err != nil {
		return api.ControlSnapshot{}, err
	}
	return window, nil
}
