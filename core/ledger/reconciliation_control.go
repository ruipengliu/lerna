package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Service) ControlReconciliation(ctx context.Context, caller *v1.Caller, c *v1.ControlReconciliationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("control-reconciliation", c.OperationId, c.ExpectedRevision, c.Action, c.Limits, c.GrantRef, c.ConfirmationRef), "ledger.reconcile_control", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" && caller.IssuerId != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		p, e := s.QueryReconciliation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if p == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if c.ExpectedRevision != p.Ref.Revision {
			return nil, command.Fail("REVISION_CONFLICT")
		}
		if p.State == "COMPLETED" {
			return nil, command.Fail("ALREADY_SETTLED")
		}
		_, now, e := s.store.LedgerPosition(tx)
		if e != nil {
			return nil, e
		}
		switch c.Action {
		case "PAUSE":
			if c.Limits != nil || c.GrantRef != nil || c.ConfirmationRef != nil {
				return nil, command.Fail("INVALID_INPUT")
			}
			p.State = "PAUSED"
			p.PauseReason = "USER_PAUSED"
		case "RESUME":
			if p.State != "PAUSED" {
				return nil, command.Fail("RECONCILIATION_NOT_PAUSED")
			}
			if c.Limits != nil {
				l := c.Limits
				if l.MaxChecks <= p.CheckCount || l.MaxChecks > 1000 || l.DeadlineUnixMs <= now || l.MaxFee < p.ReservedFee || l.InitialDelayMs <= 0 || l.MaxDelayMs < l.InitialDelayMs || l.MaxDelayMs > 86400000 {
					return nil, command.Fail("INVALID_RECONCILIATION_POLICY")
				}
				p.Limits = proto.Clone(l).(*v1.ReconciliationLimits)
			}
			if p.ActiveQueryRef != nil {
				q, e := s.QueryReconciliationQuery(tx, caller, p.ActiveQueryRef)
				if e != nil {
					return nil, e
				}
				if q == nil {
					return nil, command.Fail("INVARIANT_VIOLATION")
				}
				if q.AdmissionReceipt != nil && q.AdmissionReceipt.Decision == v1.Decision_DECISION_ACCEPTED {
					op, e := s.QueryOperation(tx, caller, q.QueryOperationRef.Name)
					if e != nil {
						return nil, e
					}
					if op != nil && op.Execution != nil && op.Execution.Send.Phase != "REGISTERED" && op.Lifecycle != "SETTLED" {
						return nil, command.Fail("QUERY_RESULT_UNKNOWN")
					}
					if (op == nil || op.Lifecycle != "SETTLED") && c.GrantRef != nil && !proto.Equal(c.GrantRef, p.GrantRef) {
						return nil, command.Fail("QUERY_RESPONSIBILITY_UNRESOLVED")
					}
					if op != nil && op.Lifecycle == "SETTLED" {
						p.ActiveQueryRef = nil
					}
				} else if q.AdmissionReceipt == nil {
					if c.GrantRef != nil && !proto.Equal(c.GrantRef, p.GrantRef) || q.AdmissionCommand != nil && c.ConfirmationRef != nil && !proto.Equal(c.ConfirmationRef, q.AdmissionCommand.ConfirmationRef) {
						return nil, command.Fail("QUERY_ADMISSION_UNRESOLVED")
					}
				} else {
					p.ActiveQueryRef = nil
				}
			}
			if c.GrantRef != nil {
				p.GrantRef = c.GrantRef
			}
			if c.ConfirmationRef != nil {
				p.ConfirmationRef = c.ConfirmationRef
			}
			p.State = "READY"
			if p.ActiveQueryRef != nil {
				p.State = "IN_PROGRESS"
			}
			p.PauseReason = ""
			p.NextReconcileAtUnixMs = now
		default:
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		p.Ref.Revision++
		if e = s.saveReconciliationJob(tx, p); e != nil {
			return nil, e
		}
		return p.Ref, s.saveReconciliation(tx, p)
	})
}

func reconciliationLimitReason(p *v1.Reconciliation, now int64, cap *v1.Capability) string {
	if p.CheckCount >= p.Limits.MaxChecks {
		return "CHECK_LIMIT"
	}
	if now >= p.Limits.DeadlineUnixMs {
		return "TIME_LIMIT"
	}
	if cap == nil {
		return "CAPABILITY_UNAVAILABLE"
	}
	if cap.FeeCeiling == nil || *cap.FeeCeiling < 0 {
		return "FEE_CEILING_UNKNOWN"
	}
	if *cap.FeeCeiling > p.Limits.MaxFee-p.ReservedFee {
		return "FEE_LIMIT"
	}
	return ""
}
func (s *Service) saveReconciliationJob(ctx context.Context, p *v1.Reconciliation) error {
	jobs, e := s.store.LedgerJobs(ctx, p.OperationId)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if j.JobType != "RECONCILE_OPERATION" || !proto.Equal(j.Ref.Name, p.JobRef.Name) {
			continue
		}
		j.Ref.Revision++
		j.ReadyAtUnixMs = p.NextReconcileAtUnixMs
		j.ProcessInstance = ""
		j.LeaseUntilUnixMs = 0
		j.WaitingReason = p.PauseReason
		switch p.State {
		case "COMPLETED":
			j.State = "COMPLETED"
		case "PAUSED":
			j.State = "BLOCKED"
		case "WAITING":
			j.State = "WAITING"
		default:
			j.State = "READY"
		}
		p.JobRef = j.Ref
		return s.store.SaveLedgerJob(ctx, j)
	}
	return command.Fail("INVARIANT_VIOLATION")
}
func (s *Service) pauseReconciliation(ctx context.Context, p *v1.Reconciliation, reason string) error {
	p.Ref.Revision++
	p.State = "PAUSED"
	p.PauseReason = reason
	if e := s.saveReconciliationJob(ctx, p); e != nil {
		return e
	}
	return s.saveReconciliation(ctx, p)
}
