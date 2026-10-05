package ledger

import (
	"context"
	"strconv"

	"google.golang.org/protobuf/proto"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// handleSealDispatch 封闭尚未开始的派发（执行管理 3、4.4）。封闭不可逆；只作用于准确的动作清单，
// 不影响以后新准入的动作。封闭先于出口开放的动作由原执行管理负责方出具内部封闭证明；
// 已经可能发出的只停止后续写入，继续核对。封闭回执不得用来断言原动作未生效。
func (m *Module) handleSealDispatch(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.SealDispatchCommand)
	user := in.UserID()
	ids := cmd.GetOperationIds()
	if len(ids) == 0 {
		rows, err := tx.Query(`SELECT operation_id FROM operations WHERE user_id = ? AND task_id = ? AND dispatch = ?`,
			user, cmd.GetTaskId(), int32(lernav1.DispatchState_DISPATCH_STATE_OPEN))
		if err != nil {
			return durable.Outcome{}, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return durable.Outcome{}, err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return durable.Outcome{}, err
		}
	}
	res := &lernav1.SealDispatchResult{}
	for _, id := range ids {
		st, err := loadState(tx, user, id)
		if err != nil {
			return durable.Outcome{}, err
		}
		if st == nil {
			// 封闭先于意图被接纳：保存封闭，迟到的意图会被接纳为"禁止执行"。
			if _, err := tx.Exec(`INSERT INTO seals (user_id, operation_id, task_id, reason, created_at) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT DO NOTHING`, user, id, cmd.GetTaskId(), cmd.GetReason(), tx.NowMs()); err != nil {
				return durable.Outcome{}, err
			}
			res.SealedAheadOfIntent = append(res.SealedAheadOfIntent, id)
			continue
		}
		if st.anyDispatched() {
			res.DispatchPossible = append(res.DispatchPossible, id)
			if st.op.GetDispatch() == lernav1.DispatchState_DISPATCH_STATE_OPEN {
				op := proto.Clone(st.op).(*lernav1.Operation)
				op.Dispatch = lernav1.DispatchState_DISPATCH_STATE_SEALED
				op.SealReason = cmd.GetReason()
				if err := saveOperation(tx, op); err != nil {
					return durable.Outcome{}, err
				}
				st.op = op
				if err := m.notify(tx, op, st); err != nil {
					return durable.Outcome{}, err
				}
			}
			continue
		}
		res.SealedBeforeDispatch = append(res.SealedBeforeDispatch, id)
		if st.op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
			continue
		}
		if err := m.internalClosure(tx, st, cmd.GetReason()); err != nil {
			return durable.Outcome{}, err
		}
	}
	return durable.Outcome{Result: res}, nil
}

// internalClosure 出具内部封闭证明：完整的尝试和发送记录表明没有任何发送进入"可能已发出"，
// 封闭在执行管理域持久成立，旧工作者和迟到意图都不能再打开出口。
func (m *Module) internalClosure(tx *durable.Tx, st *opState, reason string) error {
	op := proto.Clone(st.op).(*lernav1.Operation)
	op.Dispatch = lernav1.DispatchState_DISPATCH_STATE_SEALED
	if op.GetSealReason() == "" {
		op.SealReason = reason
	}
	op.Effect = lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED
	op.LateEffect = lernav1.LateEffect_LATE_EFFECT_RULED_OUT
	op.Lifecycle = lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED
	op.ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_DONE
	op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs,
		"internal-closure:"+m.Domain.ID()+"/"+op.GetOperationId()+"@"+strconv.FormatInt(op.GetLedgerRevision()+1, 10))
	if _, err := tx.Exec(`UPDATE attempts SET phase = ? WHERE user_id = ? AND operation_id = ?`,
		int32(lernav1.AttemptPhase_ATTEMPT_PHASE_CLOSED), op.GetUserId(), op.GetOperationId()); err != nil {
		return err
	}
	if err := saveOperation(tx, op); err != nil {
		return err
	}
	if err := tx.SealJob(op.GetUserId(), "execute:"+op.GetOperationId()); err != nil {
		return err
	}
	if err := m.afterSettle(tx, op); err != nil {
		return err
	}
	st.op = op
	return m.notify(tx, op, st)
}
