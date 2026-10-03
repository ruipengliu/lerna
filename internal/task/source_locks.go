package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func decisionForTaskTx(ctx context.Context, tx runtime.Tx, id string) (taskState, decisionState, error) {
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
	var current decisionState
	if _, err = tx.Get(ctx, decisions, id, &current); err != nil {
		return taskState{}, decisionState{}, err
	}
	if !api.Equal(current.Intent, birth.Intent) || !api.Equal(current.Snapshot, birth.Snapshot) || current.ReservationID != birth.ReservationID {
		return taskState{}, decisionState{}, api.E("idempotency_conflict", "decision_source_changed")
	}
	return t, current, nil
}

func reservationForTaskTx(ctx context.Context, tx runtime.Tx, id string) (taskState, Reservation, error) {
	var birth Reservation
	if err := tx.GetVersion(ctx, reservations, id, 1, &birth); err != nil {
		return taskState{}, Reservation{}, err
	}
	t, err := getTask(ctx, tx, birth.TaskID)
	if err != nil {
		return taskState{}, Reservation{}, err
	}
	var current Reservation
	if _, err = tx.Get(ctx, reservations, id, &current); err != nil {
		return taskState{}, Reservation{}, err
	}
	if current.ReservationID != id || current.TaskID != birth.TaskID || current.SourceKind != birth.SourceKind || !api.Equal(current.SourceRef, birth.SourceRef) {
		return taskState{}, Reservation{}, api.E("idempotency_conflict", "reservation_source_changed")
	}
	return t, current, nil
}

func delegationForTaskTx(ctx context.Context, tx runtime.Tx, id string) (taskState, Delegation, error) {
	var birth Delegation
	if err := tx.GetVersion(ctx, delegations, id, 1, &birth); err != nil {
		return taskState{}, Delegation{}, err
	}
	t, err := getTask(ctx, tx, birth.ParentTaskRef.ObjectID)
	if err != nil {
		return taskState{}, Delegation{}, err
	}
	var current Delegation
	if _, err = tx.Get(ctx, delegations, id, &current); err != nil {
		return taskState{}, Delegation{}, err
	}
	if !api.Equal(current.DelegateInput, birth.DelegateInput) {
		return taskState{}, Delegation{}, api.E("idempotency_conflict", "delegation_source_changed")
	}
	return t, current, nil
}

func allocationForTaskTx(ctx context.Context, tx runtime.Tx, id string) (taskState, Allocation, error) {
	var birth Allocation
	if err := tx.GetVersion(ctx, allocations, id, 1, &birth); err != nil {
		return taskState{}, Allocation{}, err
	}
	t, err := getTask(ctx, tx, birth.ParentTaskRef.ObjectID)
	if err != nil {
		return taskState{}, Allocation{}, err
	}
	var current Allocation
	if _, err = tx.Get(ctx, allocations, id, &current); err != nil {
		return taskState{}, Allocation{}, err
	}
	if !api.Equal(current.ParentTaskRef, birth.ParentTaskRef) || current.ReceiverID != birth.ReceiverID || !api.Equal(current.Limits, birth.Limits) {
		return taskState{}, Allocation{}, api.E("idempotency_conflict", "allocation_source_changed")
	}
	return t, current, nil
}

func incomingForTaskTx(ctx context.Context, tx runtime.Tx, id string) (IncomingAllocation, error) {
	// 内部额度在version2绑定原Child；关闭先到的永久门禁可以没有Child。
	var routing IncomingAllocation
	err := tx.GetVersion(ctx, incoming, id, 2, &routing)
	if confirmedNotFound(err) {
		err = tx.GetVersion(ctx, incoming, id, 1, &routing)
	}
	if err != nil {
		return IncomingAllocation{}, err
	}
	if routing.TaskRef != nil {
		if err = runtime.CheckRef(tx.Scope(), *routing.TaskRef); err != nil {
			return IncomingAllocation{}, err
		}
		if routing.TaskRef.OwnerID != tx.Scope().OwnerID {
			return IncomingAllocation{}, api.E("forbidden", "incoming_task_scope_mismatch")
		}
		if _, err = getTask(ctx, tx, routing.TaskRef.ObjectID); err != nil {
			return IncomingAllocation{}, err
		}
	}
	var current IncomingAllocation
	if _, err = tx.Get(ctx, incoming, id, &current); err != nil {
		return IncomingAllocation{}, err
	}
	if !api.Equal(current.TaskRef, routing.TaskRef) || !api.Equal(current.ParentTaskRef, routing.ParentTaskRef) {
		return IncomingAllocation{}, api.E("revision_conflict", "incoming_task_source_changed")
	}
	return current, nil
}
