package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 完成工作的新入口只为原Task/原成果取得本次当前父证明。
// 元数据事务不调用当前父强门（此时证明尚未取得），也不读正文或出站；
// 原CompletionGatePreparer两侧Claim和CompleteTx最终强门继续负责准入。
func (a *App) prepareRemoteAgentCompletionParent(ctx context.Context, scope runtime.Scope, auth runtime.Auth, expected api.Task, in task.CompleteInput) (context.Context, error) {
	if a.RemoteAgent == nil {
		return ctx, nil
	}
	if scope != a.Scope || expected.OrchestratorID != scope.OwnerID || expected.TenantID != scope.TenantID || !auth.HasRole("service") || auth.Ref(scope.OwnerID) != a.ServiceAuth.Ref(scope.OwnerID) || in.TaskID != expected.TaskID || in.ExpectedGoalRevision != expected.GoalRevision {
		return ctx, api.E("forbidden", "remote_completion_scope_mismatch")
	}
	var needsPreparation bool
	status, err := a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		actual, err := a.Task.ReadTaskTx(ctx, tx, auth, expected.TaskID)
		if err != nil {
			return err
		}
		if actual.Revision != expected.Revision || actual.GoalRevision != expected.GoalRevision || actual.GoalRef != expected.GoalRef || actual.ControlRevision != expected.ControlRevision || actual.PolicyRef != expected.PolicyRef || actual.Deadline != expected.Deadline {
			return api.E("revision_conflict", "original_completion_task_changed")
		}
		if err = currentCredentialTx(ctx, tx, auth); err != nil {
			return err
		}
		incoming, found, err := a.Task.ReadIncomingSourceTx(ctx, tx, auth, actual.TaskID)
		if err != nil || !found || incoming.OwnerID == scope.OwnerID {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(actual.Deadline)
		if err != nil {
			return err
		}
		needsPreparation = actual.Status == "active" && actual.Control == "running" && now.Before(deadline)
		return nil
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil || !needsPreparation {
		return ctx, err
	}
	return a.RemoteAgent.PrepareChildContext(ctx, expected.TaskID)
}
