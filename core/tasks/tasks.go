// Package tasks 实现任务编排（docs/architecture/core/tasks/README.md）：
// 任务、条件版本、控制、准入决定和 Result。属于裁决域。
package tasks

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// 任务编排的工作类型。
const (
	JobCreateTask = "tasks.create_task"
)

// Module 是任务编排模块，绑定裁决域。
type Module struct {
	Domain *durable.Domain
}

// Register 登记任务编排的工作。
func (m *Module) Register() {
	m.Domain.HandleJob(JobCreateTask, m.createTask)
}

// NewGoal 实现 sessions.TaskPort：在保存输入的同一事务中校验完成条件，登记建立任务的工作。
// 模板或条件列表无效时，整条输入作为确定的拒绝保存。
func (m *Module) NewGoal(tx *durable.Tx, in sessions.GoalInput) error {
	if _, _, _, err := buildRequirements(in.Requirements); err != nil {
		return err
	}
	_, err := tx.EnqueueJob(durable.JobSpec{
		Kind:       JobCreateTask,
		User:       in.User,
		Subject:    in.InputID,
		PurposeKey: "create_task:" + in.InputID,
		Spec:       []byte(in.SessionID),
	})
	return err
}

// createTask 是持久点 2：保存任务、条件和快照依据。同一目标输入只建立一个任务。
func (m *Module) createTask(ctx context.Context, c *durable.Claim) error {
	return m.Domain.Advance(ctx, c, "tasks:create_task", func(tx *durable.Tx) (durable.Transition, error) {
		inputID := c.Subject
		sessionID := string(c.Spec)
		var existing string
		err := tx.QueryRow(`SELECT task_id FROM tasks WHERE user_id = ? AND goal_input_id = ?`, c.User, inputID).Scan(&existing)
		if err == nil {
			return durable.Done(), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return durable.Transition{}, err
		}
		var reqBlob []byte
		if err := tx.QueryRow(`SELECT requirements FROM session_inputs WHERE user_id = ? AND input_id = ?`, c.User, inputID).Scan(&reqBlob); err != nil {
			return durable.Transition{}, err
		}
		draft := &lernav1.RequirementSetDraft{}
		if len(reqBlob) > 0 {
			if err := proto.Unmarshal(reqBlob, draft); err != nil {
				return durable.Transition{}, err
			}
		}
		reqs, source, templateID, err := buildRequirements(draft)
		if err != nil {
			return durable.Block(err.Error()), nil
		}
		taskID := ids.New()
		t := &lernav1.Task{
			UserId:              c.User,
			TaskId:              taskID,
			OwnerDomainId:       tx.DomainID(),
			SessionId:           sessionID,
			GoalRef:             "session_input/" + inputID,
			RequirementsVersion: 1,
			InputVersion:        1,
			Lifecycle:           lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN,
			Control:             lernav1.TaskControl_TASK_CONTROL_ACTIVE,
			Progress:            lernav1.TaskProgress_TASK_PROGRESS_RUNNING,
			PlanningGeneration:  0,
			ControlGeneration:   1,
			Revision:            1,
		}
		set := &lernav1.RequirementSet{
			TaskId:       taskID,
			Version:      1,
			Requirements: reqs,
			TemplateId:   templateID,
		}
		if len(reqs) > 0 {
			set.Status = lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED
			set.AcceptanceSource = source
			set.BoundInputVersion = 1
		} else {
			// 模板外的目标：条件集为 DRAFT，任务等待用户澄清，不用空条件集判定成功。
			set.Status = lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_DRAFT
			reqID := ids.New()
			t.Progress = lernav1.TaskProgress_TASK_PROGRESS_WAITING
			t.WaitingOn = []*lernav1.WaitingOn{{
				Kind: lernav1.WaitingKind_WAITING_KIND_USER,
				Ref:  reqID,
				Gap:  "目标超出任务模板，需要用户给出完成条件（任务模板或显式条件列表）",
			}}
			if err := sessions.OpenRequest(tx, &lernav1.InputRequest{
				UserId:       c.User,
				RequestId:    reqID,
				SessionId:    sessionID,
				TaskId:       taskID,
				Question:     "这个目标做到什么算完成？请给出任务模板或显式条件列表。",
				ChangesBasis: true,
			}); err != nil {
				return durable.Transition{}, err
			}
		}
		if err := insertTask(tx, t, inputID); err != nil {
			return durable.Transition{}, err
		}
		if err := insertRequirementSet(tx, c.User, set); err != nil {
			return durable.Transition{}, err
		}
		if err := sessions.MarkInputAccepted(tx, c.User, inputID, taskID); err != nil {
			return durable.Transition{}, err
		}
		if err := sessions.LinkTask(tx, c.User, sessionID, taskID); err != nil {
			return durable.Transition{}, err
		}
		return durable.Done(), nil
	})
}

func insertTask(tx *durable.Tx, t *lernav1.Task, goalInputID string) error {
	waiting, err := marshalWaiting(t.GetWaitingOn())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO tasks (user_id, task_id, owner_domain_id, session_id, goal_input_id, goal_ref,
		requirements_version, input_version, lifecycle, control, progress, waiting_on, planning_generation,
		control_generation, revision, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.GetUserId(), t.GetTaskId(), t.GetOwnerDomainId(), t.GetSessionId(), goalInputID, t.GetGoalRef(),
		t.GetRequirementsVersion(), t.GetInputVersion(), int32(t.GetLifecycle()), int32(t.GetControl()),
		int32(t.GetProgress()), waiting, t.GetPlanningGeneration(), t.GetControlGeneration(), t.GetRevision(), tx.NowMs())
	return err
}

func marshalWaiting(w []*lernav1.WaitingOn) ([]byte, error) {
	return proto.Marshal(&lernav1.TaskView{Task: &lernav1.Task{WaitingOn: w}})
}

func unmarshalWaiting(b []byte) ([]*lernav1.WaitingOn, error) {
	if len(b) == 0 {
		return nil, nil
	}
	v := &lernav1.TaskView{}
	if err := proto.Unmarshal(b, v); err != nil {
		return nil, err
	}
	return v.GetTask().GetWaitingOn(), nil
}

func insertRequirementSet(tx *durable.Tx, user string, set *lernav1.RequirementSet) error {
	blob, err := proto.Marshal(set)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO requirement_sets (user_id, task_id, version, status, bound_input_version, record, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user, set.GetTaskId(), set.GetVersion(), int32(set.GetStatus()),
		set.GetBoundInputVersion(), blob, tx.NowMs())
	return err
}

// loadTask 读取任务；不存在时返回 nil。查询都带用户范围，不得只凭任务标识跨用户访问。
func loadTask(tx *durable.Tx, user, taskID string) (*lernav1.Task, error) {
	t := &lernav1.Task{UserId: user, TaskId: taskID}
	var lifecycle, control, progress int32
	var waiting []byte
	var created int64
	err := tx.QueryRow(`SELECT owner_domain_id, session_id, goal_ref, requirements_version, input_version, lifecycle,
		control, progress, waiting_on, planning_generation, control_generation, revision, result_ref, created_at
		FROM tasks WHERE user_id = ? AND task_id = ?`, user, taskID).Scan(
		&t.OwnerDomainId, &t.SessionId, &t.GoalRef, &t.RequirementsVersion, &t.InputVersion, &lifecycle, &control,
		&progress, &waiting, &t.PlanningGeneration, &t.ControlGeneration, &t.Revision, &t.ResultRef, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Lifecycle = lernav1.TaskLifecycle(lifecycle)
	t.Control = lernav1.TaskControl(control)
	t.Progress = lernav1.TaskProgress(progress)
	t.CreatedAt = timestamppb.New(time.UnixMilli(created))
	if t.WaitingOn, err = unmarshalWaiting(waiting); err != nil {
		return nil, err
	}
	return t, nil
}

func loadRequirementSet(tx *durable.Tx, user, taskID string, version int64) (*lernav1.RequirementSet, error) {
	var blob []byte
	var status int32
	var bound int64
	err := tx.QueryRow(`SELECT record, status, bound_input_version FROM requirement_sets WHERE user_id = ? AND task_id = ? AND version = ?`,
		user, taskID, version).Scan(&blob, &status, &bound)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set := &lernav1.RequirementSet{}
	if err := proto.Unmarshal(blob, set); err != nil {
		return nil, err
	}
	set.Status = lernav1.RequirementSetStatus(status)
	set.BoundInputVersion = bound
	return set, nil
}

// Phase 由生命周期、控制、推进三个维度组合得出界面阶段（任务编排 2.4），不单独存储。
func Phase(t *lernav1.Task, r *lernav1.Result) string {
	if t.GetLifecycle() == lernav1.TaskLifecycle_TASK_LIFECYCLE_CLOSED {
		switch r.GetOutcome() {
		case lernav1.TaskOutcome_TASK_OUTCOME_SUCCEEDED:
			return "SUCCEEDED"
		case lernav1.TaskOutcome_TASK_OUTCOME_CANCELLED:
			return "CANCELLED"
		default:
			return "FAILED"
		}
	}
	switch t.GetControl() {
	case lernav1.TaskControl_TASK_CONTROL_PAUSED:
		return "PAUSED"
	case lernav1.TaskControl_TASK_CONTROL_CANCELLING:
		return "CANCELLING"
	}
	if t.GetProgress() == lernav1.TaskProgress_TASK_PROGRESS_WAITING {
		return "WAITING"
	}
	return "RUNNING"
}

// View 返回任务的全量快照和当前阶段。
func (m *Module) View(ctx context.Context, user, taskID string) (*lernav1.TaskView, error) {
	v := &lernav1.TaskView{}
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		t, err := loadTask(tx, user, taskID)
		if err != nil {
			return err
		}
		if t == nil {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "task %s not found", taskID)
		}
		v.Task = t
		if v.Requirements, err = loadRequirementSet(tx, user, taskID, t.GetRequirementsVersion()); err != nil {
			return err
		}
		v.Phase = Phase(t, v.GetResult())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}
