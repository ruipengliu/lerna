package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type TaskClosureSource interface {
	ValidateTaskClosure(context.Context, *v1.Caller, *v1.CloseTaskEndpointCommand) (*v1.Admission, error)
}
type taskClosureSealStore interface {
	SaveTaskClosureSeal(context.Context, *v1.TaskClosureSeal) error
	LoadTaskClosureSeal(context.Context, *v1.Ref) (*v1.TaskClosureSeal, error)
	TaskClosureSealForOperation(context.Context, *v1.GlobalName) (*v1.TaskClosureSeal, error)
}

func (s *Service) WithTaskClosures(source TaskClosureSource) *Service {
	s.taskClosures = source
	return s
}

// CloseForTaskClose 由出口锁内调用；准确封闭也覆盖尚未接纳的原动作身份。
func (s *Service) CloseForTaskClose(ctx context.Context, caller *v1.Caller, c *v1.CloseTaskEndpointCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "tasks-closing" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if s.taskClosures == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("task-closure-seal", c), "ledger.task_closure_seal", func(tx context.Context) (*v1.Ref, error) {
		a, e := s.taskClosures.ValidateTaskClosure(tx, caller, c)
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
		seal := &v1.TaskClosureSeal{Ref: command.NewRef(s.user, s.domain, "task-closure-seal", "lerna.v1.TaskClosureSeal"), IntentRef: c.IntentRef, TaskClosingRef: c.TaskClosingRef, AdmissionRef: a.Ref, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, NoSendProven: true}
		facts, e := s.closeOperationSends(tx, op, seal.Ref)
		if e != nil {
			return nil, e
		}
		seal.OperationRef, seal.ClosedSendRefs = facts.operationRef, facts.closedSendRefs
		seal.NoSendProven, seal.PhysicalSendWasPossible = facts.noSendProven, facts.physicalSendWasPossible
		if op != nil {
			if op.Lifecycle != "SETTLED" || op.Effect == nil || op.Effect.Outcome == "UNKNOWN" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
				followup, err := s.retainExecutionFollowup(tx, a, op, c.TaskClosingRef, c.Header.Identity)
				if err != nil {
					return nil, err
				}
				seal.ExecutionFollowupRef = followup.Ref
			}
			if e = s.saveOperation(tx, op); e != nil {
				return nil, e
			}
		}
		return seal.Ref, s.saveTaskClosureSeal(tx, seal, a.TaskId, c.Header.Identity)
	})
}
func (s *Service) QueryTaskClosureSeal(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.TaskClosureSeal, error) {
	if e := s.checkHistory(c, r, "task-closure-seal", "lerna.v1.TaskClosureSeal"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadTaskClosureSeal(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
