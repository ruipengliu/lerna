package ledger

import (
	"context"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
)

// RequestTTL 是每次物理发送携带的请求期限：目标在期限之后到达的请求不得生效。
const RequestTTL = 10 * time.Minute

// 恢复策略（执行管理 4.2）：由能力组合决定。
const (
	strategyQuery  = "query"
	strategyResend = "resend"
	strategyKeep   = "keep"
)

// 核对退避参数：等待时间 = Uniform(0, min(上限, 初始间隔 × 2^连续无进展次数))，以可见性延迟为下界。
const (
	reconcileBase    = time.Second
	reconcileCeiling = 5 * time.Minute
)

// strategy 返回结果未知后的恢复策略。未声明、声明冲突的能力按最保守处理。
func strategy(st *opState) string {
	d := st.op.GetCapabilitySnapshot()
	switch {
	case d.GetQuery().GetSupported():
		return strategyQuery
	case d.GetIdempotency().GetSupported() && d.GetIdempotency().GetKeyValiditySeconds() > 0:
		return strategyResend
	default:
		return strategyKeep
	}
}

// recoveryPlan 在效果仍未知时决定下一步，并持久安排（执行管理 4.3）：下次核对时间持久保存，
// 工作者不靠长时间睡眠来维持调度；次数上限耗尽只让核对暂停，未知和迟到可能性保留。
func (m *Module) recoveryPlan(tx *durable.Tx, st *opState, op *lernav1.Operation) (durable.Transition, error) {
	if op.GetEvidenceConflict() {
		op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
		op.PauseReason = "证据冲突：停止依赖它的推进，等待核对"
		return durable.Done(), nil
	}
	if !st.anyDispatched() {
		return durable.Done(), nil
	}
	switch strategy(st) {
	case strategyQuery:
		used := st.count(lernav1.SendPurpose_SEND_PURPOSE_QUERY, false)
		if used >= op.GetQueryQuota() {
			if next, ok := m.resendPlan(tx, st, op); ok {
				return next, nil
			}
			op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
			op.PauseReason = "核对次数已用尽（" + strconv.Itoa(int(used)) + " 次）；未知和迟到可能性保留，可追加核对额度后恢复"
			return durable.Done(), nil
		}
		d := op.GetCapabilitySnapshot().GetQuery()
		wait := durable.Backoff(op.GetOperationId(), int(op.GetReconcileChecks()), reconcileBase, reconcileCeiling,
			time.Duration(d.GetVisibilityDelayMs())*time.Millisecond)
		next := tx.Now().Add(wait)
		op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PENDING
		op.PauseReason = ""
		op.NextReconcileAt = timestamppb.New(next)
		return durable.WaitUntil(next, "next reconciliation query"), nil
	case strategyResend:
		if next, ok := m.resendPlan(tx, st, op); ok {
			return next, nil
		}
		op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
		if op.GetPauseReason() == "" {
			op.PauseReason = "无法证明安全重发：保持未知"
		}
		return durable.Done(), nil
	default:
		op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
		op.PauseReason = "目标既不可查询也不幂等：保持未知，等待外部证据或人工核对"
		return durable.Done(), nil
	}
}

// notAfter 返回原尝试全部执行发送中最晚的请求期限。
func notAfter(st *opState) time.Time {
	var out time.Time
	for _, s := range st.sends {
		if s.purpose == lernav1.SendPurpose_SEND_PURPOSE_EXECUTE && s.dispatchPossible {
			if d := s.dispatchAt.Add(RequestTTL); d.After(out) {
				out = d
			}
		}
	}
	return out
}

// recover 在到期时执行恢复：发出一次核对查询，或按能力安全重发。
func (m *Module) recover(ctx context.Context, c *durable.Claim, st *opState) error {
	if next := st.op.GetNextReconcileAt(); next != nil && m.Domain.Now().Before(next.AsTime()) {
		return m.Domain.Advance(ctx, c, "ledger:reconcile_wait", func(*durable.Tx) (durable.Transition, error) {
			return durable.WaitUntil(next.AsTime(), "next reconciliation"), nil
		})
	}
	if st.op.GetReconcileState() != lernav1.ReconcileState_RECONCILE_STATE_PENDING {
		return m.Domain.Advance(ctx, c, "ledger:reconcile_idle", func(*durable.Tx) (durable.Transition, error) {
			return durable.Done(), nil
		})
	}
	if strategy(st) == strategyQuery && st.count(lernav1.SendPurpose_SEND_PURPOSE_QUERY, false) < st.op.GetQueryQuota() {
		return m.query(ctx, c, st)
	}
	return m.resend(ctx, c, st)
}

// query 发出一次核对查询：查询本身也经过开始门禁（占用核对额度）和出口闸门，
// 只按原尝试查询，不重新执行原动作。
func (m *Module) query(ctx context.Context, c *durable.Claim, st *opState) error {
	a := st.attempt()
	seq := st.count(lernav1.SendPurpose_SEND_PURPOSE_QUERY, false) + 1
	s := &sendRow{attemptID: a.GetAttemptId(), seq: seq, purpose: lernav1.SendPurpose_SEND_PURPOSE_QUERY, claimEpoch: c.Epoch}
	if err := m.Domain.Advance(ctx, c, "ledger:reconcile_prepare", func(tx *durable.Tx) (durable.Transition, error) {
		return durable.Keep(), insertSend(tx, c.User, st.op.GetOperationId(), s)
	}); err != nil {
		return err
	}
	out := m.gateSend(st, a, s, false)
	out.Execute = nil
	out.Query = &lernav1.QueryRequest{
		UserId:        st.op.GetUserId(),
		OperationId:   st.op.GetOperationId(),
		AttemptId:     a.GetAttemptId(),
		ExternalKey:   a.GetExternalKey(),
		CapabilityId:  st.op.GetCapabilityId(),
		Arguments:     st.op.GetParameters(),
		SendSeq:       seq,
		CredentialRef: st.intent.GetCredentialRef(),
	}
	if na := notAfter(st); !na.IsZero() {
		out.Query.NotAfter = timestamppb.New(na)
	}
	err := m.Gate.Call(ctx, out, egress.Hooks{
		DispatchPossible: func(ctx context.Context, rec *lernav1.Receipt) error { return m.dispatchPossible(ctx, c, st, s, rec) },
		Observe: func(ctx context.Context, rep *lernav1.ExecutionReport, ioErr error) error {
			return m.observe(ctx, st.op, s, rep, ioErr)
		},
	})
	var rej *egress.ErrStartRejected
	if asStart(err, &rej) {
		// 当前授权或额度不允许核对：停止相关查询，记录缺少的许可；未知不转为未生效。
		return m.Domain.Advance(ctx, c, "ledger:reconcile_refused", func(tx *durable.Tx) (durable.Transition, error) {
			cur, err := loadState(tx, c.User, c.Subject)
			if err != nil {
				return durable.Transition{}, err
			}
			op := proto.Clone(cur.op).(*lernav1.Operation)
			op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
			op.PauseReason = "核对查询被开始门禁拒绝：" + errs.FromProto(rej.Rejection).Error()
			if err := saveOperation(tx, op); err != nil {
				return durable.Transition{}, err
			}
			cur.op = op
			return durable.Done(), m.notify(tx, op, cur)
		})
	}
	if err != nil {
		return err
	}
	return m.decide(ctx, c)
}

func asStart(err error, target **egress.ErrStartRejected) bool {
	if err == nil {
		return false
	}
	r, ok := err.(*egress.ErrStartRejected)
	if ok {
		*target = r
	}
	return ok
}

// handleResume 恢复已暂停的核对：裁决域已经以收尾用途准入了追加的核对额度。
func (m *Module) handleResume(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.ResumeReconciliationCommand)
	st, err := loadState(tx, in.UserID(), cmd.GetOperationId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if st == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "operation %s not found", cmd.GetOperationId())
	}
	op := proto.Clone(st.op).(*lernav1.Operation)
	op.QueryQuota += cmd.GetExtraQueries()
	if op.GetLifecycle() != lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
		op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PENDING
		op.PauseReason = ""
		op.NextReconcileAt = nil
	}
	if err := saveOperation(tx, op); err != nil {
		return durable.Outcome{}, err
	}
	st.op = op
	if _, err := tx.EnqueueJob(durable.JobSpec{Kind: JobExecute, User: op.GetUserId(), Subject: op.GetOperationId(),
		PurposeKey: "execute:" + op.GetOperationId() + ":resume:" + in.Identity().GetCommandId()}); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{}, m.notify(tx, op, st)
}
