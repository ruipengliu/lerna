package tasks

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/sessions"
)

func loadAdmissionByOperation(tx *durable.Tx, user, opID string) (*lernav1.AdmissionRecord, error) {
	var blob []byte
	err := tx.QueryRow(`SELECT record FROM admissions WHERE user_id = ? AND operation_id = ?`, user, opID).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &lernav1.AdmissionRecord{}
	return r, proto.Unmarshal(blob, r)
}

// handleStartSend 是出口 P4 的开始门禁（核心契约 2.6）：在裁决域里与取消、条件变更、输入变更、
// 开始核验和授权撤销按同一裁决顺序排先后，谁先持久成立谁决定。通过后返回持久的开始回执，
// 它只表示"允许开始"，不证明请求已经发出。
func (m *Module) handleStartSend(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.StartSendCommand)
	user := in.UserID()
	adm, err := loadAdmissionByOperation(tx, user, cmd.GetOperationId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if adm == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "operation %s was never admitted", cmd.GetOperationId())
	}
	// 开始-1：凭据、调用者、执行端点、调用描述与准入一致。
	if in.Identity().GetIssuerId() != adm.GetLedgerDomainId() || cmd.GetExecutorEndpointId() != adm.GetExecutorEndpointId() ||
		cmd.GetCapabilityId() != adm.GetCapabilityId() || cmd.GetTaskId() != adm.GetTaskId() ||
		!bytes.Equal(cmd.GetRequestDigest(), RequestDigest(adm.GetCapabilityId(), adm.GetParameters())) {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "start request does not match the admission of %s", cmd.GetOperationId())
	}
	t, err := loadTask(tx, user, adm.GetTaskId())
	if err != nil {
		return durable.Outcome{}, err
	}
	// 开始-2：首次开始比较准入时的控制代次；安全重发改为检查当前控制状态，取消之后不得再重发写请求。
	// 核对查询是收尾工作：取消之后仍可以核对。
	if cmd.GetPurpose() != lernav1.SendPurpose_SEND_PURPOSE_QUERY {
		if t.GetLifecycle() != lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
			return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "task %s is closed", t.GetTaskId())
		}
		if cmd.GetSafeResend() {
			if t.GetControl() != lernav1.TaskControl_TASK_CONTROL_ACTIVE {
				return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "task control is %s; no further write resend", t.GetControl())
			}
		} else if t.GetControlGeneration() != adm.GetControlGeneration() {
			code := lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION
			if t.GetInputVersion() != adm.GetInputVersion() {
				code = lernav1.ErrorCode_ERROR_CODE_STALE_INPUT
			}
			return durable.Outcome{}, errs.New(code, "control generation moved from %d to %d after admission",
				adm.GetControlGeneration(), t.GetControlGeneration())
		}
	}
	// 开始-3：有与该动作精确匹配的授权使用记录，授权链未撤销、未过期；不再要求剩余次数。
	if err := grants.EgressCheck(tx, user, cmd.GetCredentialRef(), cmd.GetOperationId(), adm.GetExecutorEndpointId(), cmd.GetRequestDigest()); err != nil {
		return durable.Outcome{}, err
	}
	// 开始-4：M1 没有记忆依赖，内容限制只有"当前任务"一种处理目的。
	// 开始-5：占用一次实际发送额度，与开始回执同事务绑定。
	if err := budget.OccupySend(tx, user, cmd.GetOperationId(), cmd.GetPurpose()); err != nil {
		return durable.Outcome{}, err
	}
	// 开始-6：M1 只有本机执行管理，启动状态总是"允许恢复"。
	if cmd.GetPurpose() == lernav1.SendPurpose_SEND_PURPOSE_EXECUTE {
		v, taskID, err := loadOperationView(tx, user, cmd.GetOperationId())
		if err != nil {
			return durable.Outcome{}, err
		}
		if v != nil && !v.GetStarted() {
			v.Started = true
			if err := saveOperationView(tx, user, taskID, v); err != nil {
				return durable.Outcome{}, err
			}
		}
	}
	return durable.Outcome{Result: &lernav1.StartSendResult{
		StartReceiptRef:   in.Identity().GetCommandId(),
		ControlGeneration: t.GetControlGeneration(),
	}}, nil
}

// handleOperationUpdate 接纳执行管理的变化通知：按执行管理修订接纳，重复或迟到的旧修订不改变视图，
// 也不重复推进任务。任务关闭后补来的事实仍然接纳，但不改写 Result。
func (m *Module) handleOperationUpdate(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	u := in.Payload.(*lernav1.OperationUpdate)
	user := in.UserID()
	v, taskID, err := loadOperationView(tx, user, u.GetOperationId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if v == nil || taskID != u.GetTaskId() {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown operation %s", u.GetOperationId())
	}
	if u.GetLedgerRevision() <= v.GetLedgerRevision() {
		return durable.Outcome{}, nil
	}
	v.LedgerRevision = u.GetLedgerRevision()
	v.LedgerAccepted = true
	v.Effect = u.GetEffect()
	v.LateEffect = u.GetLateEffect()
	v.Dispatch = u.GetDispatch()
	v.Settled = u.GetSettled()
	if u.GetStarted() {
		v.Started = true
	}
	v.Observed = u.GetObserved()
	v.EvidenceRefs = u.GetEvidenceRefs()
	v.ReconcileState = u.GetReconcileState()
	v.PauseReason = u.GetPauseReason()
	if err := saveOperationView(tx, user, taskID, v); err != nil {
		return durable.Outcome{}, err
	}
	if err := grants.RefreshRevocations(tx, user); err != nil {
		return durable.Outcome{}, err
	}
	t, err := loadTask(tx, user, taskID)
	if err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{}, m.onOperationProgress(tx, t, v, u)
}

// onOperationProgress 根据动作的新事实重新计算可推进范围。
func (m *Module) onOperationProgress(tx *durable.Tx, t *lernav1.Task, v *lernav1.OperationView, u *lernav1.OperationUpdate) error {
	if t.GetLifecycle() != lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
		return nil
	}
	if err := m.onCoreWorkUpdate(tx, t, v, u); err != nil {
		return err
	}
	if t.GetFrozenRound() != 0 {
		// 核验轮次进行中：动作的新事实交给核验工作重新判断。
		return tx.WakeJob(t.GetUserId(), verifyPurpose(t.GetTaskId(), t.GetFrozenRound()))
	}
	if !v.GetSettled() && v.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN {
		return nil
	}
	// 因阻断而等待的旧提议是在旧进展上做出的：作废，在新快照上重新请求提议。
	rows, err := tx.Query(`SELECT proposal_id FROM proposals WHERE user_id = ? AND task_id = ? AND status = ?`,
		t.GetUserId(), t.GetTaskId(), proposalReceived)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range stale {
		if err := setProposalStatus(tx, t.GetUserId(), id, proposalStale); err != nil {
			return err
		}
		if err := sessions.SupersedeForProposal(tx, t.GetUserId(), id); err != nil {
			return err
		}
		if err := tx.SealJob(t.GetUserId(), "adjudicate:"+id); err != nil {
			return err
		}
	}
	return m.maybeRequestProposal(tx, t)
}

// Digest 返回准入记录对应的请求摘要（供测试核对开始门禁）。
func (m *Module) Digest(adm *lernav1.AdmissionRecord) []byte {
	return RequestDigest(adm.GetCapabilityId(), adm.GetParameters())
}
