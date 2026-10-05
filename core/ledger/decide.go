package ledger

import (
	"context"
	"strconv"

	"google.golang.org/protobuf/proto"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

// interpretation 是受信解释规则对一条观察的结论。
type interpretation struct {
	effect lernav1.EffectOutcome
	late   lernav1.LateEffect
}

// interpret 是 M1 参考适配器的受信解释规则（执行管理 2.3）：基于可信出口固定的原始观察，
// 按能力声明判定效果和迟到可能性。HTTP 200、"已接受"回执或适配器的一句"完成"都不自动满足"已生效"；
// 强结论（NOT_APPLIED、RULED_OUT）必须覆盖全部可能已发出的发送。
func interpret(st *opState, o *obsRow) interpretation {
	unknown := interpretation{effect: lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN, late: lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR}
	decl := st.op.GetCapabilitySnapshot()
	rep := o.report
	provable := decl.GetLateFinality().GetProvable()
	dispatched := st.count(lernav1.SendPurpose_SEND_PURPOSE_EXECUTE, true)
	sameAttempt := len(st.attempts) <= 1
	idem := decl.GetIdempotency().GetSupported()
	switch o.o.GetPurpose() {
	case lernav1.SendPurpose_SEND_PURPOSE_EXECUTE:
		switch {
		case rep.GetStatus() == lernav1.ExecutionStatus_EXECUTION_STATUS_FINISHED && rep.GetClaimedEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED:
			late := lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR
			// 终局回执只覆盖同一外部键：多次发送时必须由幂等保证它们是同一个外部动作。
			if rep.GetClaimsTerminal() && provable && sameAttempt && (dispatched <= 1 || idem) {
				late = lernav1.LateEffect_LATE_EFFECT_RULED_OUT
			}
			return interpretation{effect: lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED, late: late}
		case rep.GetClaimsTerminal() && rep.GetClaimedEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED &&
			(rep.GetStatus() == lernav1.ExecutionStatus_EXECUTION_STATUS_NOT_EXECUTED || rep.GetStatus() == lernav1.ExecutionStatus_EXECUTION_STATUS_RATE_LIMITED):
			// 目标明确拒绝了这一次发送；只有它是唯一可能已发出的发送时，才证明整个动作未生效。
			if dispatched <= 1 && sameAttempt {
				return interpretation{effect: lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED, late: lernav1.LateEffect_LATE_EFFECT_RULED_OUT}
			}
			return unknown
		default:
			return unknown
		}
	case lernav1.SendPurpose_SEND_PURPOSE_QUERY:
		if rep.GetStatus() != lernav1.ExecutionStatus_EXECUTION_STATUS_FINISHED || !rep.GetClaimsTerminal() || !provable || !sameAttempt {
			// "暂时没看到"必须保持未知（G1）。
			return interpretation{}
		}
		switch rep.GetClaimedEffect() {
		case lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED:
			return interpretation{effect: lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED, late: lernav1.LateEffect_LATE_EFFECT_RULED_OUT}
		case lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED:
			return interpretation{effect: lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED, late: lernav1.LateEffect_LATE_EFFECT_RULED_OUT}
		}
	}
	return interpretation{}
}

// applyEvidence 把一条结论写入动作。已生效和未生效不得直接互相覆盖：出现相反证据时保留原观察，
// 记录冲突，停止依赖它的推进，并重新核对（核心契约 2.5）。
func applyEvidence(op *lernav1.Operation, in interpretation, obsID string) bool {
	if in.effect == lernav1.EffectOutcome_EFFECT_OUTCOME_UNSPECIFIED || in.effect == lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN {
		return false
	}
	cur := op.GetEffect()
	strongCur := cur == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED ||
		(cur == lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED && len(op.GetClosureEvidenceRefs()) > 0)
	if strongCur && cur != in.effect {
		op.EvidenceConflict = true
		op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, "conflict:"+obsID)
		return true
	}
	op.Effect = in.effect
	if in.late == lernav1.LateEffect_LATE_EFFECT_RULED_OUT {
		op.LateEffect = lernav1.LateEffect_LATE_EFFECT_RULED_OUT
	}
	op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, obsID)
	return true
}

// settleable 是收尾依据（执行管理 2.3）：派发可封闭、迟到可能性已排除、没有未处理的证据冲突，且效果确定。
func settleable(op *lernav1.Operation) bool {
	return !op.GetEvidenceConflict() && op.GetLateEffect() == lernav1.LateEffect_LATE_EFFECT_RULED_OUT &&
		(op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED || op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED)
}

// decide 是 P8：执行管理依据观察裁决效果，形成收尾依据或进入恢复；然后通知任务编排。
func (m *Module) decide(ctx context.Context, c *durable.Claim) error {
	return m.Domain.Advance(ctx, c, "ledger:p8_decide", func(tx *durable.Tx) (durable.Transition, error) {
		st, err := loadState(tx, c.User, c.Subject)
		if err != nil {
			return durable.Transition{}, err
		}
		op := proto.Clone(st.op).(*lernav1.Operation)
		changed := false
		for _, o := range st.obs {
			if o.decided {
				continue
			}
			if applyEvidence(op, interpret(st, o), o.o.GetObservationId()) {
				changed = true
			}
			if _, err := tx.Exec(`UPDATE observations SET decided = 1 WHERE user_id = ? AND observation_id = ?`,
				c.User, o.o.GetObservationId()); err != nil {
				return durable.Transition{}, err
			}
		}
		if settleable(op) && op.GetLifecycle() != lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
			op.Dispatch = lernav1.DispatchState_DISPATCH_STATE_SEALED
			if op.GetSealReason() == "" {
				op.SealReason = "settled"
			}
			op.Lifecycle = lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED
			op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_DONE
			changed = true
		}
		var tr durable.Transition
		if op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
			tr = durable.Done()
		} else {
			if tr, err = m.recoveryPlan(tx, st, op); err != nil {
				return durable.Transition{}, err
			}
			changed = true
		}
		if changed {
			if err := saveOperation(tx, op); err != nil {
				return durable.Transition{}, err
			}
			if op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
				if err := m.afterSettle(tx, op); err != nil {
					return durable.Transition{}, err
				}
			}
		}
		st.op = op
		if err := m.notify(tx, op, st); err != nil {
			return durable.Transition{}, err
		}
		return tr, nil
	})
}

// recoveryPlan 在效果仍未知时，按能力声明的组合决定恢复策略（执行管理 4.2）。
func (m *Module) recoveryPlan(_ *durable.Tx, _ *opState, op *lernav1.Operation) (durable.Transition, error) {
	op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
	op.PauseReason = "no automatic recovery for this capability"
	return durable.Done(), nil
}

// sourceID 是一次发送的内部计费来源：出口之前就由发送身份固定，供应商身份返回后登记为别名。
func sourceID(attemptID string, purpose lernav1.SendPurpose, seq int32, item int) string {
	return "send:" + attemptID + ":" + purpose.String() + ":" + strconv.Itoa(int(seq)) + ":" + strconv.Itoa(item)
}

// afterObserve 在保存观察的同一事务中登记用量交接（P7）：用量交预算，按计费来源去重；
// 没有拿到用量时记为未知，不按零继续。
func (m *Module) afterObserve(tx *durable.Tx, op *lernav1.Operation, s *sendRow, o *lernav1.Observation, rep *lernav1.ExecutionReport) error {
	it, err := loadIntent(tx, op.GetUserId(), op.GetOperationId())
	if err != nil {
		return err
	}
	items := rep.GetUsage()
	if len(items) == 0 {
		items = []*lernav1.UsageItem{{Unknown: true}}
	}
	ext := ""
	if a := attemptOf(tx, op.GetUserId(), s.attemptID); a != "" {
		ext = a
	}
	for i, u := range items {
		r := &lernav1.UsageReport{
			ReportId:       "usage:" + o.GetObservationId() + ":" + strconv.Itoa(i),
			UserId:         op.GetUserId(),
			OperationId:    op.GetOperationId(),
			AttemptId:      s.attemptID,
			SendSeq:        s.seq,
			BillingSource:  u.GetBillingSource(),
			SourceRevision: u.GetSourceRevision(),
			Measurement:    lernav1.Measurement_MEASUREMENT_CUMULATIVE,
			Unit:           u.GetUnit(),
			Amount:         u.GetAmount(),
			Final:          u.GetFinal(),
			Unknown:        u.GetUnknown(),
			PriceVersion:   u.GetPriceVersion(),
			EvidenceRefs:   []string{o.GetObservationId()},
			TaskId:         op.GetTaskId(),
			ReservationId:  it.GetBudgetBasis().GetReservationId(),
			SourceId:       sourceID(s.attemptID, s.purpose, s.seq, i),
			NativeId:       u.GetNativeId(),
			ExternalKey:    ext,
		}
		if err := tx.EnqueueHandoff(durable.Handoff{
			ID:        r.GetReportId(),
			User:      op.GetUserId(),
			Target:    m.AdjudicationDomain,
			Kind:      ports.CommandReportUsage,
			Payload:   r,
			IntentRef: op.GetOperationId(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func attemptOf(tx *durable.Tx, user, attemptID string) string {
	var key string
	_ = tx.QueryRow(`SELECT external_key FROM attempts WHERE user_id = ? AND attempt_id = ?`, user, attemptID).Scan(&key)
	return key
}

// afterSettle 在动作收尾的同一事务中请求预算关闭预留：从未发出的部分释放，可能已发出、
// 费用尚未确定的发送继续占用。
func (m *Module) afterSettle(tx *durable.Tx, op *lernav1.Operation) error {
	st, err := loadState(tx, op.GetUserId(), op.GetOperationId())
	if err != nil {
		return err
	}
	cmd := &lernav1.CloseReservationCommand{UserId: op.GetUserId(), TaskId: op.GetTaskId(), OperationId: op.GetOperationId(),
		Reason: "operation settled"}
	for _, s := range st.sends {
		if s.dispatchPossible {
			cmd.DispatchedSources = append(cmd.DispatchedSources, sourceID(s.attemptID, s.purpose, s.seq, 0))
		}
	}
	return tx.EnqueueHandoff(durable.Handoff{
		ID:        "close-reservation:" + op.GetOperationId(),
		User:      op.GetUserId(),
		Target:    m.AdjudicationDomain,
		Kind:      ports.CommandCloseReservation,
		Payload:   cmd,
		IntentRef: op.GetOperationId(),
	})
}
