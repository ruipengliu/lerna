package durable

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	"github.com/ruipengliu/lerna/contracts/fingerprint"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// Incoming 是一条已校验的命令：信封和按命令类型解码的正文。
type Incoming struct {
	Envelope *lernav1.CommandEnvelope
	Payload  proto.Message
}

// Identity 返回命令身份。
func (in Incoming) Identity() *lernav1.CommandIdentity { return in.Envelope.GetIdentity() }

// UserID 返回命令所属用户。
func (in Incoming) UserID() string { return in.Envelope.GetIdentity().GetUserId() }

// Outcome 是命令被接受时写入决定回执的内容。
type Outcome struct {
	Refs   []*lernav1.Ref
	Result proto.Message
}

// CommandHandler 由事实的所属模块实现：只在事务内修改核心状态，没有事务外副作用。
//
// 返回 PERMANENT 类错误表示确定的业务拒绝：业务修改回滚，拒绝作为决定持久保存。
// 返回其他错误时整个事务回滚，不写决定。
type CommandHandler func(ctx context.Context, tx *Tx, in Incoming) (Outcome, error)

type commandEntry struct {
	newPayload func() proto.Message
	handle     CommandHandler
}

// HandleCommand 登记本域接受的命令类型。命令类型来自核心固定的集合。
func (d *Domain) HandleCommand(kind string, newPayload func() proto.Message, h CommandHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands[kind] = commandEntry{newPayload: newPayload, handle: h}
}

// NewEnvelope 构造命令信封并按当前指纹版本计算指纹。
func NewEnvelope(id *lernav1.CommandIdentity, kind string, payload proto.Message) (*lernav1.CommandEnvelope, error) {
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "marshal %s: %v", kind, err)
	}
	fp, err := fingerprint.Of(kind, payload)
	if err != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "fingerprint %s: %v", kind, err)
	}
	return &lernav1.CommandEnvelope{
		Identity:           proto.Clone(id).(*lernav1.CommandIdentity),
		CommandKind:        kind,
		ContractVersion:    ports.ContractVersion,
		SchemaId:           string(payload.ProtoReflect().Descriptor().FullName()),
		Payload:            body,
		FingerprintVersion: fingerprint.Version,
		Fingerprint:        fp,
	}, nil
}

func validIdentity(id *lernav1.CommandIdentity) bool {
	return id.GetUserId() != "" && id.GetIssuerId() != "" && id.GetTargetDomainId() != "" && id.GetCommandId() != ""
}

// Execute 执行发给本域的命令（持久工作 4.1）。
//
// 同一 CommandIdentity：指纹相同返回原决定，不同返回 IDEMPOTENCY_CONFLICT。
// 首次命令由所属模块裁决，原决定回执与业务修改、待办工作和交接在一次提交中持久保存。
// 无法解析、版本不支持等不是业务拒绝，不写决定。
func (d *Domain) Execute(ctx context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error) {
	id := env.GetIdentity()
	if !validIdentity(id) {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "incomplete command identity")
	}
	if id.GetTargetDomainId() != d.id {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "command for %s delivered to %s", id.GetTargetDomainId(), d.id)
	}
	if env.GetContractVersion() != ports.ContractVersion {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_CONTRACT, "contract %q", env.GetContractVersion())
	}
	if len(env.GetMustUnderstand()) > 0 {
		// M1 不定义任何必需特性；不理解的必需语义一律拒绝（核心契约 7.6）。
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_CONTRACT, "must_understand %v", env.GetMustUnderstand())
	}
	if env.GetFingerprintVersion() != fingerprint.Version {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_CONTRACT, "fingerprint version %q", env.GetFingerprintVersion())
	}
	d.mu.RLock()
	entry, ok := d.commands[env.GetCommandKind()]
	d.mu.RUnlock()
	if !ok {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "command kind %q not handled by %s", env.GetCommandKind(), d.id)
	}
	payload := entry.newPayload()
	if err := proto.Unmarshal(env.GetPayload(), payload); err != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "decode %s: %v", env.GetCommandKind(), err)
	}
	fp, err := fingerprint.Of(env.GetCommandKind(), payload)
	if err != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "%v", err)
	}
	if !fingerprint.Equal(fp, env.GetFingerprint()) {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "fingerprint does not match payload")
	}

	// 快速路径：已有决定时不进入写事务。
	if prior, err := d.lookupReceipt(ctx, id); err == nil && prior != nil {
		return sameOrConflict(prior, env)
	}

	var receipt *lernav1.Receipt
	label := "cmd:" + env.GetCommandKind()
	err = d.Write(ctx, label, func(tx *Tx) error {
		prior, err := readReceipt(tx, id)
		if err != nil {
			return err
		}
		if prior != nil {
			receipt, err = sameOrConflict(prior, env)
			return err
		}
		if _, err := tx.Exec("SAVEPOINT business"); err != nil {
			return err
		}
		out, herr := entry.handle(ctx, tx, Incoming{Envelope: env, Payload: payload})
		r := &lernav1.Receipt{
			Identity:            id,
			CommandKind:         env.GetCommandKind(),
			FingerprintVersion:  env.GetFingerprintVersion(),
			Fingerprint:         env.GetFingerprint(),
			Phase:               lernav1.CommandPhase_COMMAND_PHASE_DECIDED,
			ResponsibleDomainId: d.id,
			DecidedAt:           timestamppb.New(tx.Now()),
		}
		switch {
		case herr == nil:
			if _, err := tx.Exec("RELEASE business"); err != nil {
				return err
			}
			r.Decision = lernav1.Decision_DECISION_ACCEPTED
			r.ResultRefs = out.Refs
			if out.Result != nil {
				b, err := proto.MarshalOptions{Deterministic: true}.Marshal(out.Result)
				if err != nil {
					return err
				}
				r.Result = b
			}
		case errs.IsPermanent(herr):
			// 确定的业务拒绝：不留下部分修改，拒绝作为决定保存。
			if _, err := tx.Exec("ROLLBACK TO business"); err != nil {
				return err
			}
			if _, err := tx.Exec("RELEASE business"); err != nil {
				return err
			}
			r.Decision = lernav1.Decision_DECISION_REJECTED
			r.Rejection = errs.Proto(herr)
		default:
			return herr
		}
		if err := insertReceipt(tx, r); err != nil {
			return err
		}
		receipt = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

func sameOrConflict(prior *lernav1.Receipt, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error) {
	if prior.GetCommandKind() != env.GetCommandKind() ||
		prior.GetFingerprintVersion() != env.GetFingerprintVersion() ||
		!fingerprint.Equal(prior.GetFingerprint(), env.GetFingerprint()) {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT,
			"command %s already decided with different content", env.GetIdentity().GetCommandId())
	}
	return prior, nil
}

func (d *Domain) lookupReceipt(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.Receipt, error) {
	var r *lernav1.Receipt
	err := d.Read(ctx, func(tx *Tx) error {
		var err error
		r, err = readReceipt(tx, id)
		return err
	})
	return r, err
}

func readReceipt(tx *Tx, id *lernav1.CommandIdentity) (*lernav1.Receipt, error) {
	var blob []byte
	err := tx.QueryRow(`SELECT receipt FROM command_receipts
		WHERE user_id = ? AND issuer_id = ? AND target_domain_id = ? AND command_id = ?`,
		id.GetUserId(), id.GetIssuerId(), id.GetTargetDomainId(), id.GetCommandId()).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &lernav1.Receipt{}
	if err := proto.Unmarshal(blob, r); err != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_DATA_LOSS, "receipt decode: %v", err)
	}
	return r, nil
}

func insertReceipt(tx *Tx, r *lernav1.Receipt) error {
	var pos int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(commit_position), 0) + 1 FROM command_receipts`).Scan(&pos); err != nil {
		return err
	}
	r.CommitPosition = pos
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
	if err != nil {
		return err
	}
	id := r.GetIdentity()
	_, err = tx.Exec(`INSERT INTO command_receipts
		(user_id, issuer_id, target_domain_id, command_id, command_kind, fingerprint_version, fingerprint,
		 phase, decision, commit_position, decided_at, receipt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.GetUserId(), id.GetIssuerId(), id.GetTargetDomainId(), id.GetCommandId(), r.GetCommandKind(),
		r.GetFingerprintVersion(), r.GetFingerprint(), int32(r.GetPhase()), int32(r.GetDecision()),
		pos, tx.NowMs(), blob)
	return err
}

// QueryReceipt 用原命令身份查询回执（持久工作 3）。只读，不触发重试。
// NOT_FOUND 不得当作"没提交"的证明；存储不可用时返回 UNAVAILABLE。
func (d *Domain) QueryReceipt(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error) {
	resp := &lernav1.ReceiptQueryResponse{ResponsibleDomainId: d.id}
	if id.GetTargetDomainId() != d.id {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "query for %s sent to %s", id.GetTargetDomainId(), d.id)
	}
	err := d.Read(ctx, func(tx *Tx) error {
		if err := tx.QueryRow(`SELECT COALESCE(MAX(commit_position), 0) FROM command_receipts`).Scan(&resp.ReadPosition); err != nil {
			return err
		}
		r, err := readReceipt(tx, id)
		if err != nil {
			return err
		}
		switch {
		case r == nil:
			resp.Outcome = lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND
		case r.GetPhase() == lernav1.CommandPhase_COMMAND_PHASE_SUBMITTED:
			resp.Outcome = lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_SUBMITTED
			resp.Receipt = r
		default:
			resp.Outcome = lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED
			resp.Receipt = r
		}
		return nil
	})
	if err != nil {
		return &lernav1.ReceiptQueryResponse{
			ResponsibleDomainId: d.id,
			Outcome:             lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_UNAVAILABLE,
		}, nil
	}
	return resp, nil
}

// ResultOf 解码决定回执中的结果正文。
func ResultOf(r *lernav1.Receipt, into proto.Message) error {
	if r == nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "nil receipt")
	}
	return proto.Unmarshal(r.GetResult(), into)
}
