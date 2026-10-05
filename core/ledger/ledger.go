// Package ledger 实现执行管理（docs/architecture/core/ledger/README.md）：
// 动作意图的接纳、执行尝试、出口记录、效果裁决、迟到可能性、核对和收尾依据。
//
// 执行管理单独成域，与裁决域之间按 R7 交接；同进程部署也不省略持久回执。
package ledger

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
)

// Module 是执行管理模块，绑定执行管理域和固定的执行端点。
type Module struct {
	Domain *durable.Domain
	// Endpoint 是本执行管理负责的执行端点；接纳后固定，工作者替换不得改派。
	Endpoint string
	// AdjudicationDomain 是动作的准入负责方：开始门禁和变化通知的接收域。
	AdjudicationDomain string
	// Gate 是出口闸门。
	Gate *egress.Gate
}

// Register 登记执行管理接受的命令和工作。
func (m *Module) Register() {
	m.Domain.HandleCommand(ports.CommandAcceptIntent,
		func() proto.Message { return &lernav1.OperationIntent{} }, m.handleAcceptIntent)
	m.Domain.HandleCommand(ports.CommandSealDispatch,
		func() proto.Message { return &lernav1.SealDispatchCommand{} }, m.handleSealDispatch)
	m.Domain.HandleCommand(ports.CommandResumeReconciliation,
		func() proto.Message { return &lernav1.ResumeReconciliationCommand{} }, m.handleResume)
	m.Domain.HandleJob(JobExecute, m.execute)
}

// handleAcceptIntent 是 R7 第二步（持久点 6b、出口 P2）：执行管理持久承担该动作的执行和核对责任。
// 接纳不表示已经执行。按原交接标识幂等：重复投递返回原决定，不新建动作。
func (m *Module) handleAcceptIntent(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	it := in.Payload.(*lernav1.OperationIntent)
	if it.GetUserId() != in.UserID() {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "intent user does not match the command")
	}
	if it.GetLedgerDomainId() != m.Domain.ID() || it.GetExecutorEndpointId() != m.Endpoint {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT,
			"intent is assigned to %s/%s, not %s/%s", it.GetLedgerDomainId(), it.GetExecutorEndpointId(), m.Domain.ID(), m.Endpoint)
	}
	if it.GetOperationId() == "" || it.GetAdmissionRef() == "" || it.GetCapability() == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "intent lacks operation, admission or capability")
	}
	if existing, err := loadOperation(tx, it.GetUserId(), it.GetOperationId()); err != nil {
		return durable.Outcome{}, err
	} else if existing != nil {
		// 同一动作只能由一次交接接纳；换交接标识重投同一动作属于内容冲突。
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "operation %s already accepted", it.GetOperationId())
	}
	op := &lernav1.Operation{
		UserId:             it.GetUserId(),
		OperationId:        it.GetOperationId(),
		TaskId:             it.GetTaskId(),
		AdmissionRef:       it.GetAdmissionRef(),
		LedgerDomainId:     m.Domain.ID(),
		ExecutorEndpointId: m.Endpoint,
		AdapterRef:         it.GetAdapterRef(),
		CapabilityId:       it.GetCapability().GetCapabilityId(),
		Parameters:         it.GetParameters(),
		RequestDigest:      it.GetRequestDigest(),
		CapabilitySnapshot: it.GetCapability(),
		Lifecycle:          lernav1.OperationLifecycle_OPERATION_LIFECYCLE_ACCEPTED,
		Dispatch:           lernav1.DispatchState_DISPATCH_STATE_OPEN,
		Effect:             lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED,
		LateEffect:         lernav1.LateEffect_LATE_EFFECT_RULED_OUT,
		ReconcileState:     lernav1.ReconcileState_RECONCILE_STATE_NONE,
		LedgerRevision:     1,
		SendQuota:          it.GetSendQuota(),
		QueryQuota:         it.GetQueryQuota(),
		Origin:             it.GetOrigin(),
		HandoffId:          in.Identity().GetCommandId(),
	}
	// 封闭先于意图被接纳：迟到的意图仍被接纳为责任，但不会被执行。
	var sealReason string
	if err := tx.QueryRow(`SELECT COALESCE(MAX(reason), '') FROM seals WHERE user_id = ? AND operation_id = ?`,
		it.GetUserId(), it.GetOperationId()).Scan(&sealReason); err != nil {
		return durable.Outcome{}, err
	}
	intentBlob, err := proto.MarshalOptions{Deterministic: true}.Marshal(it)
	if err != nil {
		return durable.Outcome{}, err
	}
	if err := insertOperation(tx, op, intentBlob); err != nil {
		return durable.Outcome{}, err
	}
	if sealReason != "" {
		st, err := loadState(tx, op.GetUserId(), op.GetOperationId())
		if err != nil {
			return durable.Outcome{}, err
		}
		if err := m.internalClosure(tx, st, sealReason); err != nil {
			return durable.Outcome{}, err
		}
		return durable.Outcome{Result: &lernav1.AcceptIntentResult{OperationId: op.GetOperationId(), LedgerRevision: st.op.GetLedgerRevision(), Sealed: true}}, nil
	}
	if err := m.afterAccept(tx, op); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.AcceptIntentResult{OperationId: op.GetOperationId(), LedgerRevision: op.GetLedgerRevision()}}, nil
}

func insertOperation(tx *durable.Tx, op *lernav1.Operation, intent []byte) error {
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO operations (user_id, operation_id, task_id, handoff_id, lifecycle, dispatch, effect,
		late_effect, ledger_revision, record, intent, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.GetUserId(), op.GetOperationId(), op.GetTaskId(), op.GetHandoffId(), int32(op.GetLifecycle()), int32(op.GetDispatch()),
		int32(op.GetEffect()), int32(op.GetLateEffect()), op.GetLedgerRevision(), blob, intent, tx.NowMs(), tx.NowMs())
	return err
}

func loadOperation(tx *durable.Tx, user, opID string) (*lernav1.Operation, error) {
	var blob []byte
	err := tx.QueryRow(`SELECT record FROM operations WHERE user_id = ? AND operation_id = ?`, user, opID).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	op := &lernav1.Operation{}
	return op, proto.Unmarshal(blob, op)
}

// Record 返回动作的权威记录。
func (m *Module) Record(ctx context.Context, user, opID string) (*lernav1.OperationRecord, error) {
	r := &lernav1.OperationRecord{}
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		op, err := loadOperation(tx, user, opID)
		if err != nil {
			return err
		}
		if op == nil {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "operation %s not found", opID)
		}
		r.Operation = op
		return nil
	})
	return r, err
}
