// Package durable 保存命令原决定与待办，不裁决业务事实。
package durable

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Store interface {
	BusinessScope(context.Context, func(context.Context) error) error
	Transaction(context.Context, string, func(context.Context) error) error
	LoadReceipt(context.Context, *v1.CommandIdentity) (*v1.CommandReceipt, error)
	SaveReceipt(context.Context, *v1.CommandReceipt) error
	SaveJob(context.Context, *v1.Job) error
	LoadJob(context.Context, *v1.GlobalName) (*v1.Job, error)
	FindJobPurpose(context.Context, string, string) (*v1.Job, error)
	PendingJobs(context.Context, string) ([]*v1.Job, error)
	Position(context.Context) (uint64, int64, error)
}
type Service struct {
	store        Store
	user, domain string
}

func New(store Store, user, domain string) *Service {
	return &Service{store: store, user: user, domain: domain}
}
func (s *Service) Submit(ctx context.Context, caller *v1.Caller, c *v1.SubmitGoalCommand, content *v1.Ref) (*v1.CommandReceipt, error) {
	if err := command.CheckIdentity(caller, c.Identity, s.user, s.domain); err != nil {
		return nil, err
	}
	fingerprint := command.FingerprintV1(c)
	var receipt *v1.CommandReceipt
	err := s.store.Transaction(ctx, "durable.submit", func(tx context.Context) error {
		old, err := s.store.LoadReceipt(tx, c.Identity)
		if err != nil {
			return err
		}
		if old != nil {
			if old.Fingerprint != fingerprint {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			receipt = old
			return nil
		}
		position, _, err := s.store.Position(tx)
		if err != nil {
			return err
		}
		jobRef := command.NewRef(s.user, s.domain, "job", "lerna.v1.Job")
		pending := proto.Clone(c).(*v1.SubmitGoalCommand)
		pending.Goal = ""
		pending.Credential = ""
		pending.TraceId = ""
		identityKey, _ := json.Marshal([]string{c.Identity.UserId, c.Identity.IssuerId, c.Identity.TargetDomainId, c.Identity.CommandId})
		job := &v1.Job{Ref: jobRef, Module: "sessions", JobType: "DECIDE_GOAL", ContractVersion: 1, Responsibility: c.Identity, State: "READY", SpecificationRef: content, PurposeKey: "decide:" + string(identityKey), Goal: &v1.PendingGoal{Command: pending, ContentRef: content}}
		receipt = &v1.CommandReceipt{Identity: c.Identity, FingerprintVersion: 1, Fingerprint: fingerprint, Phase: v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED, JobRef: jobRef, ResponsibleDomainId: s.domain, CommitPosition: position, DurabilityProfile: "LOCAL", InputRef: content}
		if err := s.store.SaveJob(tx, job); err != nil {
			return err
		}
		return s.store.SaveReceipt(tx, receipt)
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

// Decide 在同一事务中调用固定的事实写入方，再保存不可变决定。
// handler 必须在任何写入前完成全部业务拒绝检查；写入后只能返回导致整笔回滚的暂时性错误。
// 多参与者需要写入后裁决时，必须先扩展事务保存点，不能直接复用此约定。
func (s *Service) Decide(ctx context.Context, job *v1.Job, handler func(context.Context, *v1.PendingGoal) (*v1.Ref, *v1.Ref, error)) error {
	return s.store.Transaction(ctx, "durable.decide", func(tx context.Context) error {
		if job == nil || job.Ref == nil || job.Ref.Name == nil || job.Ref.Name.UserId != s.user || job.Ref.Name.AuthorityDomainId != s.domain {
			return command.Fail("STALE_CLAIM")
		}
		current, err := s.store.LoadJob(tx, job.Ref.Name)
		if err != nil {
			return err
		}
		_, now, err := s.store.Position(tx)
		if err != nil {
			return err
		}
		if err := validClaim(current, job.ProcessInstance, job.ClaimEpoch, job.Ref.Revision, now); err != nil {
			return err
		}
		job = current

		r, err := s.store.LoadReceipt(tx, job.Responsibility)
		if err != nil {
			return err
		}
		if r == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		if r.Phase == v1.ReceiptPhase_RECEIPT_PHASE_DECIDED {
			return nil
		}
		session, task, err := handler(tx, job.Goal)
		if err != nil {
			var f *command.Failure
			if !errors.As(err, &f) {
				return err
			}
			if f.Detail.Category != v1.ErrorCategory_ERROR_CATEGORY_PERMANENT {
				return err
			}
			r.Decision = v1.Decision_DECISION_REJECTED
			r.Error = proto.Clone(f.Detail).(*v1.ContractError)
			r.Error.CommandAcceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_DECIDED
		} else {
			r.Decision = v1.Decision_DECISION_ACCEPTED
			r.SessionRef = session
			r.TaskRef = task
		}
		position, now, err := s.store.Position(tx)
		if err != nil {
			return err
		}
		r.Phase = v1.ReceiptPhase_RECEIPT_PHASE_DECIDED
		r.CommitPosition = position
		r.DecidedAtUnixMs = now
		r.DecisionRef = command.NewRef(s.user, s.domain, "decision", "lerna.v1.CommandReceipt")
		job.State = "COMPLETED"
		job.Ref.Revision++
		if err := s.store.SaveJob(tx, job); err != nil {
			return err
		}
		return s.store.SaveReceipt(tx, r)
	})
}
func (s *Service) Pending(ctx context.Context, caller *v1.Caller) ([]*v1.Job, error) {
	if err := command.CheckCaller(caller, s.user); err != nil {
		return nil, err
	}
	return s.store.PendingJobs(ctx, s.user)
}
func (s *Service) QueryReceipt(ctx context.Context, caller *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	if err := command.CheckIdentity(caller, id, s.user, s.domain); err != nil {
		return nil, err
	}
	r, err := s.store.LoadReceipt(ctx, id)
	q := &v1.ReceiptQuery{State: v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND, ResponsibleDomainId: s.domain}
	if err != nil {
		q.State = v1.ReceiptQueryState_RECEIPT_QUERY_STATE_UNAVAILABLE
		q.Error = &v1.ContractError{Code: "DEPENDENCY_UNAVAILABLE", Category: v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, CommandAcceptance: v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN, RecoveryAction: "QUERY_OR_RETRY_ORIGINAL"}
		return q, nil
	}
	if r != nil {
		q.Receipt = r
		q.Revision = r.CommitPosition
		q.State = v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED
		if r.Phase == v1.ReceiptPhase_RECEIPT_PHASE_DECIDED {
			q.State = v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED
		}
	}
	return q, nil
}
