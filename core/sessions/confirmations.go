package sessions

import (
	"bytes"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// ConfirmationTTL 是确认事项的有效期；到期在回应和消费时检查，不只依赖后台定时器。
const ConfirmationTTL = 24 * time.Hour

// CreateConfirmation 发布一个确认事项（核心调用）：描述和摘要由核心生成，回应方不得修改。
func CreateConfirmation(tx *durable.Tx, c *lernav1.Confirmation) error {
	if c.GetExpiresAt() == nil {
		c.ExpiresAt = timestamppb.New(tx.Now().Add(ConfirmationTTL))
	}
	c.Status = lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING
	_, err := tx.Exec(`INSERT INTO confirmations (user_id, confirmation_id, session_id, subject_kind, task_id, proposal_id, step_id,
		description, requirements_version, input_version, intent_fingerprint, expires_at, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		c.GetUserId(), c.GetConfirmationId(), c.GetSessionId(), int32(c.GetSubjectKind()), c.GetTaskId(), c.GetProposalId(), c.GetStepId(),
		c.GetDescription(), c.GetRequirementsVersion(), c.GetInputVersion(), c.GetIntentFingerprint(),
		c.GetExpiresAt().AsTime().UnixMilli(), int32(c.GetStatus()), tx.NowMs())
	return err
}

func scanConfirmation(row *sql.Row, user string) (*lernav1.Confirmation, error) {
	c := &lernav1.Confirmation{UserId: user}
	var kind, status int32
	var expires int64
	var responded sql.NullInt64
	var consumedKind, consumedRef string
	err := row.Scan(&c.ConfirmationId, &c.SessionId, &kind, &c.TaskId, &c.ProposalId, &c.StepId, &c.Description,
		&c.RequirementsVersion, &c.InputVersion, &c.IntentFingerprint, &expires, &status, &consumedKind, &consumedRef, &responded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.SubjectKind = lernav1.ConfirmationSubjectKind(kind)
	c.Status = lernav1.ConfirmationStatus(status)
	c.ExpiresAt = timestamppb.New(time.UnixMilli(expires))
	if responded.Valid {
		c.RespondedAt = timestamppb.New(time.UnixMilli(responded.Int64))
	}
	switch consumedKind {
	case "grant":
		c.ConsumedBy = &lernav1.Confirmation_GrantIssuanceRef{GrantIssuanceRef: consumedRef}
	case "admission":
		c.ConsumedBy = &lernav1.Confirmation_AdmissionRef{AdmissionRef: consumedRef}
	}
	return c, nil
}

const confirmationColumns = `confirmation_id, session_id, subject_kind, task_id, proposal_id, step_id, description,
	requirements_version, input_version, intent_fingerprint, expires_at, status, consumed_kind, consumed_ref, responded_at`

// LoadConfirmation 读取一个确认事项；不存在时返回 nil。
func LoadConfirmation(tx *durable.Tx, user, id string) (*lernav1.Confirmation, error) {
	return scanConfirmation(tx.QueryRow(`SELECT `+confirmationColumns+` FROM confirmations WHERE user_id = ? AND confirmation_id = ?`, user, id), user)
}

func setConfirmationStatus(tx *durable.Tx, user, id string, st lernav1.ConfirmationStatus) error {
	_, err := tx.Exec(`UPDATE confirmations SET status = ? WHERE user_id = ? AND confirmation_id = ?`, int32(st), user, id)
	return err
}

// respond 记录用户对确认事项的明确回应。批准不等于已准入或已执行；回应不递增输入版本或控制代次。
// 用户看到的事项摘要必须与核心绑定一致，否则这是对另一件事的回应。
func respond(tx *durable.Tx, user, id string, approve bool, fp []byte) (*lernav1.Confirmation, error) {
	c, err := LoadConfirmation(tx, user, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s not found", id)
	}
	if c.GetStatus() != lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s is already %s", id, c.GetStatus())
	}
	if !tx.Now().Before(c.GetExpiresAt().AsTime()) {
		if err := setConfirmationStatus(tx, user, id, lernav1.ConfirmationStatus_CONFIRMATION_STATUS_EXPIRED); err != nil {
			return nil, err
		}
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s has expired", id)
	}
	if !bytes.Equal(fp, c.GetIntentFingerprint()) {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "the response does not match the item the core asked to confirm")
	}
	st := lernav1.ConfirmationStatus_CONFIRMATION_STATUS_DENIED
	if approve {
		st = lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED
	}
	if _, err := tx.Exec(`UPDATE confirmations SET status = ?, responded_at = ? WHERE user_id = ? AND confirmation_id = ?`,
		int32(st), tx.NowMs(), user, id); err != nil {
		return nil, err
	}
	c.Status = st
	return c, nil
}

// consume 是会话共享的唯一消费逻辑：两条路径（授权签发、动作准入）都调用它，
// 一个确认只能被消费一次；消费目标的类型必须与事项类型一致。只有整个签发或准入事务提交时才生效。
func consume(tx *durable.Tx, c *lernav1.Confirmation, kind lernav1.ConfirmationSubjectKind, fp []byte, ref string) error {
	if c.GetSubjectKind() != kind {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s is for %s, not %s", c.GetConfirmationId(), c.GetSubjectKind(), kind)
	}
	if c.GetStatus() != lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s is %s", c.GetConfirmationId(), c.GetStatus())
	}
	if !tx.Now().Before(c.GetExpiresAt().AsTime()) {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s has expired", c.GetConfirmationId())
	}
	if !bytes.Equal(fp, c.GetIntentFingerprint()) {
		// 参数、条件或主体有任何改变，旧确认就失效。
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s was given for a different item", c.GetConfirmationId())
	}
	consumedKind := "admission"
	if kind == lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_GRANT_ISSUANCE {
		consumedKind = "grant"
	}
	res, err := tx.Exec(`UPDATE confirmations SET status = ?, consumed_kind = ?, consumed_ref = ?
		WHERE user_id = ? AND confirmation_id = ? AND status = ?`,
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_CONSUMED), consumedKind, ref, c.GetUserId(), c.GetConfirmationId(),
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s was already consumed", c.GetConfirmationId())
	}
	return nil
}

// ConsumeForAdmission 在动作准入事务中消费确认（准入-9）：找到为该提议步骤批准的确认，
// 绑定一致才消费。没有已批准的确认时返回 nil。
func ConsumeForAdmission(tx *durable.Tx, user, proposalID, stepID string, fp []byte, admissionRef string) (*lernav1.Confirmation, error) {
	c, err := scanConfirmation(tx.QueryRow(`SELECT `+confirmationColumns+` FROM confirmations
		WHERE user_id = ? AND proposal_id = ? AND step_id = ? AND subject_kind = ? AND status = ?
		ORDER BY created_at DESC LIMIT 1`, user, proposalID, stepID,
		int32(lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION),
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED)), user)
	if err != nil || c == nil {
		return nil, err
	}
	if err := consume(tx, c, lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION, fp, admissionRef); err != nil {
		return nil, err
	}
	return c, nil
}

// ConsumeForGrant 在授权签发事务中消费 GRANT_ISSUANCE 确认。
func ConsumeForGrant(tx *durable.Tx, user, confirmationID string, fp []byte, grantRef string) error {
	c, err := LoadConfirmation(tx, user, confirmationID)
	if err != nil {
		return err
	}
	if c == nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "confirmation %s not found", confirmationID)
	}
	return consume(tx, c, lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_GRANT_ISSUANCE, fp, grantRef)
}

// PendingForProposal 返回为该提议步骤建立的、仍可回应或已批准的确认。
func PendingForProposal(tx *durable.Tx, user, proposalID, stepID string) (*lernav1.Confirmation, error) {
	return scanConfirmation(tx.QueryRow(`SELECT `+confirmationColumns+` FROM confirmations
		WHERE user_id = ? AND proposal_id = ? AND step_id = ? AND status IN (?, ?) ORDER BY created_at DESC LIMIT 1`,
		user, proposalID, stepID, int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING),
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED)), user)
}

// SupersedeForProposal 把提议上尚未消费的动作确认标为被替代：事项已经不是当前依据。
func SupersedeForProposal(tx *durable.Tx, user, proposalID string) error {
	_, err := tx.Exec(`UPDATE confirmations SET status = ? WHERE user_id = ? AND proposal_id = ? AND subject_kind = ? AND status IN (?, ?)`,
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_SUPERSEDED), user, proposalID,
		int32(lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION),
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING), int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED))
	return err
}

func pendingConfirmations(tx *durable.Tx, user, sessionID string) ([]*lernav1.Confirmation, error) {
	rows, err := tx.Query(`SELECT confirmation_id FROM confirmations WHERE user_id = ? AND session_id = ? AND status = ? ORDER BY created_at`,
		user, sessionID, int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var out []*lernav1.Confirmation
	for _, id := range ids {
		c, err := LoadConfirmation(tx, user, id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
