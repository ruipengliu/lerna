package tasks

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type HandoffJobs interface {
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
	ReadHandoffClaim(context.Context, *v1.Job) (*v1.Job, error)
	CompleteHandoffInTransaction(context.Context, *v1.Job) error
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
}
type Recipient interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	Accept(context.Context, *v1.Caller, *v1.AcceptOperationCommand) (*v1.CommandReceipt, error)
}

func (s *Service) WithHandoffs(j HandoffJobs, r Recipient) *Service {
	s.handoffJobs = j
	s.recipient = r
	return s
}
func (s *Service) QueryHandoff(ctx context.Context, c *v1.Caller, a *v1.Ref) (*v1.Handoff, error) {
	if a == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, a.Name, s.user, s.domain, "admission"); e != nil {
		return nil, e
	}
	return s.store.LoadHandoff(ctx, a)
}

// ProcessHandoffClaim 的三个提交域分别由准入、接收方与本方法保存；始终先查询原身份。
func (s *Service) ProcessHandoffClaim(ctx context.Context, j *v1.Job) error {
	current, e := s.handoffJobs.ReadHandoffClaim(ctx, j)
	if e != nil {
		return e
	}
	caller := &v1.Caller{UserId: s.user, IssuerId: "tasks-handoff"}
	a, e := s.store.LoadAdmission(ctx, current.SpecificationRef)
	if e != nil {
		return e
	}
	if a == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	h, e := s.store.LoadHandoff(ctx, a.Ref)
	if e != nil {
		return e
	}
	if h == nil || !proto.Equal(current.Responsibility, h.Identity) || current.LedgerDomainId != a.LedgerDomainId || current.ExecutorEndpointId != a.ExecutorEndpointId {
		return command.Fail("INVARIANT_VIOLATION")
	}
	q, e := s.recipient.QueryReceipt(ctx, caller, h.Identity)
	if e != nil {
		return e
	}
	var receipt *v1.CommandReceipt
	switch q.State {
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
		receipt = q.Receipt
	case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
		receipt, e = s.recipient.Accept(ctx, caller, &v1.AcceptOperationCommand{Header: &v1.CommandHeader{Identity: h.Identity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, Admission: a})
		if e != nil {
			return e
		}
	default:
		return &command.Failure{Detail: &v1.ContractError{Code: "DEPENDENCY_UNAVAILABLE", Category: v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, CommandAcceptance: v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, RecoveryAction: "QUERY_OR_RETRY_ORIGINAL"}}
	}
	if receipt == nil || !proto.Equal(receipt.Identity, h.Identity) || receipt.Decision != v1.Decision_DECISION_ACCEPTED || receipt.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED || receipt.ResponsibleDomainId != a.LedgerDomainId || receipt.FingerprintVersion != 1 || receipt.Fingerprint != command.SemanticFingerprint("accept-operation", a) || receipt.ResultRef == nil || !proto.Equal(receipt.ResultRef.Name, a.OperationId) {
		return command.Fail("HANDOFF_RECEIPT_INVALID")
	}
	return s.store.Transaction(ctx, "tasks.handoff_receipt", func(tx context.Context) error {
		h, e := s.store.LoadHandoff(tx, a.Ref)
		if e != nil {
			return e
		}
		if h.RecipientReceipt != nil {
			if !proto.Equal(h.RecipientReceipt, receipt) {
				return command.Fail("INVARIANT_VIOLATION")
			}
		}
		if e = s.handoffJobs.CompleteHandoffInTransaction(tx, current); e != nil {
			return e
		}
		h.RecipientReceipt = receipt
		h.State = "ACKNOWLEDGED"
		h.Ref.Revision++
		return s.store.SaveHandoff(tx, h)
	})
}
func (s *Service) ProcessHandoffs(ctx context.Context, c *v1.Caller) error {
	process := command.NewRef(s.user, s.domain, "process", "process").Name.LocalId
	for {
		id := &v1.CommandIdentity{UserId: s.user, IssuerId: c.IssuerId, TargetDomainId: s.domain, CommandId: command.NewRef(s.user, s.domain, "command", "command").Name.LocalId}
		r, e := s.handoffJobs.ExecuteJob(ctx, c, &v1.JobCommand{Identity: id, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_HANDOFF"}, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			return nil
		}
		if e = s.ProcessHandoffClaim(ctx, r.Jobs[0]); e != nil {
			return e
		}
	}
}
func (s *Service) RecoverHandoffs(ctx context.Context, c *v1.Caller) error {
	for {
		if e := s.ProcessHandoffs(ctx, c); e != nil {
			return e
		}
		jobs, e := s.handoffJobs.Pending(ctx, c)
		if e != nil {
			return e
		}
		pending := false
		for _, j := range jobs {
			if j.Module == "tasks" && j.JobType == "DELIVER_HANDOFF" {
				pending = true
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
