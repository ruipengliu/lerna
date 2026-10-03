package development

import (
	"context"
	"reflect"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原Operation的独立Job不能借上一Decision或磁盘中的肯定证明启动。
// 这里只读准确准入元数据，再在事务外把当前父证明写入本次入口载体。
// PrepareDispatch返回后，Task原短事务仍核当前主体、控制和来源用途。
func (a *App) prepareRemoteAgentOperationParent(ctx context.Context, scope runtime.Scope, original task.OperationIntent) (context.Context, error) {
	if a.RemoteAgent == nil {
		return ctx, nil
	}
	if scope != a.Scope || original.TaskRef.TenantID != scope.TenantID || original.TaskRef.OwnerID != scope.OwnerID {
		return ctx, api.E("forbidden", "original_remote_operation_scope_required")
	}
	var needsPreparation bool
	status, err := a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		fixed, err := a.Task.OperationIntentTx(ctx, tx, original.OperationID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(fixed, original) {
			return api.E("idempotency_conflict", "original_remote_operation_changed")
		}
		actual, err := a.Task.ReadTaskTx(ctx, tx, a.ServiceAuth, fixed.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		if err = currentCredentialTx(ctx, tx, a.ServiceAuth); err != nil {
			return err
		}
		incoming, found, err := a.Task.ReadIncomingSourceTx(ctx, tx, a.ServiceAuth, actual.TaskID)
		if err != nil {
			return err
		}
		if !found || incoming.OwnerID == scope.OwnerID {
			return nil
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(actual.Deadline)
		if err != nil {
			return err
		}
		operationDeadline, err := api.ParseTime(fixed.Deadline)
		if err != nil {
			return err
		}
		needsPreparation = actual.Status == "active" && actual.Control == "running" && actual.GoalRevision == fixed.GoalRevision && actual.ControlRevision == fixed.ControlRevision && now.Before(deadline) && now.Before(operationDeadline)
		return nil
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil || !needsPreparation {
		return ctx, err
	}
	// 此端口没有返回ctx的线合同，必须使用宿主在本次入口建立的共享载体。
	// 缺载体不能悄悄返回只供局部调用使用的证明，令后续纯门禁再次失联。
	entry, ok := ctx.Value(remoteAgentEntryKey{}).(remoteAgentEntry)
	if !ok || entry.scope != scope {
		return ctx, api.E("dependency_unavailable", "remote_parent_flow_required")
	}
	ctx, err = a.RemoteAgent.PrepareChildContext(ctx, original.TaskRef.ObjectID)
	if err != nil {
		return ctx, err
	}
	// 只沿这个profile已经登记的准确holder刷新派发用途，缺声明不能
	// 由准备器新造copy或放宽DataPolicy。后续Task原authorize仍逐源核验。
	return a.prepareForeignSources(ctx, scope, a.ServiceAuth, original.ProcessedSourceRefs, "task.dispatch", "cloud")
}
