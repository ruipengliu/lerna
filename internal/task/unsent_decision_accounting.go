package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 只有同一原Decision尚未发送且当前目标/控制已经关闭该Snapshot，才可证明
// 此预留从未成为模型消费。缺Brain记录、取消回执或网络错误本身均不构成此证明。
func closedUnsentDecisionTx(ctx context.Context, tx runtime.Tx, t taskState, d decisionState) (bool, error) {
	if d.Sent || d.Consumed {
		return false, nil
	}
	if terminal(t) || t.Task.Control != "running" || t.Task.GoalRevision != d.Snapshot.GoalRevision || t.Task.ControlRevision != d.Snapshot.ControlRevision {
		return true, nil
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return false, err
	}
	deadline, err := api.ParseTime(t.Task.Deadline)
	if err != nil {
		return false, err
	}
	return !now.Before(deadline), nil
}

// 调用方已按根到叶Task/预算→原Decision锁序取得当前门禁；这里再核不可变
// Reservation绑定与全部原单位。不制造UsageSnapshot、Brain账单或已消费提案。
func (s *Service) settleClosedUnsentDecisionTx(ctx context.Context, tx runtime.Tx, t *taskState, d decisionState, wakeBilling bool) error {
	closed, err := closedUnsentDecisionTx(ctx, tx, *t, d)
	if err != nil {
		return err
	}
	if !closed {
		return runtime.ErrConflict
	}
	var birth, current Reservation
	if err = tx.GetVersion(ctx, reservations, d.ReservationID, 1, &birth); err != nil {
		return err
	}
	if _, err = tx.Get(ctx, reservations, d.ReservationID, &current); err != nil {
		return err
	}
	source := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: d.Intent.BrainOwnerID, ObjectID: d.Intent.DecisionID, Revision: 1}
	if birth.ReservationID != d.ReservationID || birth.TaskID != t.Task.TaskID || birth.SourceKind != "brain_decision" || !api.Equal(birth.SourceRef, source) ||
		current.ReservationID != birth.ReservationID || current.TaskID != birth.TaskID || current.SourceKind != birth.SourceKind || !api.Equal(current.SourceRef, birth.SourceRef) ||
		current.BindingState != "bound" || birth.BindingState != "bound" || len(current.Units) != len(birth.Units) {
		return api.E("idempotency_conflict", "reservation_source_changed")
	}
	if current.State == "settled" {
		return nil
	}
	if current.State != "open" || current.AppliedUsageRevision != 0 || current.UsageDigest != "" {
		return api.E("accounting_unknown", "unsent_reservation_usage_present")
	}
	for i := range current.Units {
		unit, original := &current.Units[i], birth.Units[i]
		if unit.Unit != original.Unit || unit.OriginalReserved != original.OriginalReserved || unit.RemainingReserved != original.OriginalReserved || unit.AppliedCumulative != "0" {
			return api.E("accounting_unknown", "unsent_reservation_usage_present")
		}
		index := -1
		for j := range t.Task.Budget {
			if t.Task.Budget[j].Unit == unit.Unit {
				index = j
				break
			}
		}
		if index < 0 {
			return api.E("dependency_unavailable", "budget_binding_missing")
		}
		balance := &t.Task.Budget[index]
		balance.Reserved, err = api.SubDecimal(balance.Reserved, unit.RemainingReserved)
		if err != nil {
			return err
		}
		unit.RemainingReserved = "0"
	}
	current.State = "settled"
	current.Revision++
	if err = tx.Put(ctx, reservations, current.ReservationID, current.Revision-1, current); err != nil {
		return err
	}
	rows, err := tx.List(ctx, reservations, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
	if err != nil {
		return err
	}
	if uint64(len(rows)) > s.config.MaxRelations {
		return api.E("dependency_unavailable", "reservation_index_capacity")
	}
	t.Task.AccountingOpen = false
	for _, row := range rows {
		var reservation Reservation
		if err = row.Decode(&reservation); err != nil {
			return err
		}
		if reservation.State != "settled" {
			t.Task.AccountingOpen = true
		}
	}
	if err = s.saveTask(ctx, tx, t); err != nil {
		return err
	}
	if t.IncomingAllocationID != "" {
		if err = s.refreshIncomingTx(ctx, tx, *t); err != nil {
			return err
		}
	}
	if wakeBilling {
		return queueJob(ctx, tx, JobBilling, "billing/"+current.ReservationID, tx.Scope().Ref(current.ReservationID, current.Revision))
	}
	return nil
}

func (s *Service) finishClosedUnsentDecision(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, decisionID string) error {
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		t, d, err := s.closedUnsentDecisionForFinishTx(ctx, tx, decisionID)
		if err != nil {
			return err
		}
		closed, err := closedUnsentDecisionTx(ctx, tx, t, d)
		if err != nil {
			return err
		}
		if closed {
			if err = s.settleClosedUnsentDecisionTx(ctx, tx, &t, d, true); err != nil {
				return err
			}
		} else if d.Sent {
			return runtime.ErrConflict
		}
		if terminal(t) {
			return nil
		}
		return s.scheduleDeadlineTx(ctx, tx, t)
	})
}

// 原deadline关闭可能还要锁完整子树；先完成这段上游Task锁，再取得
// Decision/Reservation头锁，不能在账务对象锁之后追加子Task锁。
func (s *Service) closedUnsentDecisionForFinishTx(ctx context.Context, tx runtime.Tx, id string) (taskState, decisionState, error) {
	var birth decisionState
	if err := tx.GetVersion(ctx, decisions, id, 1, &birth); err != nil {
		return taskState{}, decisionState{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), birth.Intent.TaskRef); err != nil {
		return taskState{}, decisionState{}, err
	}
	if birth.Intent.TaskRef.OwnerID != tx.Scope().OwnerID || birth.Intent.DecisionID != id {
		return taskState{}, decisionState{}, api.E("forbidden", "decision_task_scope_mismatch")
	}
	t, err := getTask(ctx, tx, birth.Intent.TaskRef.ObjectID)
	if err != nil {
		return taskState{}, decisionState{}, err
	}
	if _, err = s.expireTx(ctx, tx, &t); err != nil {
		return taskState{}, decisionState{}, err
	}
	return decisionForTaskTx(ctx, tx, id)
}

// 旧DispatchDecision可能已完成而Billing仍等待不存在的模型账本。原Billing
// 责任沿同一Decision/Reservation重核后完成；不新建决策、使用或财务身份。
func (s *Service) finishClosedUnsentBilling(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, reservation Reservation) (bool, error) {
	closed := false
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		t, d, err := decisionForTaskTx(ctx, tx, reservation.SourceRef.ObjectID)
		if err != nil {
			return err
		}
		if d.ReservationID != reservation.ReservationID || t.Task.TaskID != reservation.TaskID {
			return api.E("idempotency_conflict", "reservation_source_changed")
		}
		closed, err = closedUnsentDecisionTx(ctx, tx, t, d)
		return err
	})
	if err != nil || !closed {
		return false, err
	}
	return true, s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		t, d, err := s.closedUnsentDecisionForFinishTx(ctx, tx, reservation.SourceRef.ObjectID)
		if err != nil {
			return err
		}
		if d.ReservationID != reservation.ReservationID || t.Task.TaskID != reservation.TaskID {
			return api.E("idempotency_conflict", "reservation_source_changed")
		}
		// 不唤醒当前正在Finish的Billing，避免原work_revision被本轮自身递增。
		return s.settleClosedUnsentDecisionTx(ctx, tx, &t, d, false)
	})
}
