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
		facts, e := s.closeOperationSends(tx, op, seal.Ref)
		if e != nil {
			return nil, e
		}
		seal.OperationRef, seal.ClosedSendRefs = facts.operationRef, facts.closedSendRefs
		seal.NoSendProven, seal.PhysicalSendWasPossible = facts.noSendProven, facts.physicalSendWasPossible
		if op != nil {
			if e = s.saveOperation(tx, op); e != nil {
				return nil, e
			}
		}
		return seal.Ref, s.saveCompletionSeal(tx, seal, a.TaskId, c.Header.Identity)
	})
}

func (s *Service) QueryCompletionSeal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.CompletionSeal, error) {
	if e := s.checkHistory(c, r, "completion-seal", "lerna.v1.CompletionSeal"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadCompletionSeal(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
