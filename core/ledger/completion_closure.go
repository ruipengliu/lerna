package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type CompletionClosureSource interface {
	ValidateCompletionClosure(context.Context, *v1.Caller, *v1.CloseCompletionCommand) (*v1.Admission, error)
}
type completionSealStore interface {
	SaveCompletionSeal(context.Context, *v1.CompletionSeal) error
	LoadCompletionSeal(context.Context, *v1.Ref) (*v1.CompletionSeal, error)
	CompletionSealForOperation(context.Context, *v1.GlobalName) (*v1.CompletionSeal, error)
}

func (s *Service) WithCompletionClosures(source CompletionClosureSource) *Service {
	s.completionClosures = source
	return s
}

// CloseForCompletion 由出口锁内调用；准确封闭也覆盖尚未接纳的原动作身份。
func (s *Service) CloseForCompletion(ctx context.Context, caller *v1.Caller, c *v1.CloseCompletionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "tasks-completion" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if s.completionClosures == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("completion-seal", c), "ledger.completion_seal", func(tx context.Context) (*v1.Ref, error) {
		a, e := s.completionClosures.ValidateCompletionClosure(tx, caller, c)
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
		seal := &v1.CompletionSeal{Ref: command.NewRef(s.user, s.domain, "completion-seal", "lerna.v1.CompletionSeal"), IntentRef: c.IntentRef, VerificationRef: c.VerificationRef, AdmissionRef: a.Ref, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, NoSendProven: true}
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
			if e = s.store.SaveOperation(tx, op); e != nil {
				return nil, e
			}
		}
		return seal.Ref, s.store.(completionSealStore).SaveCompletionSeal(tx, seal)
	})
}
func applyCompletionNoSend(op *v1.Operation) {
	op.Dispatch = "SEALED"
	op.Lifecycle = "SETTLED"
	op.Effect.Ref.Revision++
	op.Effect.Outcome = "NOT_APPLIED"
	op.Effect.LateEffect = "RULED_OUT"
	op.EffectRef = op.Effect.Ref
	if op.Execution != nil {
		if op.Execution.Send.Phase != "CLOSED" {
			op.Execution.Send.Ref.Revision++
			op.Execution.Send.Phase = "CLOSED"
		}
		op.Execution.Attempt.Ref.Revision++
		op.Execution.Attempt.Phase = "CLOSED"
		op.AttemptRefs = []*v1.Ref{op.Execution.Attempt.Ref}
	}
}
func (s *Service) QueryCompletionSeal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.CompletionSeal, error) {
	if e := s.checkHistory(c, r, "completion-seal", "lerna.v1.CompletionSeal"); e != nil {
		return nil, e
	}
	v, e := s.store.(completionSealStore).LoadCompletionSeal(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
