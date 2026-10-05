package budget

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// 计费来源状态。
const (
	sourceUnknown     = "UNKNOWN"
	sourceProvisional = "PROVISIONAL"
	sourceFinal       = "FINAL"
)

// BillIssuerPrefix 是受信计费接入的调用者命名空间前缀；账单只能由它们回报。
const BillIssuerPrefix = "billing."

type reservation struct {
	id, task, op string
	budgets      []string
	ceiling      int64
	remaining    int64
	state        string
}

func loadReservation(tx *durable.Tx, user, opID string) (*reservation, error) {
	r := &reservation{op: opID}
	var budgets string
	err := tx.QueryRow(`SELECT reservation_id, task_id, budget_ids, ceiling, remaining, state FROM reservations
		WHERE user_id = ? AND operation_id = ?`, user, opID).Scan(&r.id, &r.task, &budgets, &r.ceiling, &r.remaining, &r.state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.budgets = strings.Split(budgets, ",")
	return r, nil
}

type source struct {
	id, status, native, extKey string
	maxAmount, portion         int64
	reservationID, op, task    string
}

func loadSource(tx *durable.Tx, user, id string) (*source, error) {
	s := &source{id: id}
	err := tx.QueryRow(`SELECT status, native_id, external_key, max_amount, portion, reservation_id, operation_id, task_id
		FROM billing_sources WHERE user_id = ? AND source_id = ?`, user, id).
		Scan(&s.status, &s.native, &s.extKey, &s.maxAmount, &s.portion, &s.reservationID, &s.op, &s.task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// ensureSource 在出口关联上建立内部来源：它占用预留中一次发送的上界，直到费用确定。
func ensureSource(tx *durable.Tx, user string, r *reservation, id, extKey string) (*source, error) {
	s, err := loadSource(tx, user, id)
	if err != nil || s != nil {
		return s, err
	}
	s = &source{id: id, status: sourceUnknown, portion: r.ceiling, reservationID: r.id, op: r.op, task: r.task, extKey: extKey}
	_, err = tx.Exec(`INSERT INTO billing_sources (user_id, source_id, task_id, operation_id, reservation_id, external_key, status,
		max_amount, portion, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		user, id, r.task, r.op, r.id, extKey, sourceUnknown, s.portion, tx.NowMs(), tx.NowMs())
	return s, err
}

func addUsed(tx *durable.Tx, user string, budgets []string, delta int64) error {
	for _, b := range budgets {
		if _, err := tx.Exec(`UPDATE budgets SET used = used + ? WHERE user_id = ? AND budget_id = ?`, delta, user, b); err != nil {
			return err
		}
	}
	return nil
}

func release(tx *durable.Tx, user string, r *reservation, amount int64) error {
	if amount <= 0 {
		return nil
	}
	for _, b := range r.budgets {
		if _, err := tx.Exec(`UPDATE budgets SET held = held - ? WHERE user_id = ? AND budget_id = ?`, amount, user, b); err != nil {
			return err
		}
	}
	r.remaining -= amount
	_, err := tx.Exec(`UPDATE reservations SET remaining = ? WHERE user_id = ? AND reservation_id = ?`, r.remaining, user, r.id)
	return err
}

// apply 把一份可信的用量写入来源（预算 2.1）：来源的已消耗额度取历史可信金额的最大值，
// 重复回报和乱序的累计值都不会重复计费；最终金额替换这次发送占用的预留。
func apply(tx *durable.Tx, user string, r *reservation, s *source, rep *lernav1.UsageReport) error {
	if rep.GetUnknown() {
		// 费用不明：记为未知而不是零，预留继续占用。
		return nil
	}
	if rep.GetMeasurement() == lernav1.Measurement_MEASUREMENT_REFUND {
		// 退款单独记录，不自动变回可用额度（M2 完整实现退款与贷记）。
		return nil
	}
	amount := rep.GetAmount()
	if amount > s.maxAmount {
		if err := addUsed(tx, user, r.budgets, amount-s.maxAmount); err != nil {
			return err
		}
		s.maxAmount = amount
	}
	status := sourceProvisional
	if rep.GetFinal() {
		status = sourceFinal
		if s.portion > 0 {
			if err := release(tx, user, r, s.portion); err != nil {
				return err
			}
			s.portion = 0
		}
	}
	if s.status == sourceFinal {
		status = sourceFinal
	}
	s.status = status
	native := s.native
	if native == "" {
		native = rep.GetNativeId()
	}
	_, err := tx.Exec(`UPDATE billing_sources SET status = ?, max_amount = ?, portion = ?, native_id = ?, updated_at = ?
		WHERE user_id = ? AND source_id = ?`, s.status, s.maxAmount, s.portion, native, tx.NowMs(), user, s.id)
	return err
}

func recordReport(tx *durable.Tx, user string, rep *lernav1.UsageReport, sourceID string) error {
	blob, err := proto.Marshal(rep)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO usage_reports (user_id, report_id, source_id, measurement, amount, final, unknown, record, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, user, rep.GetReportId(), sourceID, int32(rep.GetMeasurement()),
		rep.GetAmount(), boolInt(rep.GetFinal()), boolInt(rep.GetUnknown()), blob, tx.NowMs())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// handleReportUsage 接纳执行管理按 P7 交来的用量回报：按计费来源和来源修订去重，去重和生效在同一事务。
// 预算关闭、任务取消或关闭之后，已发生的费用仍然接纳。
func (m *Module) handleReportUsage(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	rep := in.Payload.(*lernav1.UsageReport)
	user := in.UserID()
	if rep.GetUserId() != user || rep.GetSourceId() == "" {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "usage report needs the user's internal billing source")
	}
	r, err := loadReservation(tx, user, rep.GetOperationId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if r == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "no reservation for operation %s", rep.GetOperationId())
	}
	s, err := ensureSource(tx, user, r, rep.GetSourceId(), rep.GetExternalKey())
	if err != nil {
		return durable.Outcome{}, err
	}
	if err := recordReport(tx, user, rep, s.id); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{}, apply(tx, user, r, s, rep)
}

// handleCloseReservation 在动作收尾后关闭预留：出口没有发生、以后也不会发生的部分释放；
// 可能已发出、费用尚未确定的发送继续占用对应的上界。
func (m *Module) handleCloseReservation(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.CloseReservationCommand)
	user := in.UserID()
	r, err := loadReservation(tx, user, cmd.GetOperationId())
	if err != nil || r == nil {
		return durable.Outcome{}, err
	}
	for _, sid := range cmd.GetDispatchedSources() {
		if _, err := ensureSource(tx, user, r, sid, ""); err != nil {
			return durable.Outcome{}, err
		}
	}
	var held int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(portion), 0) FROM billing_sources WHERE user_id = ? AND reservation_id = ?`,
		user, r.id).Scan(&held); err != nil {
		return durable.Outcome{}, err
	}
	if err := release(tx, user, r, r.remaining-held); err != nil {
		return durable.Outcome{}, err
	}
	_, err = tx.Exec(`UPDATE reservations SET state = 'CLOSED' WHERE user_id = ? AND reservation_id = ?`, user, r.id)
	return durable.Outcome{}, err
}

// handleIngestBill 接纳受信计费接入回报的供应商账单：按原生计费实例或外部键关联到原来源，
// 关联不上时保存为待核对，不凭金额相等自动匹配。任务关闭很久之后的账单同样接纳，不改写 Result。
func (m *Module) handleIngestBill(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	rep := in.Payload.(*lernav1.UsageReport)
	user := in.UserID()
	if !strings.HasPrefix(in.Identity().GetIssuerId(), BillIssuerPrefix) {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "only a trusted billing integration may report bills")
	}
	var sid string
	if rep.GetNativeId() != "" {
		err := tx.QueryRow(`SELECT source_id FROM billing_sources WHERE user_id = ? AND native_id = ?`, user, rep.GetNativeId()).Scan(&sid)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return durable.Outcome{}, err
		}
	}
	if sid == "" && rep.GetExternalKey() != "" {
		// 外部键关联到原尝试：只有恰好一个尚未确定的来源时才能确定归属。
		rows, err := tx.Query(`SELECT source_id FROM billing_sources WHERE user_id = ? AND external_key = ? AND status != ?`,
			user, rep.GetExternalKey(), sourceFinal)
		if err != nil {
			return durable.Outcome{}, err
		}
		var cands []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return durable.Outcome{}, err
			}
			cands = append(cands, id)
		}
		if err := rows.Close(); err != nil {
			return durable.Outcome{}, err
		}
		if len(cands) == 1 {
			sid = cands[0]
		}
	}
	if sid == "" {
		blob, err := proto.Marshal(rep)
		if err != nil {
			return durable.Outcome{}, err
		}
		_, err = tx.Exec(`INSERT INTO unmatched_bills (user_id, report_id, record, created_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			user, rep.GetReportId(), blob, tx.NowMs())
		return durable.Outcome{}, err
	}
	s, err := loadSource(tx, user, sid)
	if err != nil {
		return durable.Outcome{}, err
	}
	r, err := loadReservation(tx, user, s.op)
	if err != nil {
		return durable.Outcome{}, err
	}
	if err := recordReport(tx, user, rep, sid); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{}, apply(tx, user, r, s, rep)
}

// CloseTaskBudget 在任务关闭的事务中关闭任务预算：不再接受新消耗，但仍接受已发生的费用。
func CloseTaskBudget(tx *durable.Tx, user, taskID string) error {
	_, err := tx.Exec(`UPDATE budgets SET closed = 1 WHERE user_id = ? AND scope = ? AND task_id = ?`,
		user, int32(lernav1.BudgetScope_BUDGET_SCOPE_TASK), taskID)
	return err
}
