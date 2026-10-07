package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type CancellationCommands interface {
	QueryCancellation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Cancellation, error)
	QueryCancellationIntent(context.Context, *v1.Caller, *v1.Ref) (*v1.CancellationClosureIntent, error)
}
type CancellationSeals interface {
	QueryCancellationSeal(context.Context, *v1.Caller, *v1.Ref) (*v1.CancellationSeal, error)
}

func (c CLI) cancellation(ctx context.Context, args []string) (*v1.CancellationView, error) {
	tasks, ok := c.Tasks.(CancellationCommands)
	if !ok || c.Ledger == nil {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if len(args) != 2 {
		return nil, command.Fail("INVALID_INPUT")
	}
	id := &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]}
	scope, e := tasks.QueryCancellation(ctx, c.Caller, id)
	if e != nil {
		return nil, e
	}
	if scope == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	task, e := c.Tasks.QueryTask(ctx, c.Caller, id)
	if e != nil {
		return nil, e
	}
	view := &v1.CancellationView{Cancellation: scope, Task: task}
	for _, ref := range scope.ClosureIntentRefs {
		intent, e := tasks.QueryCancellationIntent(ctx, c.Caller, ref)
		if e != nil {
			return nil, e
		}
		if intent == nil || intent.RecipientReceipt == nil {
			view.PendingClosureRefs = append(view.PendingClosureRefs, ref)
		}
		if intent == nil {
			continue
		}
		view.ClosureIntents = append(view.ClosureIntents, intent)
		var seal *v1.CancellationSeal
		if intent.RecipientReceipt != nil {
			ledger, ok := c.Ledger.(CancellationSeals)
			if !ok {
				return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
			}
			seal, e = ledger.QueryCancellationSeal(ctx, c.Caller, intent.RecipientReceipt.ResultRef)
			if e != nil {
				return nil, e
			}
			if seal == nil || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.CancellationRef, scope.Ref) || !proto.Equal(seal.AdmissionRef, intent.Command.AdmissionRef) || !proto.Equal(seal.OperationId, intent.Command.OperationId) || seal.ExecutorEndpointId != intent.Command.ExecutorEndpointId {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			view.ClosureSeals = append(view.ClosureSeals, seal)
		}
		op, e := c.Ledger.QueryOperation(ctx, c.Caller, intent.Command.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil {
			if seal != nil && (seal.OperationRef != nil || !seal.NoSendProven || seal.PhysicalSendWasPossible) {
				return nil, command.Fail("EXECUTION_FACTS_UNAVAILABLE")
			}
			view.AwaitingOperationAdmissionRefs = append(view.AwaitingOperationAdmissionRefs, intent.Command.AdmissionRef)
			continue
		}
		view.Operations = append(view.Operations, op)
		if op.Effect == nil || op.Effect.Outcome == "UNKNOWN" || op.Effect.LateEffect == "MAY_OCCUR" || op.Effect.EvidenceConflict {
			view.UnresolvedEffectOperationRefs = append(view.UnresolvedEffectOperationRefs, op.Ref)
		}
		if ledger, ok := c.Ledger.(ReconciliationLedger); ok {
			r, e := ledger.QueryReconciliation(ctx, c.Caller, op.Ref.Name)
			if e != nil {
				return nil, e
			}
			if r != nil {
				view.Reconciliations = append(view.Reconciliations, r)
			}
		}
	}
	return view, nil
}
