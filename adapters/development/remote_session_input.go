package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

var _ task.TaskInputContextPreparer = remoteTaskGate{}

// PrepareTaskInput 恢复本次原 receiver input 的范围；工作者角色不能代替原提交者。
// Core 在 Tx 外传入原 pending.CommandID，随后仍在完成事务检查 Current。
func (g remoteTaskGate) PrepareTaskInput(ctx context.Context, scope runtime.Scope, actor runtime.Auth, actual api.Task, commandID string) (context.Context, error) {
	if scope != g.a.Scope || actual.OrchestratorID != scope.OwnerID || !api.ValidID(commandID) {
		return ctx, api.E("forbidden", "remote_input_preparation_scope_mismatch")
	}
	var remote bool
	var input bool
	status, err := g.a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, actor); err != nil {
			return err
		}
		ref, found, err := g.a.Task.ReadIncomingSourceTx(ctx, tx, actor, actual.TaskID)
		if err != nil || !found || ref.OwnerID == scope.OwnerID {
			return err
		}
		remote = true
		command, err := tx.LoadCommand(ctx, commandID)
		if err != nil {
			return err
		}
		if command.Command.TargetID != actual.TaskID || command.Receipt.Stage != "accepted" {
			return api.E("idempotency_conflict", "original_remote_input_preparation_changed")
		}
		input = command.Command.Method == "collaboration.input"
		return nil
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil || !remote || g.a.RemoteAgent == nil {
		return ctx, err
	}
	if input {
		prepared, err := g.a.RemoteAgent.PrepareInputContext(ctx, commandID)
		if err != nil {
			return ctx, err
		}
		return g.a.RemoteAgent.PrepareInputPublicationContext(prepared, commandID)
	}
	return g.a.RemoteAgent.PrepareChildContext(ctx, actual.TaskID)
}
