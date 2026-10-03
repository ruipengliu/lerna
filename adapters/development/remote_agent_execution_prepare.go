package development

import (
	"context"
	"reflect"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原prepared Attempt在新的Execution入口恢复时仍需本次有限父证明。
// 只核本方已冻结的准确Invoke/Intent，再取得当前父范围；不重新读取
// 业务正文、创建Use、签窗或改变原Attempt。最终StartBarrier仍完整验真。
func (e executionAuthority) PrepareStartGate(ctx context.Context, scope runtime.Scope, request execution.StartRequest) (context.Context, error) {
	if e.a.RemoteAgent == nil {
		return ctx, nil
	}
	if scope != e.a.Scope || request.Invoke.TaskRef.OwnerID != scope.OwnerID || request.Invoke.TaskRef.TenantID != scope.TenantID || !api.ValidID(request.AttemptID) {
		return ctx, api.E("forbidden", "original_remote_start_scope_required")
	}
	var original task.OperationIntent
	var foreign bool
	status, err := e.a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		var err error
		original, err = e.a.Task.OperationIntentTx(ctx, tx, request.Invoke.OperationID)
		if err != nil {
			return err
		}
		if err = currentCredentialTx(ctx, tx, request.Auth); err != nil {
			return err
		}
		incoming, found, err := e.a.Task.ReadIncomingSourceTx(ctx, tx, e.a.ServiceAuth, original.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		foreign = found && incoming.OwnerID != scope.OwnerID
		if !foreign {
			return nil
		}
		var fixed encodedIntent
		if _, err = tx.Get(ctx, "platform.execution_intents", original.OperationID, &fixed); err != nil {
			return err
		}
		if fixed.Command == nil || fixed.Command.Method != "execution.invoke" || fixed.Command.CommandID != original.CommandID || fixed.Command.LogicalServiceID != scope.OwnerID || fixed.AdmissionHash != original.IntentHash || !reflect.DeepEqual(fixed.Domain, request.Intent) {
			return api.E("idempotency_conflict", "original_remote_start_changed")
		}
		var invoke execution.InvokeInput
		if err = api.Decode(fixed.Command.Payload, &invoke); err != nil {
			return err
		}
		if !reflect.DeepEqual(invoke, request.Invoke) || invoke.IntentHash != fixed.Hash || invoke.IntentRef != fixed.Ref {
			return api.E("idempotency_conflict", "original_remote_start_changed")
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil || !foreign {
		return ctx, err
	}
	return e.a.prepareRemoteAgentOperationParent(ctx, scope, original)
}
