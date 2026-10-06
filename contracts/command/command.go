// Package command 提供版本固定的命令校验和语义指纹。
package command

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// Failure 保留完整公共错误，不能只传递字符串。
type Failure struct{ Detail *v1.ContractError }

func (e *Failure) Error() string { return e.Detail.Code }

// Fail 按固定错误码分类；没有原回执依据时，暂时失败或提交结果未知不得声明已拒绝。
func Fail(code string) error {
	category := v1.ErrorCategory_ERROR_CATEGORY_PERMANENT
	acceptance := v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED
	recovery := "CORRECT_REQUEST"
	switch code {
	case "AUTHORITY_UNREACHABLE", "DEPENDENCY_UNAVAILABLE", "RATE_LIMITED":
		category = v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT
	case "DEADLINE_EXCEEDED", "TRANSPORT_LOST":
		category = v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE
	}
	if category != v1.ErrorCategory_ERROR_CATEGORY_PERMANENT {
		acceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN
		recovery = "QUERY_OR_RETRY_ORIGINAL"
	}
	return &Failure{Detail: &v1.ContractError{Code: code, Category: category, CommandAcceptance: acceptance, RecoveryAction: recovery}}
}
func CheckCaller(c *v1.Caller, user string) error {
	if c == nil || c.UserId != user || c.IssuerId == "" {
		return Fail("PERMISSION_DENIED")
	}
	return nil
}
func CheckIdentity(c *v1.Caller, id *v1.CommandIdentity, user, domain string) error {
	if err := CheckCaller(c, user); err != nil {
		return err
	}
	if id == nil || id.CommandId == "" {
		return Fail("INVALID_INPUT")
	}
	if id.UserId != user || id.IssuerId != c.IssuerId || id.TargetDomainId != domain {
		return Fail("PERMISSION_DENIED")
	}
	return nil
}
func CheckName(c *v1.Caller, n *v1.GlobalName, user, domain, kind string) error {
	if err := CheckCaller(c, user); err != nil {
		return err
	}
	if n == nil || n.LocalId == "" {
		return Fail("INVALID_INPUT")
	}
	if n.UserId != user || n.AuthorityDomainId != domain || n.ObjectKind != kind {
		return Fail("PERMISSION_DENIED")
	}
	return nil
}
func ValidateGoal(c *v1.SubmitGoalCommand) error {
	if c == nil || c.Identity == nil || c.Goal == "" || len(c.Goal) > 65536 {
		return Fail("INVALID_INPUT")
	}
	if c.ContractVersion != 1 || c.SchemaId != "lerna.v1.SubmitGoal" || c.FingerprintVersion != 1 {
		return Fail("UNSUPPORTED_CONTRACT")
	}
	if len(c.MustUnderstand) > 0 || len(c.ProtoReflect().GetUnknown()) > 0 || len(c.Identity.ProtoReflect().GetUnknown()) > 0 || (c.Session != nil && len(c.Session.ProtoReflect().GetUnknown()) > 0) {
		return Fail("UNSUPPORTED_FEATURE")
	}
	return nil
}

// FingerprintV1 显式投影语义字段，凭据与追踪信息不参与比较。
func FingerprintV1(c *v1.SubmitGoalCommand) string {
	projection := struct {
		Version                                  uint32
		User, Issuer, Domain, Kind, Schema, Goal string
		Contract                                 uint32
		Session                                  *v1.GlobalName
		Expected                                 *uint64
		Until                                    *int64
	}{1, c.Identity.UserId, c.Identity.IssuerId, c.Identity.TargetDomainId, "SUBMIT_GOAL", c.SchemaId, c.Goal, c.ContractVersion, c.Session, c.ExpectedRevision, c.AcceptUntilUnixMs}
	b, _ := json.Marshal(projection)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

// NewRef 使用 UUIDv7；时间部分不参与授权或业务顺序。
func NewRef(user, domain, kind, schema string) *v1.Ref {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = (b[6] & 15) | 112
	b[8] = (b[8] & 63) | 128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	return &v1.Ref{Name: &v1.GlobalName{UserId: user, AuthorityDomainId: domain, ObjectKind: kind, LocalId: id}, Revision: 1, SchemaId: schema}
}
