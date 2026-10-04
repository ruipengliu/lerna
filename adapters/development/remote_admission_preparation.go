package development

import (
	"context"
	"fmt"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原意图和具体驱动只声明实际消费的用途；来源闭包仍由 Memory 当前门禁核验。
func remotePermissionRoots(i task.OperationIntent, fixed encodedIntent, filePermissions []remoteSourcePermission) []remoteSourcePermission {
	queue := []remoteSourcePermission{{fixed.Ref, "execution_intent"}, {i.ArgumentsRef, "execution_arguments"}}
	for _, ref := range uniqueSources(append(append([]api.ContentRef{}, i.ProcessedSourceRefs...), i.DisclosedSourceRefs...)) {
		queue = append(queue, remoteSourcePermission{ref, "execution_arguments"})
	}
	return append(queue, filePermissions...)
}

// 初次准入先核原 Task、主体和租约，再在 Tx 外按原用途取得双主体的当前来源
// 证明。后续原准入事务仍逐项强核，不借磁盘副本、新用途或旧 bundle 授权。
func (e executionBridge) prepareRemoteAdmissionSources(ctx context.Context, s runtime.Scope, i task.OperationIntent, fixed encodedIntent, filePermissions []remoteSourcePermission) (context.Context, []task.CurrentMaterialPreparation, error) {
	flow, ok := ctx.Value(foreignFlowKey{}).(runtime.Flow)
	if !ok || flow.Kind != "job" || flow.Scope != s || flow.Work == nil || flow.Work.Job.Kind != task.JobDispatchOperation || flow.Work.Job.SourceRef.ObjectID != i.OperationID {
		return ctx, nil, api.E("forbidden", "original_remote_worker_claim_required")
	}
	if len(i.UseIntentRefs) != 1 {
		return ctx, nil, api.E("forbidden", "original_remote_lease_missing")
	}
	parts := []string{"task", "memory", "content", "governance", "platform"}
	if e.a.RemoteAgent != nil {
		parts = append(parts, "collaboration")
	}
	var user runtime.Auth
	var materialGroups []remoteCurrentGroup
	var materialPlan []task.CurrentMaterialPreparation
	status, err := e.a.Store.Within(ctx, s, parts, func(tx runtime.Tx) error {
		original, err := e.a.Task.OperationIntentTx(ctx, tx, i.OperationID)
		if err != nil {
			return err
		}
		if !api.Equal(original, i) || fixed.AdmissionHash != i.IntentHash {
			return api.E("idempotency_conflict", "original_remote_intent_changed")
		}
		if err = e.a.Task.CheckTaskCurrentTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID, true); err != nil {
			return err
		}
		materialGroups, materialPlan, err = e.a.remoteTaskMaterialGroupsTx(ctx, tx, i.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		current, err := e.a.Task.ReadTaskTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		if current.GoalRevision != i.GoalRevision || current.ControlRevision != i.ControlRevision {
			return api.E("revision_conflict", "original_task_control_changed")
		}
		if err = currentCredentialTx(ctx, tx, e.a.ServiceAuth); err != nil {
			return err
		}
		user, err = e.a.remoteOriginalSubmitterTx(ctx, tx, i.TaskRef)
		if err != nil {
			return err
		}
		if _, err = e.a.Governance.CheckUseHeadsTx(ctx, tx, e.a.ServiceAuth, i.UseIntentRefs[0], remoteTarget(s, i.ExecutorID, i.OperationID), i.IntentHash); err != nil {
			return err
		}
		return tx.Guard(ctx, flow.Work.Claim)
	})
	if status == runtime.CommitUnknown {
		return ctx, nil, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, nil, err
	}
	if status != runtime.Committed {
		return ctx, nil, api.E("dependency_unavailable", "original_remote_prepare_not_committed")
	}
	groups := map[string][]api.ContentRef{}
	roots := remotePermissionRoots(i, fixed, filePermissions)
	if len(roots) > 256 {
		return ctx, nil, api.E("overloaded", "remote_source_closure_limit")
	}
	for _, root := range roots {
		groups[root.purpose] = append(groups[root.purpose], root.ref)
	}
	purposes := make([]string, 0, len(groups))
	for purpose := range groups {
		purposes = append(purposes, purpose)
	}
	sort.Strings(purposes)
	for _, purpose := range purposes {
		refs := uniqueSources(groups[purpose])
		for _, auth := range []runtime.Auth{e.a.ServiceAuth, user} {
			ctx, err = e.a.prepareForeignSources(ctx, s, auth, refs, purpose, "device")
			if err != nil {
				return ctx, nil, fmt.Errorf("prepare original remote sources purpose=%s location=device subject=%s: %w", purpose, auth.SubjectID, err)
			}
		}
	}
	if err = e.a.Store.CheckClaim(ctx, s, flow.Work.Claim); err != nil {
		return ctx, nil, err
	}
	// 原登记和字节/holder 准备已完成；准入只使用空消费载体内
	// 最后取得的 Current 证明。
	consumers := make([]remoteCurrentGroup, 0, 2*len(purposes))
	for _, purpose := range purposes {
		for _, auth := range []runtime.Auth{e.a.ServiceAuth, user} {
			consumers = append(consumers, remoteCurrentGroup{auth, uniqueSources(groups[purpose]), purpose, "device"})
		}
	}
	consumers = append(consumers, materialGroups...)
	ctx, err = e.a.refreshRemoteConsumerSources(ctx, s, consumers)
	return ctx, materialPlan, err
}
