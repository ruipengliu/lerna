// Package budget 实现预算（docs/architecture/core/budget/README.md）：额度、预留、用量和结算。
// 属于裁决域；准入内的检查与预留参与任务编排的裁决事务，不是独立的远程准入操作。
//
// M1 只有一个维度（金额，整数微单位）：用户和当前任务各有明确上限；
// 按单次费用上界做最小预留，用量回报后结清；费用上界未知的调用不准入（预算 9）。
package budget

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

// Unit 是 M1 唯一的预算单位：美元的百万分之一。不使用二进制浮点数。
const Unit = "micro_usd"

// Module 是预算模块，绑定裁决域。
type Module struct {
	Domain *durable.Domain
}

// Register 登记预算接受的公共命令。
func (m *Module) Register() {
	m.Domain.HandleCommand(ports.CommandSetBudget,
		func() proto.Message { return &lernav1.SetBudgetCommand{} }, m.handleSet)
	m.Domain.HandleCommand(ports.CommandReportUsage,
		func() proto.Message { return &lernav1.UsageReport{} }, m.handleReportUsage)
	m.Domain.HandleCommand(ports.CommandCloseReservation,
		func() proto.Message { return &lernav1.CloseReservationCommand{} }, m.handleCloseReservation)
	m.Domain.HandleCommand(ports.CommandIngestBill,
		func() proto.Message { return &lernav1.UsageReport{} }, m.handleIngestBill)
}

func (m *Module) handleSet(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.SetBudgetCommand)
	if cmd.GetLimit() < 0 {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "limit must not be negative")
	}
	taskID := ""
	switch cmd.GetScope() {
	case lernav1.BudgetScope_BUDGET_SCOPE_USER:
	case lernav1.BudgetScope_BUDGET_SCOPE_TASK:
		if cmd.GetTaskId() == "" {
			return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "task budget needs task_id")
		}
		taskID = cmd.GetTaskId()
	default:
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown budget scope")
	}
	id, version, err := setLimit(tx, in.UserID(), cmd.GetScope(), taskID, cmd.GetLimit(), cmd.GetExpectedLimitVersion())
	if err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.SetBudgetResult{BudgetId: id, LimitVersion: version}}, nil
}

func setLimit(tx *durable.Tx, user string, scope lernav1.BudgetScope, taskID string, limit, expected int64) (string, int64, error) {
	var id string
	var version int64
	err := tx.QueryRow(`SELECT budget_id, limit_version FROM budgets WHERE user_id = ? AND scope = ? AND task_id = ?`,
		user, int32(scope), taskID).Scan(&id, &version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expected != 0 {
			return "", 0, errs.New(lernav1.ErrorCode_ERROR_CODE_REVISION_CONFLICT, "budget does not exist yet")
		}
		id = ids.New()
		_, err = tx.Exec(`INSERT INTO budgets (user_id, budget_id, scope, task_id, unit, limit_amount, limit_version, used, held, closed)
			VALUES (?, ?, ?, ?, ?, ?, 1, 0, 0, 0)`, user, id, int32(scope), taskID, Unit, limit)
		return id, 1, err
	case err != nil:
		return "", 0, err
	}
	if expected != version {
		return "", 0, errs.New(lernav1.ErrorCode_ERROR_CODE_REVISION_CONFLICT, "budget limit version is %d, expected %d", version, expected)
	}
	// 降额不抹掉已有责任：低于现有责任时只停止新准入。
	_, err = tx.Exec(`UPDATE budgets SET limit_amount = ?, limit_version = limit_version + 1 WHERE user_id = ? AND budget_id = ?`,
		limit, user, id)
	return id, version + 1, err
}

// EnsureTaskBudget 在任务创建事务中为当前任务设定明确上限。
func EnsureTaskBudget(tx *durable.Tx, user, taskID string, limit int64) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM budgets WHERE user_id = ? AND scope = ? AND task_id = ?`,
		user, int32(lernav1.BudgetScope_BUDGET_SCOPE_TASK), taskID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, _, err := setLimit(tx, user, lernav1.BudgetScope_BUDGET_SCOPE_TASK, taskID, limit, 0)
	return err
}

type budgetRow struct {
	id                string
	limit, used, held int64
	closed            bool
	scope             lernav1.BudgetScope
	taskID            string
	limitVersion      int64
}

func loadBudget(tx *durable.Tx, user string, scope lernav1.BudgetScope, taskID string) (*budgetRow, error) {
	r := &budgetRow{scope: scope, taskID: taskID}
	var closed int
	err := tx.QueryRow(`SELECT budget_id, limit_amount, used, held, closed, limit_version FROM budgets
		WHERE user_id = ? AND scope = ? AND task_id = ?`, user, int32(scope), taskID).
		Scan(&r.id, &r.limit, &r.used, &r.held, &closed, &r.limitVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.closed = closed == 1
	return r, nil
}

func (r *budgetRow) available() int64 {
	a := r.limit - r.used - r.held
	if a < 0 {
		return 0
	}
	return a
}

// ReserveRequest 描述准入时的预留。
type ReserveRequest struct {
	User        string
	TaskID      string
	AdmissionID string
	OperationID string
	Billing     *lernav1.Billing
	SendQuota   int32
	QueryQuota  int32
}

// Reserve 是"准入内检查与预留"（准入-8）：按单次费用上界 ×（发送额度 + 核对额度）预留，
// 用户和任务两级都检查；上界未知的调用不准入。与准入原子创建，超时不自动释放。
func Reserve(tx *durable.Tx, r ReserveRequest) (*lernav1.BudgetBasis, error) {
	b := r.Billing
	ceiling := int64(0)
	if b == nil || b.GetBilled() {
		if b == nil || !b.GetCeilingKnown() || b.GetCeiling() < 0 {
			return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "the cost ceiling of this call is unknown; it cannot be admitted")
		}
		if b.GetUnit() != "" && b.GetUnit() != Unit {
			return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "cost unit %q is not %q", b.GetUnit(), Unit)
		}
		ceiling = b.GetCeiling()
	}
	reserved := ceiling * int64(r.SendQuota+r.QueryQuota)
	user, err := loadBudget(tx, r.User, lernav1.BudgetScope_BUDGET_SCOPE_USER, "")
	if err != nil {
		return nil, err
	}
	task, err := loadBudget(tx, r.User, lernav1.BudgetScope_BUDGET_SCOPE_TASK, r.TaskID)
	if err != nil {
		return nil, err
	}
	if user == nil || task == nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "user and task need explicit budget limits")
	}
	for _, x := range []*budgetRow{user, task} {
		if x.closed {
			return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "budget %s is closed", x.id)
		}
		if x.available() < reserved {
			return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED,
				"budget %s has %d available, %d needed", x.id, x.available(), reserved)
		}
	}
	resID := ids.New()
	if _, err := tx.Exec(`INSERT INTO reservations (user_id, reservation_id, admission_id, operation_id, task_id, budget_ids,
		unit, ceiling, reserved, remaining, send_quota, sends_used, query_quota, queries_used, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 0, 'RESERVED', ?)`,
		r.User, resID, r.AdmissionID, r.OperationID, r.TaskID, user.id+","+task.id, Unit, ceiling, reserved, reserved,
		r.SendQuota, r.QueryQuota, tx.NowMs()); err != nil {
		return nil, err
	}
	for _, x := range []*budgetRow{user, task} {
		if _, err := tx.Exec(`UPDATE budgets SET held = held + ? WHERE user_id = ? AND budget_id = ?`, reserved, r.User, x.id); err != nil {
			return nil, err
		}
	}
	return &lernav1.BudgetBasis{
		BudgetIds:     []string{user.id, task.id},
		Unit:          Unit,
		Ceiling:       ceiling,
		ReservationId: resID,
		Reserved:      reserved,
	}, nil
}

// OccupySend 是开始-5：每个新的发送身份（包括安全重发）占用一次实际发送额度，
// 与开始回执同事务绑定。额度不够就等待，G1 的重发许可不自动扩大次数。
func OccupySend(tx *durable.Tx, user, operationID string, purpose lernav1.SendPurpose) error {
	var sendQuota, sendsUsed, queryQuota, queriesUsed int32
	var state string
	err := tx.QueryRow(`SELECT send_quota, sends_used, query_quota, queries_used, state FROM reservations
		WHERE user_id = ? AND operation_id = ?`, user, operationID).Scan(&sendQuota, &sendsUsed, &queryQuota, &queriesUsed, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "no reservation for operation %s", operationID)
	}
	if err != nil {
		return err
	}
	if strings.HasPrefix(state, "CLOSED") {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "reservation for %s is closed", operationID)
	}
	switch purpose {
	case lernav1.SendPurpose_SEND_PURPOSE_QUERY:
		if queriesUsed >= queryQuota {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "query quota of %s exhausted (%d)", operationID, queryQuota)
		}
		_, err = tx.Exec(`UPDATE reservations SET queries_used = queries_used + 1 WHERE user_id = ? AND operation_id = ?`, user, operationID)
	default:
		if sendsUsed >= sendQuota {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED, "send quota of %s exhausted (%d)", operationID, sendQuota)
		}
		_, err = tx.Exec(`UPDATE reservations SET sends_used = sends_used + 1, state = 'MAY_BILL' WHERE user_id = ? AND operation_id = ?`, user, operationID)
	}
	return err
}

// View 返回用户的预算视图。
func (m *Module) View(ctx context.Context, user string) (*lernav1.BudgetView, error) {
	v := &lernav1.BudgetView{}
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		rows, err := tx.Query(`SELECT budget_id, scope, task_id, unit, limit_amount, limit_version, used, held, closed
			FROM budgets WHERE user_id = ? ORDER BY scope, task_id`, user)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			b := &lernav1.Budget{UserId: user}
			var scope int32
			var closed int
			if err := rows.Scan(&b.BudgetId, &scope, &b.TaskId, &b.Unit, &b.Limit, &b.LimitVersion, &b.Used, &b.Held, &closed); err != nil {
				return err
			}
			b.Scope = lernav1.BudgetScope(scope)
			b.Closed = closed == 1
			b.Available = max(0, b.GetLimit()-b.GetUsed()-b.GetHeld())
			b.Deficit = max(0, b.GetUsed()+b.GetHeld()-b.GetLimit())
			v.Budgets = append(v.Budgets, b)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		_ = rows.Close()
		for _, b := range v.Budgets {
			q := `SELECT COUNT(*) FROM billing_sources WHERE user_id = ? AND status = 'UNKNOWN'`
			args := []any{user}
			if b.GetScope() == lernav1.BudgetScope_BUDGET_SCOPE_TASK {
				q += ` AND task_id = ?`
				args = append(args, b.GetTaskId())
			}
			if err := tx.QueryRow(q, args...).Scan(&b.UnknownSources); err != nil {
				return err
			}
		}
		srows, err := tx.Query(`SELECT source_id, operation_id, task_id, native_id, status, max_amount, portion FROM billing_sources
			WHERE user_id = ? ORDER BY created_at, source_id`, user)
		if err != nil {
			return err
		}
		defer func() { _ = srows.Close() }()
		for srows.Next() {
			sv := &lernav1.BillingSourceView{}
			if err := srows.Scan(&sv.SourceId, &sv.OperationId, &sv.TaskId, &sv.NativeId, &sv.Status, &sv.Amount, &sv.Held); err != nil {
				return err
			}
			if sv.GetStatus() != sourceFinal {
				v.UnresolvedSources = append(v.UnresolvedSources, sv.GetSourceId())
			}
			v.Sources = append(v.Sources, sv)
		}
		if err := srows.Err(); err != nil {
			return err
		}
		urows, err := tx.Query(`SELECT report_id FROM unmatched_bills WHERE user_id = ? ORDER BY created_at`, user)
		if err != nil {
			return err
		}
		defer func() { _ = urows.Close() }()
		for urows.Next() {
			var id string
			if err := urows.Scan(&id); err != nil {
				return err
			}
			v.UnmatchedBills = append(v.UnmatchedBills, id)
		}
		return urows.Err()
	})
	return v, err
}
