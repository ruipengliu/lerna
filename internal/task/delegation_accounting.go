package task

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 预算关闭是原额度的负意图，不取消已成功的子目标，也不重新消费许可。
// 先取得原根/预算，随后关闭额度与唤醒原委派责任同一事务提交。
func (s *Service) prepareDelegationBudgetCloseTx(ctx context.Context, tx runtime.Tx, id string) (Delegation, Allocation, error) {
	_, d, err := delegationForTaskTx(ctx, tx, id)
	if err != nil {
		return d, Allocation{}, err
	}
	_, a, err := allocationForTaskTx(ctx, tx, d.AllocationRef.ObjectID)
	if err != nil {
		return d, a, err
	}
	if d.AllocationRef.TenantID != tx.Scope().TenantID || d.AllocationRef.OwnerID != tx.Scope().OwnerID || d.AllocationRef.Revision != 1 || d.AllocationRef.ObjectID != a.AllocationID || a.ParentTaskRef.ObjectID != d.ParentTaskRef.ObjectID || a.ReceiverID != d.ReceiverID {
		return d, a, api.E("idempotency_conflict", "original_delegation_allocation_changed")
	}
	if d.ClosureRef != nil || !d.CloseRequested && (!d.GoalWorkClosed || !d.EffectsClosed) {
		return d, a, nil
	}
	if a.State == "preparing" || a.State == "open" {
		if a.Revision >= api.MaxSafeInteger || d.Revision >= api.MaxSafeInteger {
			return d, a, api.E("overloaded", "original_closing_revision_exhausted")
		}
		a.State = "closing"
		a.Revision++
		if err = tx.Put(ctx, allocations, a.AllocationID, a.Revision-1, a); err != nil {
			return d, a, err
		}
		d.Phase = "reconciling"
		d.Revision++
		if err = tx.Put(ctx, delegations, d.DelegationID, d.Revision-1, d); err != nil {
			return d, a, err
		}
	}
	if err = queueJob(ctx, tx, JobDelegation, "delegation/"+d.DelegationID, tx.Scope().Ref(d.DelegationID, d.Revision)); err != nil {
		return d, a, err
	}
	return d, a, nil
}

// 旧版本可能已结束委派Job而保留了原预算Billing。沿原唯一分配映射
// 恢复同一个责任；独立allocation没有Delegation时仍等它自己的关闭证据。
func (s *Service) allocationBillingJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, reservation Reservation) error {
	return s.finish(ctx, store, scope, work, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
		t, current, err := reservationForTaskTx(ctx, tx, reservation.ReservationID)
		if err != nil {
			return err
		}
		if current.State == "settled" {
			return nil
		}
		if current.SourceKind != "budget_allocation" {
			return api.E("idempotency_conflict", "original_billing_source_changed")
		}
		_, a, err := allocationForTaskTx(ctx, tx, current.SourceRef.ObjectID)
		if err != nil {
			return err
		}
		if current.SourceRef.TenantID != scope.TenantID || current.SourceRef.OwnerID != a.ReceiverID || a.ParentTaskRef.ObjectID != t.Task.TaskID {
			return api.E("idempotency_conflict", "original_allocation_billing_changed")
		}
		rows, err := tx.List(ctx, delegations, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
		if err != nil {
			return err
		}
		if len(rows) > int(s.config.MaxRelations) {
			return api.E("overloaded", "delegation_billing_scan_limit")
		}
		var matched *Delegation
		for _, row := range rows {
			var d Delegation
			if err = row.Decode(&d); err != nil {
				return err
			}
			if d.AllocationRef.ObjectID != a.AllocationID {
				continue
			}
			if matched != nil || d.ParentTaskRef.ObjectID != t.Task.TaskID || d.AllocationRef.TenantID != scope.TenantID || d.AllocationRef.OwnerID != scope.OwnerID || d.AllocationRef.Revision != 1 || d.ReceiverID != a.ReceiverID {
				return api.E("idempotency_conflict", "original_delegation_billing_mapping_changed")
			}
			matched = &d
		}
		if matched == nil {
			return nil
		}
		_, _, err = s.prepareDelegationBudgetCloseTx(ctx, tx, matched.DelegationID)
		return err
	})
}
