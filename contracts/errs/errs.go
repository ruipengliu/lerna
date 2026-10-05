// Package errs 把核心契约 3.2 的结构化错误包装成 Go 的 error。
//
// 错误必须说清楚：是否被接纳、怎样恢复、是否必须去查原记录。
// 不吞掉"提交结果未知""效果未知"这类结果。
package errs

import (
	"errors"
	"fmt"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// Error 是携带核心契约错误的 Go error。
type Error struct {
	E *lernav1.Error
}

func (e *Error) Error() string {
	if e == nil || e.E == nil {
		return "<nil>"
	}
	if e.E.GetDiagnostic() != "" {
		return fmt.Sprintf("%s: %s", codeName(e.E.GetCode()), e.E.GetDiagnostic())
	}
	return codeName(e.E.GetCode())
}

func codeName(c lernav1.ErrorCode) string {
	s := c.String()
	const p = "ERROR_CODE_"
	if len(s) > len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}

// 每个错误码的分类与恢复动作（核心契约 3.2 的表）。
var table = map[lernav1.ErrorCode]struct {
	cat lernav1.ErrorCategory
	rec lernav1.RecoveryAction
}{
	lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT:          {perm, lernav1.RecoveryAction_RECOVERY_ACTION_FIX_INPUT},
	lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_CONTRACT:   {perm, lernav1.RecoveryAction_RECOVERY_ACTION_FIX_INPUT},
	lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE:    {perm, lernav1.RecoveryAction_RECOVERY_ACTION_FIX_INPUT},
	lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED:      {perm, lernav1.RecoveryAction_RECOVERY_ACTION_NEW_COMMAND},
	lernav1.ErrorCode_ERROR_CODE_GRANT_REVOKED:          {perm, lernav1.RecoveryAction_RECOVERY_ACTION_NEW_COMMAND},
	lernav1.ErrorCode_ERROR_CODE_CONTENT_UNUSABLE:       {perm, lernav1.RecoveryAction_RECOVERY_ACTION_NEW_COMMAND},
	lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED:        {perm, lernav1.RecoveryAction_RECOVERY_ACTION_WAIT},
	lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID:   {perm, lernav1.RecoveryAction_RECOVERY_ACTION_WAIT},
	lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT:      {perm, lernav1.RecoveryAction_RECOVERY_ACTION_REREAD},
	lernav1.ErrorCode_ERROR_CODE_STALE_INPUT:            {perm, lernav1.RecoveryAction_RECOVERY_ACTION_REREAD},
	lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION:       {perm, lernav1.RecoveryAction_RECOVERY_ACTION_REREAD},
	lernav1.ErrorCode_ERROR_CODE_REVISION_CONFLICT:      {perm, lernav1.RecoveryAction_RECOVERY_ACTION_REREAD},
	lernav1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT:   {perm, lernav1.RecoveryAction_RECOVERY_ACTION_STOP},
	lernav1.ErrorCode_ERROR_CODE_AUTHORITY_UNREACHABLE:  {trans, lernav1.RecoveryAction_RECOVERY_ACTION_WAIT},
	lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE: {trans, lernav1.RecoveryAction_RECOVERY_ACTION_QUERY_ORIGINAL},
	lernav1.ErrorCode_ERROR_CODE_RATE_LIMITED:           {trans, lernav1.RecoveryAction_RECOVERY_ACTION_QUERY_ORIGINAL},
	lernav1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED:      {indet, lernav1.RecoveryAction_RECOVERY_ACTION_QUERY_ORIGINAL},
	lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST:         {indet, lernav1.RecoveryAction_RECOVERY_ACTION_QUERY_ORIGINAL},
	lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM:            {perm, lernav1.RecoveryAction_RECOVERY_ACTION_STOP},
	lernav1.ErrorCode_ERROR_CODE_EVIDENCE_CONFLICT:      {perm, lernav1.RecoveryAction_RECOVERY_ACTION_STOP},
	lernav1.ErrorCode_ERROR_CODE_INVARIANT_VIOLATION:    {perm, lernav1.RecoveryAction_RECOVERY_ACTION_STOP},
	lernav1.ErrorCode_ERROR_CODE_DATA_LOSS:              {perm, lernav1.RecoveryAction_RECOVERY_ACTION_STOP},
}

const (
	perm  = lernav1.ErrorCategory_ERROR_CATEGORY_PERMANENT
	trans = lernav1.ErrorCategory_ERROR_CATEGORY_TRANSIENT
	indet = lernav1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE
)

// New 按核心契约 3.2 的分类构造错误。
func New(code lernav1.ErrorCode, format string, args ...any) *Error {
	t := table[code]
	acc := lernav1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_ACCEPTED
	if t.cat == indet {
		acc = lernav1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN
	}
	return &Error{E: &lernav1.Error{
		Code:              code,
		Category:          t.cat,
		CommandAcceptance: acc,
		RecoveryAction:    t.rec,
		Diagnostic:        fmt.Sprintf(format, args...),
	}}
}

// From 从 error 中取出契约错误；不是契约错误时返回 nil。
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// Code 返回错误码；不是契约错误时返回 UNSPECIFIED。
func Code(err error) lernav1.ErrorCode {
	if e := From(err); e != nil {
		return e.E.GetCode()
	}
	return lernav1.ErrorCode_ERROR_CODE_UNSPECIFIED
}

// Is 判断错误是否为指定的错误码。
func Is(err error, code lernav1.ErrorCode) bool { return Code(err) == code }

// IsPermanent 判断是否为确定的、对当前请求不可恢复的错误（可以作为业务拒绝持久保存）。
func IsPermanent(err error) bool {
	e := From(err)
	return e != nil && e.E.GetCategory() == perm
}

// IsIndeterminate 判断是否无法确定原命令是否被接纳或是否生效。
func IsIndeterminate(err error) bool {
	e := From(err)
	return e != nil && e.E.GetCategory() == indet
}

// Proto 返回契约错误的 Protobuf 形式；非契约错误包装为 DEPENDENCY_UNAVAILABLE。
func Proto(err error) *lernav1.Error {
	if err == nil {
		return nil
	}
	if e := From(err); e != nil {
		return e.E
	}
	return New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "%v", err).E
}

// FromProto 把 Protobuf 错误还原为 Go error。
func FromProto(p *lernav1.Error) error {
	if p == nil {
		return nil
	}
	return &Error{E: p}
}
