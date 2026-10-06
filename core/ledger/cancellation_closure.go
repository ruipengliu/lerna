package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type CancellationClosureSource interface {
	ValidateCancellationClosure(context.Context, *v1.Caller, *v1.CloseCancellationCommand) (*v1.Admission, error)
}
type cancellationSealStore interface {
	SaveCancellationSeal(context.Context, *v1.CancellationSeal) error
	LoadCancellationSeal(context.Context, *v1.Ref) (*v1.CancellationSeal, error)
	CancellationSealForOperation(context.Context, *v1.GlobalName) (*v1.CancellationSeal, error)
}

func (s *Service) WithCancellationClosures(source CancellationClosureSource) *Service {
	s.cancellationClosures = source
	return s
}

// CloseForCancellation 由原端点在出口锁内确认；准确封闭覆盖尚未接纳的原动作身份。
func (s *Service) CloseForCancellation(ctx context.Context, caller *v1.Caller, c *v1.CloseCancellationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "tasks-cancellation" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if s.cancellationClosures == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("cancellation-seal", c), "ledger.cancellation_seal", func(tx context.Context) (*v1.Ref, error) {
		a, e := s.cancellationClosures.ValidateCancellationClosure(tx, caller, c)
		if e != nil {
			return nil, e
		}
		op, e := s.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op != nil && (!proto.Equal(op.AdmissionRef, a.Ref) || op.ExecutorEndpointId != a.ExecutorEndpointId) {
			return nil, command.Fail("INVALID_CLOSURE")
		}
		seal := &v1.CancellationSeal{Ref: command.NewRef(s.user, s.domain, "cancellation-seal", "lerna.v1.CancellationSeal"), IntentRef: c.IntentRef, CancellationRef: c.CancellationRef, AdmissionRef: a.Ref, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, NoSendProven: true}
		if op != nil {
			if op.Execution != nil {
				seal.PhysicalSendWasPossible = executionMayHaveSent(op.Execution)
				seal.NoSendProven = !seal.PhysicalSendWasPossible
				for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
					if send.Phase == "REGISTERED" {
						send.Ref.Revision++
						send.Phase = "CLOSED"
					}
					if send.Phase == "CLOSED" {
						seal.ClosedSendRefs = append(seal.ClosedSendRefs, proto.Clone(send.Ref).(*v1.Ref))
					}
				}
			}
			// 有尝试引用却无完整执行记录时，不具备内部未发送证明。
			if op.Execution == nil && len(op.AttemptRefs) > 0 {
				seal.NoSendProven = false
				seal.PhysicalSendWasPossible = true
			}
			op.Ref.Revision++
			seal.OperationRef = proto.Clone(op.Ref).(*v1.Ref)
			op.Dispatch = "SEALED"
			op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, seal.Ref)
			if seal.NoSendProven {
				applyCompletionNoSend(op)
			}
			jobs, e := s.store.LedgerJobs(tx, op.Ref.Name)
			if e != nil {
				return nil, e
			}
			for _, j := range jobs {
				if j.JobType != "EXECUTE_OPERATION" {
					continue
				}
				j.Ref.Revision++
				j.ProcessInstance = ""
				j.LeaseUntilUnixMs = 0
				j.State = "WAITING"
				if op.Lifecycle == "SETTLED" {
					j.State = "COMPLETED"
				}
				if e = s.store.SaveLedgerJob(tx, j); e != nil {
					return nil, e
				}
			}
			if e = s.saveOperation(tx, op); e != nil {
				return nil, e
			}
		}
		return seal.Ref, s.saveCancellationSeal(tx, seal, a.TaskId, c.Header.Identity, op)
	})
}
func (s *Service) QueryCancellationSeal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.CancellationSeal, error) {
	if e := s.checkHistory(c, r, "cancellation-seal", "lerna.v1.CancellationSeal"); e != nil {
		return nil, e
	}
	v, e := s.store.(cancellationSealStore).LoadCancellationSeal(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
