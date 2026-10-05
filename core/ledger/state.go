package ledger

import (
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// sendRow 是一次物理发送的记录。
type sendRow struct {
	attemptID        string
	seq              int32
	purpose          lernav1.SendPurpose
	claimEpoch       int64
	startReceipt     *lernav1.Receipt
	dispatchPossible bool
	dispatchAt       time.Time
	observed         bool
}

type obsRow struct {
	o       *lernav1.Observation
	report  *lernav1.ExecutionReport
	decided bool
}

// opState 是一个动作在执行管理中的全部权威记录。
type opState struct {
	op       *lernav1.Operation
	intent   *lernav1.OperationIntent
	attempts []*lernav1.Attempt
	sends    []*sendRow
	obs      []*obsRow
}

func (s *opState) attempt() *lernav1.Attempt {
	if len(s.attempts) == 0 {
		return nil
	}
	return s.attempts[len(s.attempts)-1]
}

// lastSend 返回最近一次执行目的的发送。
func (s *opState) lastSend(purpose lernav1.SendPurpose) *sendRow {
	var out *sendRow
	for _, x := range s.sends {
		if x.purpose == purpose {
			out = x
		}
	}
	return out
}

// anyDispatched 报告是否有发送进入"可能已发出"。
func (s *opState) anyDispatched() bool {
	for _, x := range s.sends {
		if x.dispatchPossible && x.purpose == lernav1.SendPurpose_SEND_PURPOSE_EXECUTE {
			return true
		}
	}
	return false
}

func (s *opState) count(purpose lernav1.SendPurpose, dispatchedOnly bool) int32 {
	var n int32
	for _, x := range s.sends {
		if x.purpose == purpose && (!dispatchedOnly || x.dispatchPossible) {
			n++
		}
	}
	return n
}

func loadState(tx *durable.Tx, user, opID string) (*opState, error) {
	op, err := loadOperation(tx, user, opID)
	if err != nil || op == nil {
		return nil, err
	}
	st := &opState{op: op}
	if st.intent, err = loadIntent(tx, user, opID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT attempt_id, attempt_no, external_key, external_key_scope, COALESCE(key_valid_until, 0), phase,
		send_count, COALESCE(first_possible_send_at, 0) FROM attempts WHERE user_id = ? AND operation_id = ? ORDER BY attempt_no`, user, opID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		a := &lernav1.Attempt{OperationId: opID}
		var kv, first int64
		var phase int32
		if err := rows.Scan(&a.AttemptId, &a.AttemptNo, &a.ExternalKey, &a.ExternalKeyScope, &kv, &phase, &a.SendCount, &first); err != nil {
			_ = rows.Close()
			return nil, err
		}
		a.Phase = lernav1.AttemptPhase(phase)
		if kv > 0 {
			a.KeyValidUntil = timestamppb.New(time.UnixMilli(kv))
		}
		if first > 0 {
			a.FirstPossibleSendAt = timestamppb.New(time.UnixMilli(first))
		}
		st.attempts = append(st.attempts, a)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(`SELECT attempt_id, send_seq, purpose, claim_epoch, start_receipt, dispatch_possible,
		COALESCE(dispatch_possible_at, 0), observed FROM sends WHERE user_id = ? AND operation_id = ? ORDER BY created_at, send_seq`, user, opID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		s := &sendRow{}
		var purpose int32
		var rec []byte
		var dp, obs int
		var at int64
		if err := rows.Scan(&s.attemptID, &s.seq, &purpose, &s.claimEpoch, &rec, &dp, &at, &obs); err != nil {
			_ = rows.Close()
			return nil, err
		}
		s.purpose = lernav1.SendPurpose(purpose)
		s.dispatchPossible = dp == 1
		s.observed = obs == 1
		if at > 0 {
			s.dispatchAt = time.UnixMilli(at)
		}
		if len(rec) > 0 {
			s.startReceipt = &lernav1.Receipt{}
			if err := proto.Unmarshal(rec, s.startReceipt); err != nil {
				_ = rows.Close()
				return nil, err
			}
		}
		st.sends = append(st.sends, s)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(`SELECT record, report, decided FROM observations WHERE user_id = ? AND operation_id = ? ORDER BY created_at, observation_id`, user, opID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rec, rep []byte
		var decided int
		if err := rows.Scan(&rec, &rep, &decided); err != nil {
			_ = rows.Close()
			return nil, err
		}
		o := &obsRow{o: &lernav1.Observation{}, report: &lernav1.ExecutionReport{}, decided: decided == 1}
		if err := proto.Unmarshal(rec, o.o); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := proto.Unmarshal(rep, o.report); err != nil {
			_ = rows.Close()
			return nil, err
		}
		st.obs = append(st.obs, o)
	}
	return st, rows.Close()
}

func loadIntent(tx *durable.Tx, user, opID string) (*lernav1.OperationIntent, error) {
	var blob []byte
	if err := tx.QueryRow(`SELECT intent FROM operations WHERE user_id = ? AND operation_id = ?`, user, opID).Scan(&blob); err != nil {
		return nil, err
	}
	it := &lernav1.OperationIntent{}
	return it, proto.Unmarshal(blob, it)
}

// saveOperation 写回动作记录并递增执行管理修订。
func saveOperation(tx *durable.Tx, op *lernav1.Operation) error {
	op.LedgerRevision++
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE operations SET lifecycle = ?, dispatch = ?, effect = ?, late_effect = ?, ledger_revision = ?,
		record = ?, updated_at = ? WHERE user_id = ? AND operation_id = ?`,
		int32(op.GetLifecycle()), int32(op.GetDispatch()), int32(op.GetEffect()), int32(op.GetLateEffect()), op.GetLedgerRevision(),
		blob, tx.NowMs(), op.GetUserId(), op.GetOperationId())
	return err
}

func insertAttempt(tx *durable.Tx, user string, a *lernav1.Attempt) error {
	var kv any
	if a.GetKeyValidUntil() != nil {
		kv = a.GetKeyValidUntil().AsTime().UnixMilli()
	}
	_, err := tx.Exec(`INSERT INTO attempts (user_id, attempt_id, operation_id, attempt_no, external_key, external_key_scope,
		key_valid_until, phase, send_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		user, a.GetAttemptId(), a.GetOperationId(), a.GetAttemptNo(), a.GetExternalKey(), a.GetExternalKeyScope(), kv,
		int32(a.GetPhase()), tx.NowMs())
	return err
}

func insertSend(tx *durable.Tx, user, opID string, s *sendRow) error {
	_, err := tx.Exec(`INSERT INTO sends (user_id, operation_id, attempt_id, send_seq, purpose, claim_epoch, dispatch_possible, observed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0, ?) ON CONFLICT DO NOTHING`,
		user, opID, s.attemptID, s.seq, int32(s.purpose), s.claimEpoch, tx.NowMs())
	return err
}

func loadObservation(tx *durable.Tx, user, id string) (bool, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM observations WHERE user_id = ? AND observation_id = ?`, user, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return n > 0, err
}
