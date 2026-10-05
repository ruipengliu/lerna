package durable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// ExecuteJob 保存包括空领取在内的原决定。只允许受信宿主调用。
func (s *Service) ExecuteJob(ctx context.Context, caller *v1.Caller, c *v1.JobCommand) (*v1.CommandReceipt, error) {
	if c == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckIdentity(caller, c.Identity, s.user, s.domain); e != nil {
		return nil, e
	}
	if c.ContractVersion != 1 {
		return nil, command.Fail("UNSUPPORTED_CONTRACT")
	}
	b, _ := json.Marshal([]any{"JOB_V1", c.Action, c.ContractVersion, c.ProcessInstance, c.AllowedTypes, c.Limit, c.LeaseMs, c.JobRef, c.ClaimEpoch, c.Module, c.NextState, c.ReadyAtUnixMs, c.WaitingReason, c.SpecificationRef})
	h := sha256.Sum256(b)
	fingerprint := hex.EncodeToString(h[:])
	var result *v1.CommandReceipt
	err := s.store.Transaction(ctx, "durable.jobs", func(tx context.Context) error {
		old, e := s.store.LoadReceipt(tx, c.Identity)
		if e != nil {
			return e
		}
		if old != nil {
			if old.Fingerprint != fingerprint {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			result = old
			return nil
		}
		position, now, e := s.store.Position(tx)
		if e != nil {
			return e
		}
		result = &v1.CommandReceipt{Identity: c.Identity, FingerprintVersion: 1, Fingerprint: fingerprint, Phase: v1.ReceiptPhase_RECEIPT_PHASE_DECIDED, Decision: v1.Decision_DECISION_ACCEPTED, DecidedAtUnixMs: now, CommitPosition: position, ResponsibleDomainId: s.domain, DurabilityProfile: "LOCAL", DecisionRef: command.NewRef(s.user, s.domain, "decision", "lerna.v1.CommandReceipt")}
		jobs, e := s.applyJob(tx, c, now)
		if e != nil {
			var failure *command.Failure
			if !errors.As(e, &failure) || failure.Detail.Category != v1.ErrorCategory_ERROR_CATEGORY_PERMANENT {
				return e
			}
			result.Decision = v1.Decision_DECISION_REJECTED
			result.Error = failure.Detail
			result.Error.CommandAcceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_DECIDED
		} else {
			result.Jobs = jobs
		}
		return s.store.SaveReceipt(tx, result)
	})
	return result, err
}

func (s *Service) QueryJob(ctx context.Context, caller *v1.Caller, n *v1.GlobalName) (*v1.Job, error) {
	if e := command.CheckName(caller, n, s.user, s.domain, "job"); e != nil {
		return nil, e
	}
	return s.store.LoadJob(ctx, n)
}
func (s *Service) applyJob(ctx context.Context, c *v1.JobCommand, now int64) ([]*v1.Job, error) {
	if c.Action == "CLAIM" {
		if c.ProcessInstance == "" || c.Limit == 0 || c.Limit > 100 || c.LeaseMs <= 0 || c.LeaseMs > 60000 || len(c.AllowedTypes) == 0 {
			return nil, command.Fail("INVALID_INPUT")
		}
		jobs, e := s.store.PendingJobs(ctx, s.user)
		if e != nil {
			return nil, e
		}
		var claimed []*v1.Job
		slices.SortStableFunc(jobs, func(a, b *v1.Job) int {
			if a.Priority > b.Priority {
				return -1
			}
			if a.Priority < b.Priority {
				return 1
			}
			return 0
		})
		for _, j := range jobs {
			if !slices.Contains(c.AllowedTypes, j.JobType) || j.ReadyAtUnixMs > now || (j.State == "CLAIMED" && j.LeaseUntilUnixMs > now) {
				continue
			}
			j.State = "CLAIMED"
			j.ClaimEpoch++
			j.Attempts++
			j.ProcessInstance = c.ProcessInstance
			j.LeaseUntilUnixMs = now + c.LeaseMs
			if e := s.store.SaveJob(ctx, j); e != nil {
				return nil, e
			}
			claimed = append(claimed, j)
			if len(claimed) == int(c.Limit) {
				break
			}
		}
		return claimed, nil
	}
	if c.JobRef == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(&v1.Caller{UserId: s.user, IssuerId: c.Identity.IssuerId}, c.JobRef.Name, s.user, s.domain, "job"); e != nil {
		return nil, e
	}
	j, e := s.store.LoadJob(ctx, c.JobRef.Name)
	if e != nil {
		return nil, e
	}
	if j == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	switch c.Action {
	case "RENEW", "PROGRESS":
		if e := validClaim(j, c.ProcessInstance, c.ClaimEpoch, c.JobRef.Revision, now); e != nil {
			return nil, e
		}
		if c.Action == "RENEW" {
			if c.LeaseMs <= 0 || c.LeaseMs > 60000 {
				return nil, command.Fail("INVALID_INPUT")
			}
			j.LeaseUntilUnixMs = max(j.LeaseUntilUnixMs, now+c.LeaseMs)
		} else {
			if c.NextState != "WAITING" && c.NextState != "COMPLETED" && c.NextState != "BLOCKED" {
				return nil, command.Fail("INVALID_INPUT")
			}
			// DECIDE_GOAL 的完成必须与原命令决定同事务，不能绕过事实写入方。
			if j.JobType == "DECIDE_GOAL" && c.NextState == "COMPLETED" {
				return nil, command.Fail("UNSUPPORTED_FEATURE")
			}
			j.State = c.NextState
			j.ReadyAtUnixMs = c.ReadyAtUnixMs
			j.WaitingReason = c.WaitingReason
			j.Ref.Revision++
		}
	case "CONTROL":
		if c.Module != j.Module {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.JobRef.Revision != j.Ref.Revision {
			return nil, command.Fail("REVISION_CONFLICT")
		}
		if j.State == "CLOSED" || j.State == "COMPLETED" {
			return nil, command.Fail("TERMINAL_JOB")
		}
		if c.NextState != "CLOSED" && c.NextState != "BLOCKED" && c.NextState != "READY" {
			return nil, command.Fail("INVALID_INPUT")
		}
		j.State = c.NextState
		j.Ref.Revision++
		j.ReadyAtUnixMs = c.ReadyAtUnixMs
		j.WaitingReason = c.WaitingReason
		if c.SpecificationRef != nil {
			j.SpecificationRef = c.SpecificationRef
		}
	default:
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}

	if e := s.store.SaveJob(ctx, j); e != nil {
		return nil, e
	}
	return []*v1.Job{j}, nil
}
func validClaim(j *v1.Job, instance string, epoch, revision uint64, now int64) error {
	if j == nil || j.State != "CLAIMED" || j.ProcessInstance != instance || j.ClaimEpoch != epoch || j.Ref.Revision != revision || j.LeaseUntilUnixMs <= now {
		return command.Fail("STALE_CLAIM")
	}
	return nil
}
