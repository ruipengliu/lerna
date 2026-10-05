// Package sessions 实现会话（docs/architecture/core/sessions/README.md）：
// 用户输入、确认、与任务的关联。会话独立于任务存在，属于裁决域。
package sessions

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
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

// TaskPort 是任务编排在同一裁决事务中接纳会话输入的内部接口。
type TaskPort interface {
	// NewGoal 登记"为新目标建立任务和初始条件"的工作。
	NewGoal(tx *durable.Tx, in GoalInput) error
}

// GoalInput 是已持久保存的新目标输入。
type GoalInput struct {
	User         string
	SessionID    string
	InputID      string
	Text         string
	Requirements *lernav1.RequirementSetDraft
}

// Module 是会话模块，绑定裁决域。
type Module struct {
	Domain *durable.Domain
	Tasks  TaskPort
}

// Register 登记会话接受的公共命令。
func (m *Module) Register() {
	m.Domain.HandleCommand(ports.CommandCreateSession,
		func() proto.Message { return &lernav1.CreateSessionCommand{} }, m.handleCreateSession)
	m.Domain.HandleCommand(ports.CommandSubmitInput,
		func() proto.Message { return &lernav1.SubmitInputCommand{} }, m.handleSubmitInput)
}

func (m *Module) handleCreateSession(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	sid := ids.New()
	if err := createSession(tx, in.UserID(), sid); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.CreateSessionResult{SessionId: sid}}, nil
}

func createSession(tx *durable.Tx, user, sid string) error {
	_, err := tx.Exec(`INSERT INTO sessions (user_id, session_id, status, last_committed_seq, revision, created_at)
		VALUES (?, ?, ?, 0, 1, ?)`, user, sid, int32(lernav1.SessionStatus_SESSION_STATUS_ACTIVE), tx.NowMs())
	return err
}

// handleSubmitInput 是持久点 1：保存输入、顺序和确认绑定后才返回回执。
func (m *Module) handleSubmitInput(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.SubmitInputCommand)
	user := in.UserID()
	if cmd.GetForkFromMessageId() != "" || cmd.GetEditMessageId() != "" {
		// 分支与历史编辑：M1 明确返回"不支持"，不原地覆盖（会话 9）。
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "session branching and history editing are not supported in M1")
	}
	sid := cmd.GetSessionId()
	if sid == "" {
		sid = ids.New()
		if err := createSession(tx, user, sid); err != nil {
			return durable.Outcome{}, err
		}
	} else if ok, err := sessionExists(tx, user, sid); err != nil {
		return durable.Outcome{}, err
	} else if !ok {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "session %s not found", sid)
	}
	switch cmd.GetInputKind() {
	case lernav1.InputKind_INPUT_KIND_NEW_GOAL:
		if cmd.GetText() == "" {
			return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "new goal needs text")
		}
		rec, err := appendInput(tx, user, sid, cmd)
		if err != nil {
			return durable.Outcome{}, err
		}
		if err := m.Tasks.NewGoal(tx, GoalInput{
			User: user, SessionID: sid, InputID: rec.GetInputId(), Text: cmd.GetText(), Requirements: cmd.GetRequirements(),
		}); err != nil {
			return durable.Outcome{}, err
		}
		return outcome(rec), nil
	case lernav1.InputKind_INPUT_KIND_RECORD_ONLY:
		// 仅记录：会话保存，但不创建任务，也不投递给任意的"当前任务"。
		rec, err := appendInput(tx, user, sid, cmd)
		if err != nil {
			return durable.Outcome{}, err
		}
		return outcome(rec), nil
	default:
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "input kind %s", cmd.GetInputKind())
	}
}

func outcome(rec *lernav1.SessionInput) durable.Outcome {
	return durable.Outcome{Result: &lernav1.SubmitInputResult{
		SessionId:     rec.GetSessionId(),
		InputId:       rec.GetInputId(),
		SessionSeq:    rec.GetSessionSeq(),
		RoutingStatus: rec.GetRoutingStatus(),
		TaskId:        rec.GetTaskId(),
	}}
}

func sessionExists(tx *durable.Tx, user, sid string) (bool, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND session_id = ?`, user, sid).Scan(&n)
	return n > 0, err
}

// appendInput 锁定会话头，分配连续的事件序号，把输入和会话头一起写入（会话 2.5）。
func appendInput(tx *durable.Tx, user, sid string, cmd *lernav1.SubmitInputCommand) (*lernav1.SessionInput, error) {
	var seq int64
	if err := tx.QueryRow(`SELECT last_committed_seq + 1 FROM sessions WHERE user_id = ? AND session_id = ?`, user, sid).Scan(&seq); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE sessions SET last_committed_seq = ?, revision = revision + 1 WHERE user_id = ? AND session_id = ?`,
		seq, user, sid); err != nil {
		return nil, err
	}
	var reqBlob []byte
	if cmd.GetRequirements() != nil {
		b, err := proto.Marshal(cmd.GetRequirements())
		if err != nil {
			return nil, err
		}
		reqBlob = b
	}
	rec := &lernav1.SessionInput{
		UserId:        user,
		SessionId:     sid,
		SessionSeq:    seq,
		InputId:       ids.New(),
		InputKind:     cmd.GetInputKind(),
		TaskId:        cmd.GetTaskId(),
		RequestId:     cmd.GetRequestId(),
		RoutingStatus: lernav1.RoutingStatus_ROUTING_STATUS_RECORDED,
		RecordedAt:    timestamppb.New(tx.Now()),
	}
	_, err := tx.Exec(`INSERT INTO session_inputs (user_id, input_id, session_id, session_seq, input_kind, task_id,
		request_id, body, requirements, routing_status, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user, rec.GetInputId(), sid, seq, int32(rec.GetInputKind()), rec.GetTaskId(), rec.GetRequestId(),
		cmd.GetText(), reqBlob, int32(rec.GetRoutingStatus()), tx.NowMs())
	return rec, err
}

// MarkInputAccepted 记下输入已被任务接纳（输入投递：已记录 → 任务已接纳）。
func MarkInputAccepted(tx *durable.Tx, user, inputID, taskID string) error {
	_, err := tx.Exec(`UPDATE session_inputs SET routing_status = ?, task_id = ? WHERE user_id = ? AND input_id = ?`,
		int32(lernav1.RoutingStatus_ROUTING_STATUS_TASK_ACCEPTED), taskID, user, inputID)
	return err
}

// LinkTask 保存会话与任务的关联。关联不转移任务负责方，也不是授权。
func LinkTask(tx *durable.Tx, user, sessionID, taskID string) error {
	_, err := tx.Exec(`INSERT INTO session_tasks (user_id, session_id, task_id, role, created_at)
		VALUES (?, ?, ?, 'CONTROL', ?) ON CONFLICT DO NOTHING`, user, sessionID, taskID, tx.NowMs())
	return err
}

// InputText 返回输入正文。
func InputText(tx *durable.Tx, user, inputID string) (string, error) {
	var body string
	err := tx.QueryRow(`SELECT body FROM session_inputs WHERE user_id = ? AND input_id = ?`, user, inputID).Scan(&body)
	return body, err
}

// OpenRequest 发布一个输入请求（核心调用）：事项已保存，可以呈现。
func OpenRequest(tx *durable.Tx, r *lernav1.InputRequest) error {
	_, err := tx.Exec(`INSERT INTO input_requests (user_id, request_id, session_id, task_id, question, changes_basis, open, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?) ON CONFLICT DO NOTHING`,
		r.GetUserId(), r.GetRequestId(), r.GetSessionId(), r.GetTaskId(), r.GetQuestion(), boolInt(r.GetChangesBasis()), tx.NowMs())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// View 返回会话的全量快照。
func (m *Module) View(ctx context.Context, user, sid string) (*lernav1.SessionView, error) {
	v := &lernav1.SessionView{}
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		s := &lernav1.Session{UserId: user, SessionId: sid}
		var created int64
		var status int32
		err := tx.QueryRow(`SELECT status, last_committed_seq, revision, created_at FROM sessions WHERE user_id = ? AND session_id = ?`,
			user, sid).Scan(&status, &s.LastCommittedSeq, &s.Revision, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "session %s not found", sid)
		}
		if err != nil {
			return err
		}
		s.Status = lernav1.SessionStatus(status)
		s.CreatedAt = timestamppb.New(msTime(created))
		v.Session = s
		rows, err := tx.Query(`SELECT input_id, session_seq, input_kind, task_id, request_id, content_ref, routing_status, recorded_at
			FROM session_inputs WHERE user_id = ? AND session_id = ? ORDER BY session_seq`, user, sid)
		if err != nil {
			return err
		}
		for rows.Next() {
			in := &lernav1.SessionInput{UserId: user, SessionId: sid}
			var kind, routing int32
			var at int64
			if err := rows.Scan(&in.InputId, &in.SessionSeq, &kind, &in.TaskId, &in.RequestId, &in.ContentRef, &routing, &at); err != nil {
				_ = rows.Close()
				return err
			}
			in.InputKind = lernav1.InputKind(kind)
			in.RoutingStatus = lernav1.RoutingStatus(routing)
			in.RecordedAt = timestamppb.New(msTime(at))
			v.Inputs = append(v.Inputs, in)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = tx.Query(`SELECT task_id FROM session_tasks WHERE user_id = ? AND session_id = ? ORDER BY created_at, task_id`, user, sid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				_ = rows.Close()
				return err
			}
			v.TaskIds = append(v.TaskIds, t)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = tx.Query(`SELECT request_id, task_id, question, changes_basis FROM input_requests
			WHERE user_id = ? AND session_id = ? AND open = 1 ORDER BY created_at`, user, sid)
		if err != nil {
			return err
		}
		for rows.Next() {
			r := &lernav1.InputRequest{UserId: user, SessionId: sid, Open: true}
			var cb int
			if err := rows.Scan(&r.RequestId, &r.TaskId, &r.Question, &cb); err != nil {
				_ = rows.Close()
				return err
			}
			r.ChangesBasis = cb == 1
			v.OpenRequests = append(v.OpenRequests, r)
		}
		return rows.Close()
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func msTime(ms int64) time.Time { return time.UnixMilli(ms) }
