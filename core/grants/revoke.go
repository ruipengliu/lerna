package grants

import (
	"context"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	"github.com/ruipengliu/lerna/contracts/fingerprint"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// IssuanceFingerprint 是授权确认绑定的规范化对象摘要：授权表达本身，不带条件集版本。
func IssuanceFingerprint(cmd *lernav1.IssueGrantCommand) []byte {
	c := proto.Clone(cmd).(*lernav1.IssueGrantCommand)
	c.ConfirmationId = ""
	return fingerprint.MustOf(lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_GRANT_ISSUANCE.String(), c)
}

func describeGrant(cmd *lernav1.IssueGrantCommand) string {
	s := "签发授权（" + cmd.GetUseMode().String() + "）："
	for i, c := range cmd.GetClauses() {
		if i > 0 {
			s += "；"
		}
		s += c.GetResource() + " " + strconvList(c.GetActions())
		if len(c.GetParamEquals()) > 0 {
			s += " 参数限于 " + strconvMap(c.GetParamEquals())
		}
		if c.GetTaskId() != "" {
			s += " 仅任务 " + c.GetTaskId()
		}
	}
	if cmd.GetValidUntil() != nil {
		s += "，有效至 " + cmd.GetValidUntil().AsTime().UTC().Format("2006-01-02 15:04 MST")
	}
	return s
}

func strconvList(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "/"
		}
		out += x
	}
	return out
}

func strconvMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k + "=" + strconv.Quote(m[k])
	}
	return out
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// handleRequest 请求签发授权：先核验授权表达，再由核心生成描述和摘要，发布 GRANT_ISSUANCE 确认。
func (m *Module) handleRequest(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.RequestGrantCommand)
	if _, err := validateIssue(tx, in.UserID(), cmd.GetGrant()); err != nil {
		return durable.Outcome{}, err
	}
	if cmd.GetSessionId() == "" {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "grant confirmation needs a session")
	}
	c := &lernav1.Confirmation{
		UserId:            in.UserID(),
		ConfirmationId:    ids.New(),
		SessionId:         cmd.GetSessionId(),
		SubjectKind:       lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_GRANT_ISSUANCE,
		Description:       describeGrant(cmd.GetGrant()),
		IntentFingerprint: IssuanceFingerprint(cmd.GetGrant()),
	}
	if err := sessions.CreateConfirmation(tx, c); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.RequestGrantResult{
		ConfirmationId: c.GetConfirmationId(), Description: c.GetDescription(), IntentFingerprint: c.GetIntentFingerprint(),
	}}, nil
}

// handleRevoke 接纳撤销：一经写入，依赖它的新准入和新凭据立即被拒绝；按 R7 请求相关执行管理
// 封闭尚未开始的执行。各执行端点确认停止后撤销才算完成，期间显示"撤销处理中"。
func (m *Module) handleRevoke(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.RevokeGrantCommand)
	user := in.UserID()
	row, err := loadGrant(tx, user, cmd.GetGrantId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if row == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "grant %s not found", cmd.GetGrantId())
	}
	if row.status != lernav1.GrantStatus_GRANT_STATUS_REVOKED {
		// 任何时候都可以撤销，包括次数已用完、已过期的授权。
		if _, err := tx.Exec(`UPDATE grants SET status = ?, revocation_completion = ?, revocation_epoch = revocation_epoch + 1,
			state_version = state_version + 1 WHERE user_id = ? AND grant_id = ?`,
			int32(lernav1.GrantStatus_GRANT_STATUS_REVOKED), int32(lernav1.RevocationCompletion_REVOCATION_COMPLETION_PENDING),
			user, cmd.GetGrantId()); err != nil {
			return durable.Outcome{}, err
		}
		open, err := openOperations(tx, user, cmd.GetGrantId())
		if err != nil {
			return durable.Outcome{}, err
		}
		byTask := map[string][]string{}
		var tasks []string
		for _, o := range open {
			if _, ok := byTask[o.task]; !ok {
				tasks = append(tasks, o.task)
			}
			byTask[o.task] = append(byTask[o.task], o.op)
		}
		for _, t := range tasks {
			if err := tx.EnqueueHandoff(durable.Handoff{
				ID:     "seal:revoke:" + cmd.GetGrantId() + ":" + t,
				User:   user,
				Target: m.LedgerDomain,
				Kind:   ports.CommandSealDispatch,
				Payload: &lernav1.SealDispatchCommand{UserId: user, TaskId: t, OperationIds: byTask[t],
					Reason: "grant " + cmd.GetGrantId() + " revoked"},
				IntentRef: "revoke:" + cmd.GetGrantId(),
			}); err != nil {
				return durable.Outcome{}, err
			}
		}
		if err := RefreshRevocations(tx, user); err != nil {
			return durable.Outcome{}, err
		}
	}
	p, err := Progress(tx, user, cmd.GetGrantId())
	if err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: p}, nil
}

type openOp struct{ op, task string }

// openOperations 返回使用该授权、派发尚未封闭的动作：它们仍可能开始新的发送。
func openOperations(tx *durable.Tx, user, grantID string) ([]openOp, error) {
	rows, err := tx.Query(`SELECT u.operation_id, o.task_id, o.view FROM grant_uses u
		JOIN task_operations o ON o.user_id = u.user_id AND o.operation_id = u.operation_id
		WHERE u.user_id = ? AND u.grant_id = ? ORDER BY u.created_at`, user, grantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []openOp
	for rows.Next() {
		var o openOp
		var blob []byte
		if err := rows.Scan(&o.op, &o.task, &blob); err != nil {
			return nil, err
		}
		v := &lernav1.OperationView{}
		if err := proto.Unmarshal(blob, v); err != nil {
			return nil, err
		}
		if v.GetSettled() || v.GetDispatch() == lernav1.DispatchState_DISPATCH_STATE_SEALED {
			continue
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Progress 返回撤销进度：被接纳 ≠ 完全生效。
func Progress(tx *durable.Tx, user, grantID string) (*lernav1.RevocationProgress, error) {
	row, err := loadGrant(tx, user, grantID)
	if err != nil || row == nil {
		return nil, err
	}
	p := &lernav1.RevocationProgress{GrantId: grantID, Status: row.status, Completion: row.completion}
	if row.status == lernav1.GrantStatus_GRANT_STATUS_REVOKED {
		open, err := openOperations(tx, user, grantID)
		if err != nil {
			return nil, err
		}
		for _, o := range open {
			p.OpenOperations = append(p.OpenOperations, o.op)
		}
	}
	return p, nil
}

// RefreshRevocations 在各执行端点确认封闭之后把撤销标为完成：没有任何仍可能开始的出口。
func RefreshRevocations(tx *durable.Tx, user string) error {
	rows, err := tx.Query(`SELECT grant_id FROM grants WHERE user_id = ? AND status = ? AND revocation_completion = ?`,
		user, int32(lernav1.GrantStatus_GRANT_STATUS_REVOKED), int32(lernav1.RevocationCompletion_REVOCATION_COMPLETION_PENDING))
	if err != nil {
		return err
	}
	var pending []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		pending = append(pending, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range pending {
		open, err := openOperations(tx, user, id)
		if err != nil {
			return err
		}
		if len(open) == 0 {
			if _, err := tx.Exec(`UPDATE grants SET revocation_completion = ?, state_version = state_version + 1 WHERE user_id = ? AND grant_id = ?`,
				int32(lernav1.RevocationCompletion_REVOCATION_COMPLETION_COMPLETE), user, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// View 返回授权及撤销进度。
func (m *Module) View(ctx context.Context, user, grantID string) (*lernav1.GrantView, error) {
	v := &lernav1.GrantView{}
	err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		row, err := loadGrant(tx, user, grantID)
		if err != nil {
			return err
		}
		if row == nil {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "grant %s not found", grantID)
		}
		g := proto.Clone(row.g).(*lernav1.Grant)
		g.Status = row.status
		g.RevocationCompletion = row.completion
		g.RevocationEpoch = row.epoch
		g.Expired = expired(g, tx.Now())
		if g.Uses, err = uses(tx, user, g.GetUsePoolId()); err != nil {
			return err
		}
		v.Grant = g
		v.Revocation, err = Progress(tx, user, grantID)
		return err
	})
	return v, err
}
