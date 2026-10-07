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
		return seal.Ref, s.saveCancellationSeal(tx, seal, a.TaskId, c.Header.Identity, op)
	})
}
func (s *Service) QueryCancellationSeal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.CancellationSeal, error) {
	if e := s.checkHistory(c, r, "cancellation-seal", "lerna.v1.CancellationSeal"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadCancellationSeal(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
