package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原 completion 的新 Job 只沿原成果取得本次当前证明。Task 保留原
// pending identity、检查选择、完成裁决和最终 Guard；这里不复用上次 Job。
func (c contextCompiler) PrepareCompletionGate(ctx context.Context, scope runtime.Scope, auth runtime.Auth, t api.Task, in task.CompleteInput) (context.Context, error) {
	if scope != c.a.Scope || !auth.HasRole("service") || auth.Ref(scope.OwnerID) != c.a.ServiceAuth.Ref(scope.OwnerID) || in.TaskID != t.TaskID || in.ExpectedGoalRevision != t.GoalRevision || len(in.ArtifactRefs) == 0 || len(in.ArtifactRefs) > 100 {
		return ctx, api.E("forbidden", "original_completion_scope_required")
	}
	status, err := c.a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		current, err := c.a.Task.ReadTaskTx(ctx, tx, auth, t.TaskID)
		if err != nil {
			return err
		}
		if current.Revision != t.Revision || current.GoalRevision != in.ExpectedGoalRevision || current.ControlRevision != t.ControlRevision {
			return api.E("revision_conflict", "original_completion_task_changed")
		}
		if current.Status != "active" || current.Control != "running" {
			return api.E("invalid_state", "original_completion_task_not_running")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(current.Deadline)
		if err != nil || !now.Before(deadline) {
			return api.E("expired", "original_completion_task_expired")
		}
		return currentCredentialTx(ctx, tx, auth)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	ctx, err = c.a.prepareRemoteAgentCompletionParent(ctx, scope, auth, t, in)
	if err != nil {
		return ctx, err
	}
	return c.a.prepareForeignSources(ctx, scope, auth, in.ArtifactRefs, "task.complete", "cloud")
}

// 检查 Job 使用自己的 task.context 许可，不借模型或完成 Job 的证明。
// 本次源准备只沿原检查的准确 artifact/parameters/evidence；最终检查登记
// 仍由 Task/Governance 在原 Claim 的短事务中裁决。
func (a *App) prepareCompletionCheck(ctx context.Context, scope runtime.Scope, t api.Task, req task.CheckRequest, parameters api.ContentRef) (context.Context, error) {
	if scope != a.Scope || req.Input.TaskID != t.TaskID || req.Input.GoalRevision != t.GoalRevision || !api.ValidID(req.CheckID) || len(req.Input.EvidenceRefs) > 100 {
		return ctx, api.E("forbidden", "original_condition_check_scope_required")
	}
	status, err := a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		current, err := a.Task.ReadTaskTx(ctx, tx, a.ServiceAuth, t.TaskID)
		if err != nil {
			return err
		}
		if current.GoalRevision != t.GoalRevision || current.GoalRef != t.GoalRef || current.ControlRevision != t.ControlRevision || current.RequirementsDigest != t.RequirementsDigest {
			return api.E("revision_conflict", "original_condition_check_task_changed")
		}
		if current.Status != "active" || current.Control != "running" {
			return api.E("invalid_state", "original_condition_check_task_not_running")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(current.Deadline)
		if err != nil || !now.Before(deadline) {
			return api.E("expired", "original_condition_check_task_expired")
		}
		return currentCredentialTx(ctx, tx, a.ServiceAuth)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	refs := uniqueSources(append([]api.ContentRef{req.Input.ArtifactRef, parameters}, req.Input.EvidenceRefs...))
	return a.prepareForeignSources(ctx, scope, a.ServiceAuth, refs, "task.context", "cloud")
}
