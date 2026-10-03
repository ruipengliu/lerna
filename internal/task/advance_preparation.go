package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// AdvanceGatePreparer只为本次正向advance取得有限当前证明。
// factory不执行它；已终态、过期、暂停及收尾分支仍沿原业务处理。
type AdvanceGatePreparer interface {
	PrepareTaskAdvance(context.Context, runtime.Scope, runtime.Auth, api.Task) (context.Context, error)
}

func (s *Service) prepareAdvanceEntry(handler runtime.JobHandler) runtime.JobHandler {
	preparer, configured := s.ports.Gate.(AdvanceGatePreparer)
	if !configured {
		return handler
	}
	return func(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
		if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		var actual taskState
		var needsPreparation bool
		err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
			var err error
			actual, err = getTask(ctx, tx, work.Job.SourceRef.ObjectID)
			if err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			deadline, err := api.ParseTime(actual.Task.Deadline)
			if err != nil {
				return err
			}
			needsPreparation = !terminal(actual) && actual.Task.Control == "running" && now.Before(deadline) && actual.PendingGoalCommand == "" && actual.PendingContextID == "" && actual.PendingCompletionID == "" && actual.Task.RequirementsState != "awaiting_input"
			if needsPreparation {
				return s.checkSubmitterTx(ctx, tx, actual)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if needsPreparation {
			if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
				return err
			}
			ctx, err = preparer.PrepareTaskAdvance(ctx, scope, submitterAuth(scope, actual), actual.Task)
			if err != nil {
				return err
			}
			if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
				return err
			}
		}
		return handler(ctx, store, scope, work)
	}
}
