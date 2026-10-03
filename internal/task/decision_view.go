package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// DecisionSnapshotTx返回原准入时冻结的准确Snapshot，不授予当前发送资格。
// 宿主须另外调用CheckDecisionTx；该内部装配入口不读取Content或外部服务。
func (s *Service) DecisionSnapshotTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, decisionID string) (api.Snapshot, error) {
	d, err := decisionBirthTx(ctx, tx, auth, decisionID)
	return d.Snapshot, err
}

// DecisionCostBoundTx机械返回原Reservation的上界，不按当前费用重新定价。
func (s *Service) DecisionCostBoundTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, decisionID string) ([]api.Amount, error) {
	d, err := decisionBirthTx(ctx, tx, auth, decisionID)
	if err != nil {
		return nil, err
	}
	var reservation Reservation
	if err = tx.GetVersion(ctx, reservations, d.ReservationID, 1, &reservation); err != nil {
		return nil, err
	}
	if reservation.TaskID != d.Intent.TaskRef.ObjectID || reservation.SourceKind != "brain_decision" || reservation.SourceRef.TenantID != tx.Scope().TenantID || reservation.SourceRef.OwnerID != d.Intent.BrainOwnerID || reservation.SourceRef.ObjectID != decisionID || reservation.BindingState != "bound" {
		return nil, api.E("idempotency_conflict", "decision_reservation_source_changed")
	}
	bound := make([]api.Amount, len(reservation.Units))
	for i, unit := range reservation.Units {
		bound[i] = api.Amount{Unit: unit.Unit, Value: unit.OriginalReserved}
	}
	if len(bound) == 0 {
		return nil, api.E("dependency_unavailable", "decision_reservation_bound_missing")
	}
	if err = api.ValidateAmounts(bound); err != nil {
		return nil, err
	}
	return bound, nil
}

func decisionBirthTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, decisionID string) (decisionState, error) {
	if auth.TenantID != tx.Scope().TenantID || !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return decisionState{}, api.E("forbidden", "trusted_decision_view_required")
	}
	if !api.ValidID(decisionID) {
		return decisionState{}, invalid("invalid_decision_identity")
	}
	var d decisionState
	if err := tx.GetVersion(ctx, decisions, decisionID, 1, &d); err != nil {
		return decisionState{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), d.Intent.TaskRef); err != nil {
		return decisionState{}, err
	}
	if d.Intent.TaskRef.OwnerID != tx.Scope().OwnerID || d.Intent.DecisionID != decisionID || !api.Equal(d.Intent.TaskRef, d.Snapshot.TaskRef) || d.Intent.SnapshotRevision != d.Snapshot.Revision {
		return decisionState{}, api.E("idempotency_conflict", "decision_snapshot_source_changed")
	}
	return d, nil
}
