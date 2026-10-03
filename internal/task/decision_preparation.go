package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// DecisionGatePreparer为原正向决策派发取得本次有限当前证明。
// 准备只在原Task/主体元数据核对之后、事务外执行；它不能替代最终门禁，
// 不能改变原Decision、Command、预算或期限。终态和收尾不调用此端口。
type DecisionGatePreparer interface {
	PrepareTaskDecision(context.Context, runtime.Scope, runtime.Auth, api.Task, api.DecisionDispatchIntent) (context.Context, error)
}

func (s *Service) prepareDecisionEntry(handler runtime.JobHandler) runtime.JobHandler {
	preparer, configured := s.ports.Gate.(DecisionGatePreparer)
	if !configured {
		return handler
	}
	return func(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
		if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		var actual taskState
		var original decisionState
		var needsPreparation bool
		err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
			var err error
			actual, original, err = decisionForTaskTx(ctx, tx, work.Job.SourceRef.ObjectID)
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
			taskRef, snapshot := original.Intent.TaskRef, original.Snapshot
			needsPreparation = !original.Consumed && !terminal(actual) && actual.Task.Control == "running" && now.Before(deadline) &&
				actual.PendingGoalCommand == "" && actual.PendingContextID == "" && actual.PendingCompletionID == "" && actual.Task.RequirementsState != "awaiting_input" &&
				taskRef.TenantID == scope.TenantID && taskRef.OwnerID == scope.OwnerID && taskRef.ObjectID == actual.Task.TaskID && taskRef.Revision <= actual.Task.Revision &&
				snapshot.GoalRevision == actual.Task.GoalRevision && snapshot.ControlRevision == actual.Task.ControlRevision && api.Equal(snapshot.GoalRef, actual.Task.GoalRef) && api.Equal(snapshot.PolicyRef, actual.Task.PolicyRef)
			if needsPreparation {
				// 父范围的当前证明尚未取得；这里只核原提交者，不先调用完整CheckCurrent。
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
			ctx, err = preparer.PrepareTaskDecision(ctx, scope, submitterAuth(scope, actual), actual.Task, original.Intent)
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
