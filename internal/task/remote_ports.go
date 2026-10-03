package task

import (
	"context"
	"reflect"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type TaskSnapshotView struct {
	Snapshot api.Snapshot               `json:"snapshot"`
	Intent   api.DecisionDispatchIntent `json:"intent"`
}

// LatestTaskSnapshotTx只投影当前Goal/Control/Policy对应的原准入Snapshot。
// 无准确当前版本时不合成新权限；扫描、错误和同修订冲突均明确有界。
func (s *Service) LatestTaskSnapshotTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID string) (TaskSnapshotView, bool, error) {
	t, err := getTask(ctx, tx, taskID)
	if err != nil {
		return TaskSnapshotView{}, false, err
	}
	if err = principal(auth, t); err != nil {
		return TaskSnapshotView{}, false, err
	}
	rows, err := tx.List(ctx, decisions, taskID, "", int(s.config.MaxRelations)+1)
	if err != nil {
		return TaskSnapshotView{}, false, err
	}
	if len(rows) > int(s.config.MaxRelations) {
		return TaskSnapshotView{}, false, api.E("overloaded", "task_snapshot_scan_limit")
	}
	var latest TaskSnapshotView
	found := false
	for _, row := range rows {
		var d decisionState
		if err = row.Decode(&d); err != nil {
			return TaskSnapshotView{}, false, err
		}
		if d.Intent.TaskRef.TenantID != tx.Scope().TenantID || d.Intent.TaskRef.OwnerID != tx.Scope().OwnerID || d.Intent.TaskRef.ObjectID != taskID || d.Intent.DecisionID != row.ID || d.Intent.TaskRef != d.Snapshot.TaskRef || d.Intent.SnapshotRevision != d.Snapshot.Revision {
			return TaskSnapshotView{}, false, api.E("idempotency_conflict", "original_snapshot_scope_changed")
		}
		snap := d.Snapshot
		if snap.GoalRevision != t.Task.GoalRevision || snap.GoalRef != t.Task.GoalRef || snap.ControlRevision != t.Task.ControlRevision || snap.PolicyRef != t.Task.PolicyRef || snap.TaskRef.Revision > t.Task.Revision {
			continue
		}
		candidate := TaskSnapshotView{Snapshot: snap, Intent: d.Intent}
		if !found || candidate.Snapshot.TaskRef.Revision > latest.Snapshot.TaskRef.Revision {
			latest, found = candidate, true
		} else if candidate.Snapshot.TaskRef.Revision == latest.Snapshot.TaskRef.Revision && !reflect.DeepEqual(candidate, latest) {
			return TaskSnapshotView{}, false, api.E("idempotency_conflict", "current_task_snapshot_ambiguous")
		}
	}
	return latest, found, nil
}

// WithJobIntents 保持原Task事务模板，将已声明同库的Job意图留到领域对象之后。
// 接收端可以在同一闭包保存IncomingAllocation、原子Task映射和原命令决定。
func WithJobIntents(ctx context.Context, tx runtime.Tx, fn func(runtime.Tx) error) error {
	return withTaskJobs(ctx, tx, fn)
}

type DelegationScope struct {
	Delegation    Delegation           `json:"delegation"`
	Allocation    Allocation           `json:"allocation"`
	SubjectRef    api.ObjectRef        `json:"subject_ref"`
	ParentTask    api.Task             `json:"parent_task"`
	ParentSources []api.SourceEvidence `json:"parent_sources"`
}

// ReadIncomingAllocationTx只投影原接收额度及已观察累计费用，不授新开始权。
// 复用version2/1路由，已绑定Child先持原完整Task根/预算，关闭先到可无Child。
func (s *Service) ReadIncomingAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef) (IncomingAllocation, error) {
	if auth.TenantID != tx.Scope().TenantID || ref.TenantID != tx.Scope().TenantID || api.ValidateRecord("ObjectRef", ref) != nil || !auth.HasRole("service") && auth.SubjectID != ref.OwnerID {
		return IncomingAllocation{}, api.E("forbidden", "parent_identity_required")
	}
	current, err := incomingForTaskTx(ctx, tx, incomingID(ref))
	if err != nil {
		return current, err
	}
	if current.AllocationID != ref.ObjectID || current.ParentOwner != ref.OwnerID || current.ReceiverID != tx.Scope().OwnerID || current.ParentTaskRef.TenantID != tx.Scope().TenantID || current.ParentTaskRef.OwnerID != ref.OwnerID || current.TaskRef != nil && (current.TaskRef.TenantID != tx.Scope().TenantID || current.TaskRef.OwnerID != tx.Scope().OwnerID || api.ValidateRecord("ObjectRef", *current.TaskRef) != nil) {
		return IncomingAllocation{}, api.E("forbidden", "original_incoming_scope_changed")
	}
	return current, nil
}

// ReadIncomingSourceTx 只投影已登记的原allocation负责方，不授开始权。
func (s *Service) ReadIncomingSourceTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID string) (api.ObjectRef, bool, error) {
	t, err := getTask(ctx, tx, taskID)
	if err != nil {
		return api.ObjectRef{}, false, err
	}
	if err = principal(auth, t); err != nil {
		return api.ObjectRef{}, false, err
	}
	if t.IncomingAllocationID == "" {
		return api.ObjectRef{}, false, nil
	}
	var original IncomingAllocation
	if _, err = tx.Get(ctx, incoming, t.IncomingAllocationID, &original); err != nil {
		return api.ObjectRef{}, false, err
	}
	ref := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: original.ParentOwner, ObjectID: original.AllocationID, Revision: 1}
	if api.ValidateRecord("ObjectRef", ref) != nil || original.ReceiverID != tx.Scope().OwnerID || original.TaskRef == nil || original.TaskRef.ObjectID != taskID || original.TaskRef.OwnerID != tx.Scope().OwnerID || original.ParentTaskRef.OwnerID != original.ParentOwner {
		return api.ObjectRef{}, false, api.E("idempotency_conflict", "original_incoming_scope_changed")
	}
	return ref, true, nil
}

// CheckTaskCurrentTx 保持原根、预算、主体与 Incoming 门禁；不读取字节或外部证明。
func (s *Service) CheckTaskCurrentTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string, requireRunning bool) error {
	t, err := getTask(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = principal(auth, t); err != nil {
		return err
	}
	return s.CheckCurrent(ctx, tx, t, requireRunning)
}

// ReadTaskTx 只投影当前 Task；调用方仍须另核本次接纳、当前主体及来源门禁。
func (s *Service) ReadTaskTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (api.Task, error) {
	t, err := getTask(ctx, tx, id)
	if err != nil {
		return api.Task{}, err
	}
	if err = principal(auth, t); err != nil {
		return api.Task{}, err
	}
	return t.Task, nil
}

// ReadTaskTreeTx 在 adapter 读取自己的控制门禁前锁定有界本域子树。
// 它仍只读事实，不代替当前开始许可，也不执行外部控制。
func (s *Service) ReadTaskTreeTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (api.Task, error) {
	t, err := getTask(ctx, tx, id)
	if err != nil {
		return api.Task{}, err
	}
	if err = principal(auth, t); err != nil {
		return api.Task{}, err
	}
	if err = s.lockTaskTree(ctx, tx, id); err != nil {
		return api.Task{}, err
	}
	return t.Task, nil
}

// ReadAllocationTx 保持原根 Task/预算锁序，只返回准确账务事实。
// 读取原关闭或终态责任不重新授予开始许可。
func (s *Service) ReadAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (Allocation, error) {
	t, a, err := allocationForTaskTx(ctx, tx, id)
	if err != nil {
		return Allocation{}, err
	}
	if err = principal(auth, t); err != nil {
		return Allocation{}, err
	}
	return a, nil
}

// CheckDelegationScopeTx 投影已经核验的原提交主体，不从工作者 Auth 推断模型或子 Agent 权限。
func (s *Service) CheckDelegationScopeTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (DelegationScope, error) {
	d, a, gateErr := s.CheckDelegationTx(ctx, tx, auth, id)
	if gateErr != nil && d.DelegationID == "" {
		return DelegationScope{}, gateErr
	}
	t, err := getTask(ctx, tx, d.ParentTaskRef.ObjectID)
	if err != nil {
		return DelegationScope{}, err
	}
	return DelegationScope{Delegation: d, Allocation: a, SubjectRef: submitterAuth(tx.Scope(), t).Ref(tx.Scope().OwnerID), ParentTask: t.Task, ParentSources: append([]api.SourceEvidence{}, t.SourceRefs...)}, gateErr
}

// CheckDelegationTx 只核原负责方当前门禁，不执行传输或读取Content字节。
// 开始新使用时仍按原Task提交者检查许可，不从工作者角色推断原主体获权。
func (s *Service) CheckDelegationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (Delegation, Allocation, error) {
	var birth Delegation
	if err := tx.GetVersion(ctx, delegations, id, 1, &birth); err != nil {
		return Delegation{}, Allocation{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), birth.ParentTaskRef); err != nil {
		return Delegation{}, Allocation{}, err
	}
	if birth.ParentTaskRef.OwnerID != tx.Scope().OwnerID || birth.DelegationID != id || birth.AllocationRef.OwnerID != tx.Scope().OwnerID {
		return Delegation{}, Allocation{}, api.E("forbidden", "delegation_parent_scope_mismatch")
	}
	t, err := getTask(ctx, tx, birth.ParentTaskRef.ObjectID)
	if err != nil {
		return Delegation{}, Allocation{}, err
	}
	var a Allocation
	if _, err = tx.Get(ctx, allocations, birth.AllocationRef.ObjectID, &a); err != nil {
		return Delegation{}, Allocation{}, err
	}
	var d Delegation
	if _, err = tx.Get(ctx, delegations, id, &d); err != nil {
		return d, a, err
	}
	if !api.Equal(d.DelegateInput, birth.DelegateInput) || d.CreationKey != birth.CreationKey || !api.Equal(d.AllocationRef, birth.AllocationRef) || !api.Equal(d.CommandRef, birth.CommandRef) || !api.Equal(d.AncestorTaskRefs, birth.AncestorTaskRefs) {
		return d, a, api.E("idempotency_conflict", "delegation_source_changed")
	}
	if err = principal(auth, t); err != nil {
		return d, a, err
	}
	if err = s.CheckCurrent(ctx, tx, t, true); err != nil {
		return d, a, err
	}
	if d.CloseRequested || d.GoalWorkClosed || d.ParentGoalRevision != t.Task.GoalRevision || a.AllocationID != d.AllocationRef.ObjectID || a.ParentTaskRef.ObjectID != t.Task.TaskID || a.ParentTaskRef.OwnerID != tx.Scope().OwnerID || a.ReceiverID != d.ReceiverID || !api.Equal(a.Limits, d.Budget) || a.Deadline != d.Deadline || a.State != "preparing" && a.State != "open" {
		return d, a, api.E("invalid_state", "delegation_scope_closed")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return d, a, err
	}
	deadline, err := api.ParseTime(a.Deadline)
	if err != nil {
		return d, a, err
	}
	if !now.Before(deadline) {
		return d, a, api.E("expired", "delegation_deadline_exceeded")
	}
	actor := submitterAuth(tx.Scope(), t)
	if err = s.authorize(ctx, tx, actor, "task.delegate", append([]api.ContentRef{d.GoalRef}, d.InputRefs...), append([]api.ObjectRef{d.AgentBindingRef}, d.PermissionRefs...)); err != nil {
		return d, a, err
	}
	return d, a, nil
}
