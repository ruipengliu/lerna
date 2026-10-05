package tasks

import (
	"strconv"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// AcceptInput 实现 sessions.TaskPort：任务编排在保存输入的同一事务中接纳修改或回答（任务编排 4.5）。
//
// 输入版本递增，基于旧上下文的提议随之作废。会改变任务依据的输入（任务修改；会改变依据的回答）
// 同时递增控制代次、作废旧提议、登记封闭尚未开始的目标动作、把进行中的核验轮次标为被替代；
// 条件集的 bound_input_version 留在原值，在这条输入被处理之前普通目标准入和成功关闭都被拒绝。
func (m *Module) AcceptInput(tx *durable.Tx, in sessions.TaskInput) error {
	t, err := loadTask(tx, in.User, in.TaskID)
	if err != nil {
		return err
	}
	if t == nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "task %s not found", in.TaskID)
	}
	if t.GetLifecycle() == lernav1.TaskLifecycle_TASK_LIFECYCLE_CLOSED {
		// 关闭之后不恢复、不修改条件、不重新推进原目标；需要时发起新任务。
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "task %s is closed; start a new task instead", in.TaskID)
	}
	changesBasis := true
	switch in.Kind {
	case lernav1.InputKind_INPUT_KIND_MODIFY_TASK:
		if in.ExpectedRequirementsVersion != t.GetRequirementsVersion() || in.ExpectedInputVersion != t.GetInputVersion() {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_INPUT,
				"the modification was made against requirements v%d / input v%d, current is v%d / v%d",
				in.ExpectedRequirementsVersion, in.ExpectedInputVersion, t.GetRequirementsVersion(), t.GetInputVersion())
		}
	case lernav1.InputKind_INPUT_KIND_ANSWER:
		changesBasis = in.Request.GetChangesBasis()
	default:
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "input kind %s is not a task input", in.Kind)
	}
	set, err := loadRequirementSet(tx, in.User, t.GetTaskId(), t.GetRequirementsVersion())
	if err != nil {
		return err
	}
	// 新的条件集只接受任务模板和显式条件列表；校验失败时整条输入作为确定的拒绝保存。
	var newReqs []*lernav1.Requirement
	var source lernav1.AcceptanceSource
	var templateID string
	if in.Requirements != nil {
		if newReqs, source, templateID, err = buildRequirements(in.Requirements); err != nil {
			return err
		}
	}
	t.InputVersion++
	var seq int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(task_input_seq), 0) + 1 FROM task_inputs WHERE user_id = ? AND task_id = ?`,
		in.User, t.GetTaskId()).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_inputs (user_id, task_id, task_input_seq, input_id, input_kind, input_version, changes_basis, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, in.User, t.GetTaskId(), seq, in.InputID, int32(in.Kind), t.GetInputVersion(),
		boolInt(changesBasis), tx.NowMs()); err != nil {
		return err
	}
	if changesBasis {
		t.ControlGeneration++
		if err := m.invalidatePlanning(tx, t, "input:"+strconv.FormatInt(t.GetInputVersion(), 10)); err != nil {
			return err
		}
	}
	switch {
	case len(newReqs) > 0:
		// 新的条件集按用户的显式列表或受信模板接纳，绑定到当前输入版本。
		t.RequirementsVersion = set.GetVersion() + 1
		if err := insertRequirementSet(tx, in.User, &lernav1.RequirementSet{
			TaskId: t.GetTaskId(), Version: t.GetRequirementsVersion(), Requirements: newReqs,
			Status: lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED, AcceptanceSource: source,
			BoundInputVersion: t.GetInputVersion(), TemplateId: templateID,
		}); err != nil {
			return err
		}
	case set.GetStatus() == lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED && (in.KeepRequirements || !changesBasis):
		// 可信的用户输入确认无需改条件，或回答不改变任务依据：bound_input_version 按顺序推进。
		if err := rebind(tx, in.User, set, t.GetInputVersion()); err != nil {
			return err
		}
	default:
		// 会改变依据的输入尚未处理：保存澄清请求，任务进入等待用户，不推进绑定版本。
		reqID := ids.New()
		if err := sessions.OpenRequest(tx, &lernav1.InputRequest{
			UserId: in.User, RequestId: reqID, SessionId: in.SessionID, TaskId: t.GetTaskId(), ChangesBasis: true,
			Question: "这条输入是否改变完成条件？请给出新的条件（任务模板或显式条件列表），或确认条件不变。",
		}); err != nil {
			return err
		}
		if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_USER); err != nil {
			return err
		}
		if err := saveTaskCore(tx, t); err != nil {
			return err
		}
		return setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_USER, Ref: reqID,
			Gap: "输入版本 " + strconv.FormatInt(t.GetInputVersion(), 10) + " 尚未处理：需要用户说明完成条件是否改变"})
	}
	if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_USER); err != nil {
		return err
	}
	if err := saveTaskCore(tx, t); err != nil {
		return err
	}
	return m.maybeRequestProposal(tx, t)
}

func rebind(tx *durable.Tx, user string, set *lernav1.RequirementSet, inputVersion int64) error {
	_, err := tx.Exec(`UPDATE requirement_sets SET bound_input_version = ? WHERE user_id = ? AND task_id = ? AND version = ?`,
		inputVersion, user, set.GetTaskId(), set.GetVersion())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// invalidatePlanning 在依据改变的事务中：作废旧提议和提议请求，把进行中的核验轮次标为被替代，
// 登记封闭尚未开始的目标动作。被封闭的动作不改回"未准入"，已消费的确认不退还。
func (m *Module) invalidatePlanning(tx *durable.Tx, t *lernav1.Task, cause string) error {
	user := t.GetUserId()
	if _, err := tx.Exec(`UPDATE proposal_requests SET status = ? WHERE user_id = ? AND task_id = ? AND status = ?`,
		requestSuperseded, user, t.GetTaskId(), requestPending); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT proposal_id FROM proposals WHERE user_id = ? AND task_id = ? AND status = ?`, user, t.GetTaskId(), proposalReceived)
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
		if err := setProposalStatus(tx, user, id, proposalStale); err != nil {
			return err
		}
		if err := sessions.SupersedeForProposal(tx, user, id); err != nil {
			return err
		}
		if err := tx.SealJob(user, "adjudicate:"+id); err != nil {
			return err
		}
	}
	if err := m.supersedeRound(tx, t, "basis changed by "+cause); err != nil {
		return err
	}
	return m.sealUnstarted(tx, t, cause)
}

// sealUnstarted 登记封闭本任务尚未开始的目标动作（准确清单）。递增的控制代次已经让它们过不了 P4；
// 封闭请求让执行管理出具内部封闭证明，使它们能够收尾。
func (m *Module) sealUnstarted(tx *durable.Tx, t *lernav1.Task, cause string) error {
	ops, err := loadOperationViews(tx, t.GetUserId(), t.GetTaskId())
	if err != nil {
		return err
	}
	var list []string
	for _, op := range ops {
		if !op.GetSettled() && !op.GetStarted() && !isCoreWork(op.GetOrigin()) {
			list = append(list, op.GetOperationId())
		}
	}
	if len(list) == 0 {
		return nil
	}
	return tx.EnqueueHandoff(durable.Handoff{
		ID:        "seal:" + t.GetTaskId() + ":" + cause,
		User:      t.GetUserId(),
		Target:    m.ledgerDomain(),
		Kind:      ports.CommandSealDispatch,
		Payload:   &lernav1.SealDispatchCommand{UserId: t.GetUserId(), TaskId: t.GetTaskId(), OperationIds: list, Reason: cause, ControlGeneration: t.GetControlGeneration()},
		IntentRef: "task:" + t.GetTaskId(),
	})
}
