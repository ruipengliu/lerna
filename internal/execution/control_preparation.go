package execution

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

func definitiveControlFailure(err error) bool {
	var business *api.Error
	if !errors.As(err, &business) {
		return false
	}
	switch business.Code {
	case "forbidden", "invalid_request", "invalid_state", "revision_conflict", "idempotency_conflict", "expired", "unsupported", "gone":
		return true
	}
	return false
}

// 先耐久固定本 Attempt 的纯准备结果，后取得原独立控制命令的准确窗口。
// 提交未知或外部失答保留 prepared，恢复不重新 Prepare，也不更换 Attempt。
func (s *Service) prepareAttemptControl(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, op operationRecord, attempt Attempt) (operationRecord, Attempt, error) {
	source, ok := s.cfg.Authority.(ControlWindowSource)
	if !ok {
		return op, attempt, api.E("dependency_unavailable", "original_control_preparation_unavailable")
	}
	if err := st.CheckClaim(ctx, sc, w.Claim); err != nil {
		return op, attempt, err
	}
	request := StartRequest{ControlWindow: attempt.ControlWindow, Invoke: op.Invoke, Intent: *op.Intent, AttemptID: attempt.AttemptID, Auth: op.Principal}
	window, err := source.PrepareControlWindow(ctx, sc, request)
	if err != nil {
		return op, attempt, err
	}
	if err = api.ValidateRecord("ControlSnapshot", window); err != nil {
		return op, attempt, err
	}
	if window.TaskID != op.Invoke.TaskRef.ObjectID || window.OrchestratorID != op.Invoke.TaskRef.OwnerID || window.GoalRevision != op.Invoke.GoalRevision || window.ControlRevision != op.Invoke.ControlRevision || window.Status != "active" || window.Control != "running" {
		return op, attempt, api.E("revision_conflict", "prepared_control_changed")
	}
	issued, err := api.ParseTime(window.IssuedAt)
	if err != nil {
		return op, attempt, err
	}
	before, err := api.ParseTime(window.StartBefore)
	if err != nil || !before.After(issued) || before.Sub(issued) > 5*time.Second {
		return op, attempt, api.E("invalid_request", "unbounded_prepared_control_window")
	}
	request.ControlWindow = window
	authority, err := s.cfg.Authority.PrepareStart(ctx, sc, request)
	if err != nil {
		return op, attempt, err
	}
	if authority.AuthorityRevision == 0 || authority.ProofRef.TenantID != sc.TenantID || api.ValidateRecord("ContentRef", authority.ProofRef) != nil {
		return op, attempt, api.E("forbidden", "start_proof_missing")
	}
	if err = st.CheckClaim(ctx, sc, w.Claim); err != nil {
		return op, attempt, err
	}
	var updated operationRecord
	var prepared Attempt
	status, err := st.Within(ctx, sc, s.participants(), func(tx rt.Tx) error {
		// 当前控制 gate 与窗口先于 Operation/Attempt；本阶段仍不授予物理开始。
		if _, err := s.saveControl(ctx, tx, op.Principal, op.Invoke.TaskRef, window); err != nil {
			return err
		}
		rev, err := tx.Get(ctx, Namespace+".operations", op.Operation.OperationID, &updated)
		if err != nil {
			return err
		}
		ar, err := tx.Get(ctx, Namespace+".attempts", attempt.AttemptID, &prepared)
		if err != nil {
			return err
		}
		if err = restorePreparedBytes(&prepared); err != nil {
			return err
		}
		if updated.NewAttemptsClosed || prepared.Phase != "prepared" {
			return api.E("invalid_state", "operation_permanently_closed")
		}
		if !api.Equal(updated.Invoke, op.Invoke) || !api.Equal(updated.Intent, op.Intent) || !api.Equal(updated.Principal, op.Principal) || !api.Equal(prepared.Prepared, attempt.Prepared) {
			return api.E("revision_conflict", "original_preparation_changed")
		}
		if prepared.PreparedAuthority.AuthorityRevision != 0 {
			if !api.Equal(prepared.ControlWindow, window) || !api.Equal(prepared.PreparedAuthority, authority) {
				return api.E("idempotency_conflict", "original_prepared_authority_changed")
			}
			return tx.Guard(ctx, w.Claim)
		}
		prepared.ControlWindow, prepared.ControlWindowID, prepared.PreparedAuthority = window, window.WindowID, authority
		prepared.Revision = ar + 1
		if err = tx.Put(ctx, Namespace+".attempts", prepared.AttemptID, ar, prepared); err != nil {
			return err
		}
		updated.Operation.Attempts.CollectionRevision++
		if err = putOperation(ctx, tx, &updated, rev); err != nil {
			return err
		}
		return tx.Guard(ctx, w.Claim)
	})
	if status == rt.CommitUnknown {
		return op, attempt, rt.ErrCommitUnknown
	}
	if err != nil {
		return op, attempt, err
	}
	return updated, prepared, nil
}
