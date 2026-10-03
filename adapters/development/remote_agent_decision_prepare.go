package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 只为原正向 Decision 派发取得本次父范围证明。Task 先核原 Claim、
// 根链、预算及提交主体，准备返回后仍由原 decisionJob 强核当前门禁。
// Brain 的来源准备另沿准确 Snapshot 和 brain.input 用途进行。
func (g remoteTaskGate) PrepareTaskDecision(ctx context.Context, scope runtime.Scope, auth runtime.Auth, actual api.Task, original api.DecisionDispatchIntent) (context.Context, error) {
	if g.a.RemoteAgent == nil {
		return ctx, nil
	}
	if scope != g.a.Scope || auth.TenantID != scope.TenantID || actual.TenantID != scope.TenantID || actual.OrchestratorID != scope.OwnerID || original.TaskRef.TenantID != scope.TenantID || original.TaskRef.OwnerID != scope.OwnerID || original.TaskRef.ObjectID != actual.TaskID || original.TaskRef.Revision > actual.Revision {
		return ctx, api.E("forbidden", "original_remote_decision_scope_required")
	}
	return g.a.prepareRemoteAgentDecisionParent(ctx, scope, auth, original.DecisionID)
}

var _ task.DecisionGatePreparer = remoteTaskGate{}
