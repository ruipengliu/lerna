package ledger

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type progressStore interface {
	SaveOperationProgressHandoff(context.Context, *v1.OperationProgressHandoff) error
	LoadOperationProgressHandoff(context.Context, *v1.Ref) (*v1.OperationProgressHandoff, error)
	OperationProgressHandoffs(context.Context, *v1.GlobalName) ([]*v1.OperationProgressHandoff, error)
}
type OperationProgressReceiver interface {
	AcceptOperationProgress(context.Context, *v1.Caller, *v1.AcceptOperationProgressCommand) (*v1.CommandReceipt, error)
	QueryOperationProgressReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	ProcessOperationProgress(context.Context, *v1.Caller) error
}

func (s *Service) WithOperationProgress(r OperationProgressReceiver) *Service {
	s.progressReceiver = r
	return s
}
func (s *Service) saveReconciliation(ctx context.Context, p *v1.Reconciliation) error {
	if e := s.store.SaveReconciliation(ctx, p); e != nil {
		return e
	}
	if e := s.recordReconciliationTrace(ctx, p); e != nil {
		return e
	}
	op, e := s.store.LoadOperation(ctx, p.OperationId)
	if e != nil {
		return e
	}
	if op == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	n := &v1.OperationProgressNotice{Ref: command.NewRef(s.user, s.domain, "operation-progress", "lerna.v1.OperationProgressNotice"), TaskId: p.TaskId, OperationRef: op.Ref, ReconciliationRef: p.Ref, EffectRef: op.EffectRef, State: p.State, PauseReason: p.PauseReason, LastObservationRef: p.LastObservationRef, Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: "ledger-progress", TargetDomainId: s.sourceDomain, CommandId: fmt.Sprintf("progress:%s:%d", p.Ref.Name.LocalId, p.Ref.Revision)}}
	h := &v1.OperationProgressHandoff{Notice: n, Command: &v1.AcceptOperationProgressCommand{Header: reconcileHeader(s.user, "ledger-progress", s.sourceDomain, n.Identity.CommandId), Notice: n}}
	return s.saveOperationProgressHandoff(ctx, h)
}
func (s *Service) QueryOperationProgress(ctx context.Context, c *v1.Caller, id *v1.GlobalName) ([]*v1.OperationProgressHandoff, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "operation"); e != nil {
		return nil, e
	}
	return s.store.OperationProgressHandoffs(ctx, id)
}
func (s *Service) QueryOperationProgressNotice(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.OperationProgressNotice, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.OperationProgressNotice" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "operation-progress"); e != nil {
		return nil, e
	}
	h, e := s.store.LoadOperationProgressHandoff(ctx, r)
	if e != nil || h == nil {
		return nil, e
	}
	if !proto.Equal(h.Notice.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return h.Notice, nil
}
func (s *Service) ProcessOperationProgress(ctx context.Context, c *v1.Caller) error {
	if e := command.CheckCaller(c, s.user); e != nil {
		return e
	}
	if s.progressReceiver == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	all, e := s.store.OperationProgressHandoffs(ctx, nil)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "ledger-progress"}
	for _, h := range all {
		if h.RecipientReceipt != nil {
			continue
		}
		q, e := s.progressReceiver.QueryOperationProgressReceipt(ctx, actor, h.Command.Header.Identity)
		if e != nil {
			return e
		}
		var r *v1.CommandReceipt
		switch q.State {
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
			r = q.Receipt
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
			r, e = s.progressReceiver.AcceptOperationProgress(ctx, actor, h.Command)
		default:
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		if e != nil {
			return e
		}
		if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED || r.ResponsibleDomainId != s.sourceDomain || !proto.Equal(r.Identity, h.Command.Header.Identity) || !proto.Equal(r.ResultRef, h.Notice.Ref) || r.Fingerprint != command.SemanticFingerprint("accept-operation-progress", h.Notice) {
			return command.Fail("HANDOFF_RECEIPT_INVALID")
		}
		// 接收方已有持久状态；推进失败时源通知仍待确认，原回执重放也重试唤醒。
		if e = s.progressReceiver.ProcessOperationProgress(ctx, c); e != nil {
			return e
		}
		ack := reconcileHeader(s.user, actor.IssuerId, s.domain, "progress-ack:"+h.Notice.Ref.Name.LocalId)
		saved, e := s.work.Execute(ctx, actor, ack, command.SemanticFingerprint("operation-progress-ack", h.Notice.Ref, r), "ledger.progress_ack", func(tx context.Context) (*v1.Ref, error) {
			fresh, e := s.store.LoadOperationProgressHandoff(tx, h.Notice.Ref)
			if e != nil {
				return nil, e
			}
			if fresh == nil {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			fresh.RecipientReceipt = r
			return fresh.Notice.Ref, s.saveOperationProgressHandoff(tx, fresh)
		})
		if e != nil {
			return e
		}
		if saved.Error != nil {
			return &command.Failure{Detail: saved.Error}
		}
	}
	return nil
}
