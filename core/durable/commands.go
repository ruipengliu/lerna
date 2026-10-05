package durable

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// Execute 保存原决定；业务保存点使最后一道门禁拒绝也不会留下部分写入。
func (s *Service) Execute(ctx context.Context, caller *v1.Caller, h *v1.CommandHeader, fingerprint, point string, fn func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error) {
	if h == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if err := command.CheckIdentity(caller, h.Identity, s.user, s.domain); err != nil {
		return nil, err
	}
	var r *v1.CommandReceipt
	err := s.store.Transaction(ctx, point, func(tx context.Context) error {
		old, err := s.store.LoadReceipt(tx, h.Identity)
		if err != nil {
			return err
		}
		if old != nil {
			if old.Fingerprint != fingerprint {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			r = old
			return nil
		}
		var ref *v1.Ref
		err = s.store.BusinessScope(tx, func(scope context.Context) error { var e error; ref, e = fn(scope); return e })
		var failure *command.Failure
		if err != nil && (!errors.As(err, &failure) || failure.Detail.Category != v1.ErrorCategory_ERROR_CATEGORY_PERMANENT) {
			return err
		}
		pos, now, e := s.store.Position(tx)
		if e != nil {
			return e
		}
		r = &v1.CommandReceipt{Identity: h.Identity, FingerprintVersion: 1, Fingerprint: fingerprint, Phase: v1.ReceiptPhase_RECEIPT_PHASE_DECIDED, Decision: v1.Decision_DECISION_ACCEPTED, DecisionRef: command.NewRef(s.user, s.domain, "decision", "lerna.v1.CommandReceipt"), ResultRef: ref, ResponsibleDomainId: s.domain, CommitPosition: pos, DecidedAtUnixMs: now, DurabilityProfile: "LOCAL"}
		if failure != nil {
			r.ResultRef = nil
			r.Decision = v1.Decision_DECISION_REJECTED
			r.Error = failure.Detail
			r.Error.CommandAcceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_DECIDED
		}
		return s.store.SaveReceipt(tx, r)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}
