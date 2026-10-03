package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 独立Brain Job先读原Decision/Task元数据，再显式取得本次父证明。
// 不要求旧磁盘肯定证明，也不为终态、暂停或旧目标读取正文。
func (a *App) prepareRemoteAgentDecisionParent(ctx context.Context, scope runtime.Scope, auth runtime.Auth, decisionID string) (context.Context, error) {
	if a.RemoteAgent == nil {
		return ctx, nil
	}
	if scope != a.Scope {
		return ctx, api.E("forbidden", "remote_decision_scope_mismatch")
	}
	var taskID string
	var needsPreparation bool
	status, err := a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		// 原Snapshot只通过本宿主配置的受信元数据身份读取。Task端口
		// 传入的原提交者仍单独核Task访问和当前凭据，不继承service角色。
		snapshot, err := a.Task.DecisionSnapshotTx(ctx, tx, a.ServiceAuth, decisionID)
		if err != nil {
			return err
		}
		actual, err := a.Task.ReadTaskTx(ctx, tx, auth, snapshot.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		if err = currentCredentialTx(ctx, tx, auth); err != nil {
			return err
		}
		if err = currentCredentialTx(ctx, tx, a.ServiceAuth); err != nil {
			return err
		}
		incoming, found, err := a.Task.ReadIncomingSourceTx(ctx, tx, auth, actual.TaskID)
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
		needsPreparation = actual.Status == "active" && actual.Control == "running" && now.Before(deadline) && actual.GoalRevision == snapshot.GoalRevision && actual.GoalRef == snapshot.GoalRef && actual.ControlRevision == snapshot.ControlRevision && actual.PolicyRef == snapshot.PolicyRef
		taskID = actual.TaskID
		return nil
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil || !needsPreparation {
		return ctx, err
	}
	return a.RemoteAgent.PrepareChildContext(ctx, taskID)
}
