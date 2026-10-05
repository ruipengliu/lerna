package ledger

import (
	"context"
	"errors"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
)

// 执行管理的工作类型。
const (
	JobExecute = "ledger.execute"
)

// errSealed 表示 P5 时派发已经封闭：封闭先于出口开放，不得发送。
var errSealed = errors.New("ledger: dispatch sealed before the egress opened")

func (m *Module) afterAccept(tx *durable.Tx, op *lernav1.Operation) error {
	if op.GetDispatch() != lernav1.DispatchState_DISPATCH_STATE_OPEN {
		return nil
	}
	_, err := tx.EnqueueJob(durable.JobSpec{Kind: JobExecute, User: op.GetUserId(), Subject: op.GetOperationId(),
		PurposeKey: "execute:" + op.GetOperationId()})
	return err
}

func (m *Module) read(ctx context.Context, user, opID string) (*opState, error) {
	var st *opState
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		var err error
		st, err = loadState(tx, user, opID)
		return err
	})
	return st, err
}

// execute 推进一个动作：先读原记录，再决定下一步。接替的工作者只核对原尝试，
// 不会因为重新领取工作就再发一次调用（持久工作 4.5）。
func (m *Module) execute(ctx context.Context, c *durable.Claim) error {
	st, err := m.read(ctx, c.User, c.Subject)
	if err != nil {
		return err
	}
	if st == nil {
		return m.Domain.Advance(ctx, c, "ledger:execute_missing", func(*durable.Tx) (durable.Transition, error) {
			return durable.Block("operation missing"), nil
		})
	}
	// 有尚未裁决的观察：先裁决效果（P8）。
	for _, o := range st.obs {
		if !o.decided {
			return m.decide(ctx, c)
		}
	}
	if st.op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
		return m.Domain.Advance(ctx, c, "ledger:execute_settled", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil })
	}
	last := st.lastSend(lernav1.SendPurpose_SEND_PURPOSE_EXECUTE)
	switch {
	case last != nil && last.dispatchPossible && !last.observed:
		// P5 之后崩溃：可能已发出，按未知恢复，不得当作未执行。
		return m.decide(ctx, c)
	case st.op.GetDispatch() == lernav1.DispatchState_DISPATCH_STATE_SEALED:
		return m.closeIfNeverDispatched(ctx, c, "dispatch sealed")
	case last == nil:
		if err := m.prepareAttempt(ctx, c, st); err != nil {
			return err
		}
		if st, err = m.read(ctx, c.User, c.Subject); err != nil {
			return err
		}
		last = st.lastSend(lernav1.SendPurpose_SEND_PURPOSE_EXECUTE)
	case last.observed:
		// 最近一次发送已有回报且已裁决：交给恢复策略。
		return m.decide(ctx, c)
	}
	return m.send(ctx, c, st, last)
}

// prepareAttempt 是 P3：固定尝试标识、外部键、作用范围和本次发送身份，必须早于第一次出口。
func (m *Module) prepareAttempt(ctx context.Context, c *durable.Claim, st *opState) error {
	return m.Domain.Advance(ctx, c, "ledger:p3_attempt", func(tx *durable.Tx) (durable.Transition, error) {
		decl := st.op.GetCapabilitySnapshot()
		a := &lernav1.Attempt{
			OperationId: st.op.GetOperationId(),
			AttemptId:   ids.New(),
			AttemptNo:   1,
			Phase:       lernav1.AttemptPhase_ATTEMPT_PHASE_REGISTERED,
		}
		// 外部键绑定在执行管理固定的尝试标识上，不绑定领取代次、工作者或重试次数。
		a.ExternalKey = "lerna-" + a.GetAttemptId()
		a.ExternalKeyScope = decl.GetIdempotency().GetKeyScope()
		if decl.GetIdempotency().GetSupported() && decl.GetIdempotency().GetKeyValiditySeconds() > 0 {
			a.KeyValidUntil = timestamppb.New(tx.Now().Add(time.Duration(decl.GetIdempotency().GetKeyValiditySeconds()) * time.Second))
		}
		if err := insertAttempt(tx, c.User, a); err != nil {
			return durable.Transition{}, err
		}
		if err := insertSend(tx, c.User, st.op.GetOperationId(), &sendRow{
			attemptID: a.GetAttemptId(), seq: 1, purpose: lernav1.SendPurpose_SEND_PURPOSE_EXECUTE, claimEpoch: c.Epoch,
		}); err != nil {
			return durable.Transition{}, err
		}
		op := st.op
		op.AttemptIds = append(op.AttemptIds, a.GetAttemptId())
		return durable.Keep(), saveOperation(tx, op)
	})
}

// send 走受控出口：P4 开始门禁 → P5 → 一次实际 I/O → P6，然后裁决效果（P8）。
func (m *Module) send(ctx context.Context, c *durable.Claim, st *opState, s *sendRow) error {
	a := st.attempt()
	safeResend := st.anyDispatched()
	out := m.gateSend(st, a, s, safeResend)
	err := m.Gate.Call(ctx, out, egress.Hooks{
		DispatchPossible: func(ctx context.Context, rec *lernav1.Receipt) error { return m.dispatchPossible(ctx, c, st, s, rec) },
		Observe: func(ctx context.Context, rep *lernav1.ExecutionReport, ioErr error) error {
			return m.observe(ctx, st.op, s, rep, ioErr)
		},
	})
	var rej *egress.ErrStartRejected
	switch {
	case errors.As(err, &rej):
		if safeResend {
			// 安全重发被开始门禁拒绝：原发送仍可能生效，保持未知。
			return m.decide(ctx, c)
		}
		return m.closeIfNeverDispatched(ctx, c, "start gate rejected: "+rej.Rejection.GetCode().String())
	case errors.Is(err, errSealed):
		return m.closeIfNeverDispatched(ctx, c, "sealed before the egress opened")
	case err != nil:
		return err
	}
	return m.decide(ctx, c)
}

func (m *Module) gateSend(st *opState, a *lernav1.Attempt, s *sendRow, safeResend bool) egress.Send {
	op, it := st.op, st.intent
	return egress.Send{
		Issuer:             m.Domain.ID(),
		AdjudicationDomain: m.AdjudicationDomain,
		AdapterID:          op.GetAdapterRef(),
		Start: &lernav1.StartSendCommand{
			UserId:             op.GetUserId(),
			TaskId:             op.GetTaskId(),
			OperationId:        op.GetOperationId(),
			AttemptId:          a.GetAttemptId(),
			SendSeq:            s.seq,
			Purpose:            s.purpose,
			CredentialRef:      it.GetCredentialRef(),
			RequestDigest:      op.GetRequestDigest(),
			ExecutorEndpointId: op.GetExecutorEndpointId(),
			CapabilityId:       op.GetCapabilityId(),
			SafeResend:         safeResend,
		},
		Execute: &lernav1.ExecuteRequest{
			UserId:        op.GetUserId(),
			TaskId:        op.GetTaskId(),
			OperationId:   op.GetOperationId(),
			AttemptId:     a.GetAttemptId(),
			SendSeq:       s.seq,
			CapabilityId:  op.GetCapabilityId(),
			Arguments:     op.GetParameters(),
			ExternalKey:   a.GetExternalKey(),
			CredentialRef: it.GetCredentialRef(),
			RequestDigest: op.GetRequestDigest(),
		},
	}
}

// dispatchPossible 是 P5：检查派发未封闭，在实际 I/O 之前持久写下"可能已发出"。
// 效果转为 UNKNOWN，迟到可能性转为 MAY_OCCUR；提交之后才允许发生 I/O。
func (m *Module) dispatchPossible(ctx context.Context, c *durable.Claim, st *opState, s *sendRow, rec *lernav1.Receipt) error {
	return m.Domain.Advance(ctx, c, "ledger:p5_dispatch_possible", func(tx *durable.Tx) (durable.Transition, error) {
		op, err := loadOperation(tx, c.User, st.op.GetOperationId())
		if err != nil {
			return durable.Transition{}, err
		}
		if op.GetDispatch() == lernav1.DispatchState_DISPATCH_STATE_SEALED {
			return durable.Transition{}, errSealed
		}
		blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(rec)
		if err != nil {
			return durable.Transition{}, err
		}
		if _, err := tx.Exec(`UPDATE sends SET dispatch_possible = 1, dispatch_possible_at = ?, start_receipt = ?
			WHERE user_id = ? AND attempt_id = ? AND send_seq = ? AND purpose = ?`,
			tx.NowMs(), blob, c.User, s.attemptID, s.seq, int32(s.purpose)); err != nil {
			return durable.Transition{}, err
		}
		if s.purpose == lernav1.SendPurpose_SEND_PURPOSE_EXECUTE {
			if _, err := tx.Exec(`UPDATE attempts SET phase = ?, send_count = send_count + 1,
				first_possible_send_at = COALESCE(first_possible_send_at, ?) WHERE user_id = ? AND attempt_id = ?`,
				int32(lernav1.AttemptPhase_ATTEMPT_PHASE_DISPATCH_POSSIBLE), tx.NowMs(), c.User, s.attemptID); err != nil {
				return durable.Transition{}, err
			}
			if op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED && !op.GetEvidenceConflict() {
				op.Effect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
			}
			op.LateEffect = lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR
		}
		op.Lifecycle = lernav1.OperationLifecycle_OPERATION_LIFECYCLE_ACTIVE
		if err := saveOperation(tx, op); err != nil {
			return durable.Transition{}, err
		}
		st.op = op
		s.dispatchPossible = true
		return durable.Keep(), nil
	})
}

// observe 是 P6：保存可信出口在实际 I/O 处固定的原始观察和适配器的回报。
// 观察是需要核验的事实输入，按观察标识幂等，不因领取过期而丢弃，所以不受领取代次约束。
func (m *Module) observe(ctx context.Context, op *lernav1.Operation, s *sendRow, rep *lernav1.ExecutionReport, ioErr error) error {
	obsID := s.attemptID + ":" + s.purpose.String() + ":" + strconv.Itoa(int(s.seq))
	if rep == nil {
		rep = &lernav1.ExecutionReport{
			Status:        lernav1.ExecutionStatus_EXECUTION_STATUS_UNKNOWN,
			ClaimedEffect: lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN,
		}
	}
	if ioErr != nil {
		rep = proto.Clone(rep).(*lernav1.ExecutionReport)
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_UNKNOWN
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
		rep.ClaimsTerminal = false
		rep.Diagnostic = ioErr.Error()
	}
	return m.Domain.Write(ctx, "ledger:p6_observe", func(tx *durable.Tx) error {
		if seen, err := loadObservation(tx, op.GetUserId(), obsID); err != nil || seen {
			return err
		}
		o := &lernav1.Observation{
			ObservationId:   obsID,
			OperationId:     op.GetOperationId(),
			AttemptId:       s.attemptID,
			SendSeq:         s.seq,
			Purpose:         s.purpose,
			Source:          lernav1.ObservationSource_OBSERVATION_SOURCE_RAW,
			ProtocolResult:  rep.GetProtocolResult(),
			ClaimedEffect:   rep.GetClaimedEffect(),
			ClaimsTerminal:  rep.GetClaimsTerminal(),
			EvidenceRef:     rep.GetTargetReceipt(),
			ResourceVersion: rep.GetResourceVersion(),
			ObservedAt:      timestamppb.New(tx.Now()),
			AdapterVersion:  rep.GetAdapterVersion(),
			Fields:          rep.GetFields(),
		}
		rec, err := proto.MarshalOptions{Deterministic: true}.Marshal(o)
		if err != nil {
			return err
		}
		repBlob, err := proto.MarshalOptions{Deterministic: true}.Marshal(rep)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO observations (user_id, observation_id, operation_id, attempt_id, send_seq, purpose,
			record, report, decided, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
			op.GetUserId(), obsID, op.GetOperationId(), s.attemptID, s.seq, int32(s.purpose), rec, repBlob, tx.NowMs()); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE sends SET observed = 1 WHERE user_id = ? AND attempt_id = ? AND send_seq = ? AND purpose = ?`,
			op.GetUserId(), s.attemptID, s.seq, int32(s.purpose)); err != nil {
			return err
		}
		return m.afterObserve(tx, op, s, o, rep)
	})
}

// closeIfNeverDispatched 走内部封闭证明：没有任何发送进入"可能已发出"时，原执行管理负责方
// 封闭派发，效果为 NOT_APPLIED、迟到可能性为 RULED_OUT。有发送可能已发生时只能核对。
func (m *Module) closeIfNeverDispatched(ctx context.Context, c *durable.Claim, reason string) error {
	return m.Domain.Advance(ctx, c, "ledger:internal_closure", func(tx *durable.Tx) (durable.Transition, error) {
		st, err := loadState(tx, c.User, c.Subject)
		if err != nil {
			return durable.Transition{}, err
		}
		if st.op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
			return durable.Done(), nil
		}
		if st.anyDispatched() {
			return durable.Transition{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVARIANT_VIOLATION,
				"operation %s may have been dispatched; internal closure is not allowed", c.Subject)
		}
		return durable.Done(), m.internalClosure(tx, st, reason)
	})
}

// notify 把动作的变化通知任务编排（R7）：通知按执行管理修订接纳，回执丢失时查询原通知。
func (m *Module) notify(tx *durable.Tx, op *lernav1.Operation, st *opState) error {
	u := &lernav1.OperationUpdate{
		UserId:         op.GetUserId(),
		TaskId:         op.GetTaskId(),
		OperationId:    op.GetOperationId(),
		LedgerRevision: op.GetLedgerRevision(),
		Effect:         op.GetEffect(),
		LateEffect:     op.GetLateEffect(),
		Dispatch:       op.GetDispatch(),
		Settled:        op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED,
		ReconcileState: op.GetReconcileState(),
		PauseReason:    op.GetPauseReason(),
	}
	if st != nil {
		u.Started = st.anyDispatched()
		for _, o := range st.obs {
			if o.o.GetClaimedEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED || o.o.GetClaimsTerminal() {
				u.EvidenceRefs = append(u.EvidenceRefs, o.o.GetObservationId())
			}
			for k, v := range o.o.GetFields() {
				if u.Observed == nil {
					u.Observed = map[string]string{}
				}
				u.Observed[k] = v
			}
			if o.report.GetBody() != "" {
				u.Output = o.report.GetBody()
			}
		}
	}
	return tx.EnqueueHandoff(durable.Handoff{
		ID:        "update:" + op.GetOperationId() + ":" + strconv.FormatInt(op.GetLedgerRevision(), 10),
		User:      op.GetUserId(),
		Target:    m.AdjudicationDomain,
		Kind:      ports.CommandOperationUpdate,
		Payload:   u,
		IntentRef: op.GetOperationId(),
	})
}
