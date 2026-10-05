// Package grants 实现授权（docs/architecture/core/grants/README.md）：
// 许可、使用记录、出口凭据和撤销。属于裁决域，准入检查与任务、会话、预算在同一事务。
//
// M1 只开放单次和持续授权、准入检查、不透明出口凭据和同域撤销；
// 委派、组合多个来源、只读检索凭据返回"不支持"（授权 9）。
package grants

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// SemanticVersion 是 M1 授权表达的语义版本：动作、操作权利、处理目的和资源范围的解释规则。
const SemanticVersion = "grants.m1"

// ActionInvoke 是对能力的调用动作；ActionQuery 是核对查询。
const (
	ActionInvoke = "invoke"
	ActionQuery  = "query"
)

// Module 是授权模块，绑定裁决域。
type Module struct {
	Domain *durable.Domain
	// LedgerDomain 是撤销时封闭请求的接收域。
	LedgerDomain string
}

// Register 登记授权接受的公共命令。
func (m *Module) Register() {
	m.Domain.HandleCommand(ports.CommandIssueGrant,
		func() proto.Message { return &lernav1.IssueGrantCommand{} }, m.handleIssue)
	m.Domain.HandleCommand(ports.CommandRequestGrant,
		func() proto.Message { return &lernav1.RequestGrantCommand{} }, m.handleRequest)
	m.Domain.HandleCommand(ports.CommandRevokeGrant,
		func() proto.Message { return &lernav1.RevokeGrantCommand{} }, m.handleRevoke)
}

// handleIssue 签发授权：许可、回执在裁决域中原子提交。带 GRANT_ISSUANCE 确认时，
// 在签发事务里调用会话的消费逻辑；没有确认时，经过认证的命令本身就是用户事件。
func (m *Module) handleIssue(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.IssueGrantCommand)
	g, err := validateIssue(tx, in.UserID(), cmd)
	if err != nil {
		return durable.Outcome{}, err
	}
	g.IssuerRef = "command/" + in.Identity().GetIssuerId() + "/" + in.Identity().GetCommandId()
	if cmd.GetConfirmationId() != "" {
		if err := sessions.ConsumeForGrant(tx, in.UserID(), cmd.GetConfirmationId(), IssuanceFingerprint(cmd), g.GetGrantId()); err != nil {
			return durable.Outcome{}, err
		}
		g.IssuerRef = "confirmation/" + cmd.GetConfirmationId()
	}
	if err := Insert(tx, g); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.IssueGrantResult{GrantId: g.GetGrantId()}}, nil
}

func validateIssue(tx *durable.Tx, user string, cmd *lernav1.IssueGrantCommand) (*lernav1.Grant, error) {
	if cmd.GetParentGrantRef() != "" {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "grant delegation is not supported in M1")
	}
	if len(cmd.GetCombineFrom()) > 0 {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "combining several grant sources is not supported")
	}
	if len(cmd.GetClauses()) == 0 {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "a grant needs at least one clause")
	}
	for _, c := range cmd.GetClauses() {
		if err := validateClause(c); err != nil {
			return nil, err
		}
	}
	if cmd.GetValidUntil() == nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "a grant needs an explicit valid_until")
	}
	from := tx.Now()
	if cmd.GetValidFrom() != nil {
		from = cmd.GetValidFrom().AsTime()
	}
	if !cmd.GetValidUntil().AsTime().After(from) {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "valid_until must be after valid_from")
	}
	g := &lernav1.Grant{
		UserId:          user,
		GrantId:         ids.New(),
		SubjectRef:      "task-runner/" + user,
		Clauses:         cmd.GetClauses(),
		SemanticVersion: SemanticVersion,
		ValidFrom:       timestamppb.New(from),
		ValidUntil:      cmd.GetValidUntil(),
		UseMode:         cmd.GetUseMode(),
		MaxAdmissions:   cmd.GetMaxAdmissions(),
		Status:          lernav1.GrantStatus_GRANT_STATUS_ACTIVE,
		StateVersion:    1,
	}
	switch g.GetUseMode() {
	case lernav1.UseMode_USE_MODE_SINGLE:
		g.MaxAdmissions = 1
	case lernav1.UseMode_USE_MODE_STANDING:
		if g.GetMaxAdmissions() < 0 {
			return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "max_admissions must not be negative")
		}
	default:
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "use_mode must be SINGLE or STANDING")
	}
	g.UsePoolId = g.GetGrantId()
	return g, nil
}

// validateClause 拒绝未知或 M1 不支持的取值；缺省不解释为无限权限。
func validateClause(c *lernav1.GrantClause) error {
	if c.GetResource() == "" || len(c.GetActions()) == 0 {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "clause needs a resource and explicit actions")
	}
	for _, a := range c.GetActions() {
		if a != ActionInvoke && a != ActionQuery {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown action %q", a)
		}
	}
	if len(c.GetUseRights()) == 0 {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "clause needs explicit use rights")
	}
	for _, r := range c.GetUseRights() {
		switch r {
		case lernav1.UseRight_USE_RIGHT_READ, lernav1.UseRight_USE_RIGHT_SAVE, lernav1.UseRight_USE_RIGHT_SYNC, lernav1.UseRight_USE_RIGHT_ACT:
		default:
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown use right %d", int32(r))
		}
	}
	if len(c.GetProcessingPurposes()) == 0 {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "clause needs explicit processing purposes")
	}
	for _, p := range c.GetProcessingPurposes() {
		switch p {
		case lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK:
		case lernav1.ProcessingPurpose_PROCESSING_PURPOSE_PERSONALIZATION, lernav1.ProcessingPurpose_PROCESSING_PURPOSE_DIAGNOSIS,
			lernav1.ProcessingPurpose_PROCESSING_PURPOSE_EVALUATION, lernav1.ProcessingPurpose_PROCESSING_PURPOSE_IMPROVEMENT:
			return errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "processing purpose %s is not supported in M1", p)
		default:
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown processing purpose %d", int32(p))
		}
	}
	return nil
}

// Insert 保存一份已核验的授权。
func Insert(tx *durable.Tx, g *lernav1.Grant) error {
	blob, err := proto.Marshal(g)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO grants (user_id, grant_id, record, status, revocation_completion, revocation_epoch,
		use_pool_id, state_version, created_at) VALUES (?, ?, ?, ?, ?, 0, ?, 1, ?)`,
		g.GetUserId(), g.GetGrantId(), blob, int32(g.GetStatus()), int32(g.GetRevocationCompletion()), g.GetUsePoolId(), tx.NowMs())
	return err
}

type grantRow struct {
	g          *lernav1.Grant
	status     lernav1.GrantStatus
	completion lernav1.RevocationCompletion
	epoch      int64
}

func loadGrants(tx *durable.Tx, user string) ([]grantRow, error) {
	rows, err := tx.Query(`SELECT record, status, revocation_completion, revocation_epoch FROM grants
		WHERE user_id = ? ORDER BY created_at, grant_id`, user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []grantRow
	for rows.Next() {
		var blob []byte
		var st, comp int32
		var r grantRow
		if err := rows.Scan(&blob, &st, &comp, &r.epoch); err != nil {
			return nil, err
		}
		r.g = &lernav1.Grant{}
		if err := proto.Unmarshal(blob, r.g); err != nil {
			return nil, err
		}
		r.status = lernav1.GrantStatus(st)
		r.completion = lernav1.RevocationCompletion(comp)
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadGrant(tx *durable.Tx, user, grantID string) (*grantRow, error) {
	var blob []byte
	var st, comp int32
	r := grantRow{}
	err := tx.QueryRow(`SELECT record, status, revocation_completion, revocation_epoch FROM grants WHERE user_id = ? AND grant_id = ?`,
		user, grantID).Scan(&blob, &st, &comp, &r.epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.g = &lernav1.Grant{}
	if err := proto.Unmarshal(blob, r.g); err != nil {
		return nil, err
	}
	r.status = lernav1.GrantStatus(st)
	r.completion = lernav1.RevocationCompletion(comp)
	return &r, nil
}

// expired 是计算值：权威时间 ∉ [valid_from, valid_until)。
func expired(g *lernav1.Grant, now time.Time) bool {
	return now.Before(g.GetValidFrom().AsTime()) || !now.Before(g.GetValidUntil().AsTime())
}

func uses(tx *durable.Tx, user, pool string) (int32, error) {
	var n int32
	err := tx.QueryRow(`SELECT COUNT(*) FROM grant_uses WHERE user_id = ? AND use_pool_id = ?`, user, pool).Scan(&n)
	return n, err
}

// UseRequest 描述一次需要授权的动作。
type UseRequest struct {
	User          string
	TaskID        string
	OperationID   string
	AdmissionID   string
	Resource      string
	Action        string
	Params        map[string]string
	RequestDigest []byte
	// GrantID 非空时只使用这份授权（例如确认后签发的单次授权）。
	GrantID string
}

func clauseCovers(c *lernav1.GrantClause, r UseRequest) bool {
	if c.GetResource() != r.Resource {
		return false
	}
	if !contains(c.GetActions(), r.Action) {
		return false
	}
	actOK := false
	for _, u := range c.GetUseRights() {
		if u == lernav1.UseRight_USE_RIGHT_ACT {
			actOK = true
		}
	}
	purposeOK := false
	for _, p := range c.GetProcessingPurposes() {
		if p == lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK {
			purposeOK = true
		}
	}
	if !actOK || !purposeOK {
		return false
	}
	if c.GetTaskId() != "" && c.GetTaskId() != r.TaskID {
		return false
	}
	for k, v := range c.GetParamEquals() {
		if r.Params[k] != v {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Covering 返回当前能覆盖请求的授权（不占用）。只用于判断是否需要用户确认。
func Covering(tx *durable.Tx, r UseRequest) (bool, error) {
	_, err := pick(tx, r)
	if errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) || errs.Is(err, lernav1.ErrorCode_ERROR_CODE_GRANT_REVOKED) {
		return false, nil
	}
	return err == nil, err
}

func pick(tx *durable.Tx, r UseRequest) (*lernav1.Grant, error) {
	all, err := loadGrants(tx, r.User)
	if err != nil {
		return nil, err
	}
	revoked := false
	for _, row := range all {
		if r.GrantID != "" && row.g.GetGrantId() != r.GrantID {
			continue
		}
		covers := false
		for _, c := range row.g.GetClauses() {
			if clauseCovers(c, r) {
				covers = true
				break
			}
		}
		if !covers {
			continue
		}
		if row.status != lernav1.GrantStatus_GRANT_STATUS_ACTIVE {
			revoked = true
			continue
		}
		if expired(row.g, tx.Now()) {
			continue
		}
		if row.g.GetMaxAdmissions() > 0 {
			n, err := uses(tx, r.User, row.g.GetUsePoolId())
			if err != nil {
				return nil, err
			}
			if n >= row.g.GetMaxAdmissions() {
				continue
			}
		}
		return row.g, nil
	}
	if revoked {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_GRANT_REVOKED, "the covering grant for %s was revoked", r.Resource)
	}
	return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "no current grant covers %s %s", r.Action, r.Resource)
}

// OccupyForAdmission 是"核验并占用"（准入-7）：授权链当前有效、覆盖动作、操作权利和处理目的，
// 还有剩余次数；原子建立使用记录。只能由准入事务调用。
func OccupyForAdmission(tx *durable.Tx, r UseRequest) (grantID, useID string, err error) {
	g, err := pick(tx, r)
	if err != nil {
		return "", "", err
	}
	useID = ids.New()
	_, err = tx.Exec(`INSERT INTO grant_uses (user_id, use_id, use_pool_id, grant_id, operation_id, admission_id,
		request_digest, state, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'OCCUPIED', ?)`,
		r.User, useID, g.GetUsePoolId(), g.GetGrantId(), r.OperationID, r.AdmissionID, r.RequestDigest, tx.NowMs())
	if err != nil {
		return "", "", err
	}
	return g.GetGrantId(), useID, nil
}

// IssueCredential 签发出口凭据：不透明引用，绑定准入、主体、出口、请求摘要和期限。
// 签名有效不等于当前有效；每次出口都查核心记录。
func IssueCredential(tx *durable.Tx, user, operationID, grantID, useID, audience string, digest []byte) (string, error) {
	row, err := loadGrant(tx, user, grantID)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "grant %s not found", grantID)
	}
	id := ids.New()
	_, err = tx.Exec(`INSERT INTO credentials (user_id, credential_id, operation_id, grant_id, use_id, audience,
		request_digest, issued_at, expires_at, revocation_epoch) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user, id, operationID, grantID, useID, audience, digest, tx.NowMs(), row.g.GetValidUntil().AsTime().UnixMilli(), row.epoch)
	return id, err
}

// EgressCheck 是"核验出口使用"（开始-3）：存在与该动作精确匹配的使用记录，授权链未撤销、未过期；
// 不再要求剩余次数，否则一次性授权会在出口被自己的占用拒绝。
func EgressCheck(tx *durable.Tx, user, credentialID, operationID, audience string, digest []byte) error {
	var opID, grantID, useID, aud string
	var dg []byte
	var expires, epoch int64
	err := tx.QueryRow(`SELECT operation_id, grant_id, use_id, audience, request_digest, expires_at, revocation_epoch
		FROM credentials WHERE user_id = ? AND credential_id = ?`, user, credentialID).
		Scan(&opID, &grantID, &useID, &aud, &dg, &expires, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "unknown egress credential")
	}
	if err != nil {
		return err
	}
	if opID != operationID || aud != audience || string(dg) != string(digest) {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "egress credential does not match the admitted operation")
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM grant_uses WHERE user_id = ? AND use_id = ? AND operation_id = ?`,
		user, useID, operationID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "no grant use for operation %s", operationID)
	}
	row, err := loadGrant(tx, user, grantID)
	if err != nil {
		return err
	}
	if row == nil || row.status != lernav1.GrantStatus_GRANT_STATUS_ACTIVE || row.epoch != epoch {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_GRANT_REVOKED, "grant %s is revoked", grantID)
	}
	if expired(row.g, tx.Now()) || tx.NowMs() >= expires {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "grant %s has expired", grantID)
	}
	return nil
}
