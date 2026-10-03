package brain

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 只有正准入门禁的确定关闭进入取消；存储、依赖、未知费用、Claim 冲突不归此类。
type closedDecisionGate struct{ cause error }

func (e *closedDecisionGate) Error() string { return e.cause.Error() }
func (e *closedDecisionGate) Unwrap() error { return e.cause }
func gateFailure(err error) error {
	if api.IsCode(err, "invalid_state") || api.IsCode(err, "revision_conflict") || api.IsCode(err, "forbidden") || api.IsCode(err, "expired") {
		return &closedDecisionGate{cause: err}
	}
	return err
}

func (s *Service) currentGate(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, d decision) error {
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := s.config.Gate.CheckTx(ctx, tx, d.Principal, d.Input, d.Encoding); err != nil {
			return gateFailure(err)
		}
		return tx.Guard(ctx, work.Claim)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

func decideCancelled(ctx context.Context, tx runtime.Tx, command runtime.StoredCommand, d decision) error {
	if command.Command.CommandID != d.CommandID || command.Command.Method != "brain.decide" || command.Command.TargetID != d.Input.DecisionID || command.PrincipalID != d.Principal.SubjectID {
		return api.E("idempotency_conflict", "original_decision_command_changed")
	}
	rejection := api.E("invalid_state", "decision_cancelled")
	if command.Receipt.Stage == "rejected" && api.Equal(command.Receipt.Error, rejection) && len(command.Receipt.Output) == 0 {
		return nil
	}
	return runtime.Decide(ctx, tx, d.CommandID, nil, rejection)
}

// 原接纳关闭与原回执共同提交；不能清掉可能已发送的用量，或把缺账记为零。
func (s *Service) finishCancelled(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, original decision) error {
	disposition := runtime.Done()
	if original.Record.SendStarted && !original.Record.UsageFinal {
		disposition = runtime.Waiting(time.Now().Add(time.Second))
	}
	return s.finish(ctx, store, scope, work, disposition, func(tx runtime.Tx) error {
		command, err := tx.LoadCommand(ctx, original.CommandID)
		if err != nil {
			return err
		}
		var d decision
		rev, err := getDecisionTx(ctx, tx, original.Input.DecisionID, &d)
		if err != nil {
			return err
		}
		if !sameFrozenDecision(d, original) {
			return api.E("idempotency_conflict", "original_decision_changed")
		}
		if d.Phase == "completed" || d.Phase == "failed" {
			// 并发已提交的原终态不能被后来的负控制改写。
			return nil
		}
		if d.Record.SendStarted && d.Generated == nil {
			return api.E("accounting_unknown", "original_model_result_unknown")
		}
		if d.Phase != "cancelled" || !d.CancelRequested || d.Record.ProposalRef != nil || !d.Record.SendStarted && !d.Record.UsageFinal {
			d.CancelRequested = true
			d.Phase = "cancelled"
			d.Record.Status = "cancelled"
			d.Record.ProposalRef = nil
			if !d.Record.SendStarted {
				d.Record.UsageFinal = true
			}
			d.Revision++
			d.Record.Revision++
			if err = putDecision(ctx, tx, d.Input.DecisionID, rev, d); err != nil {
				return err
			}
		}
		return decideCancelled(ctx, tx, command, d)
	})
}

func (s *Service) closeAfterGateError(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, original decision, err error) error {
	var closed *closedDecisionGate
	if errors.As(err, &closed) {
		return s.finishCancelled(ctx, store, scope, work, original)
	}
	return err
}

func cumulativeUsage(old, next []api.Amount) error {
	current := make(map[string]string, len(next))
	for _, amount := range next {
		current[amount.Unit] = amount.Value
	}
	for _, amount := range old {
		value, ok := current[amount.Unit]
		if !ok {
			return api.E("idempotency_conflict", "original_usage_decreased")
		}
		order, err := api.CompareDecimal(value, amount.Value)
		if err != nil {
			return err
		}
		if order < 0 {
			return api.E("idempotency_conflict", "original_usage_decreased")
		}
	}
	return nil
}

// 终态已固定后仍只核原调用的迟到费用，不改变原提案/决定、不重新物理发送。
func (s *Service) reconcileClosed(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, original decision, profile Profile) error {
	if !original.Record.SendStarted || original.Record.UsageFinal {
		if original.Phase == "cancelled" {
			return s.finishCancelled(ctx, store, scope, work, original)
		}
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if original.Encoding == nil || original.Generated == nil {
		return api.E("accounting_unknown", "original_model_result_unknown")
	}
	callCtx, cancel := context.WithTimeout(ctx, profile.RequestTimeout)
	out, err := s.config.Engine.Lookup(callCtx, original.CallID, *original.Encoding)
	cancel()
	if err != nil {
		return s.wait(ctx, store, scope, work)
	}
	if !api.Equal(out.Draft, original.Generated.Draft) || !api.Equal(out.Contents, original.Generated.Contents) {
		return api.E("idempotency_conflict", "original_model_output_changed")
	}
	if err = api.ValidateAmounts(out.Usage); err != nil {
		return err
	}
	disposition := runtime.Done()
	if !out.UsageFinal {
		disposition = runtime.Waiting(time.Now().Add(time.Second))
	}
	return s.finish(ctx, store, scope, work, disposition, func(tx runtime.Tx) error {
		command, err := tx.LoadCommand(ctx, original.CommandID)
		if err != nil {
			return err
		}
		return s.change(ctx, tx, original.Input.DecisionID, func(d *decision) error {
			if !sameFrozenDecision(*d, original) || d.Phase != original.Phase {
				return api.E("revision_conflict", "decision_changed")
			}
			if err = cumulativeUsage(d.Record.Usage, out.Usage); err != nil {
				return err
			}
			d.Record.Usage = out.Usage
			d.Record.UsageFinal = d.Record.UsageFinal || out.UsageFinal
			if d.Phase == "cancelled" {
				return decideCancelled(ctx, tx, command, *d)
			}
			if d.Phase == "completed" && command.Receipt.Stage == "applied" || d.Phase == "failed" && command.Receipt.Stage == "rejected" {
				return nil
			}
			return api.E("accounting_unknown", "original_terminal_receipt_unavailable")
		})
	})
}
