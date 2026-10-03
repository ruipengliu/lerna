package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

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
