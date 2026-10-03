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
