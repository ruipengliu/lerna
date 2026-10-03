package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// CompletionGatePreparer只为本次原完成责任取得当前准确来源门禁。
// 调用在Tx外；不重建成果/目标、分配预算或发起工具/模型行动。nil保留原profile。
type CompletionGatePreparer interface {
	PrepareCompletionGate(context.Context, runtime.Scope, runtime.Auth, api.Task, CompleteInput) (context.Context, error)
}

type completionPreparation struct {
	Task    api.Task
	Pending completionIntent
}

// 原Task/预算和主体元数据先于正文/外部当前proof。这里不能调用尚缺本次carrier的
// CurrentTaskGate；它仍在后续原CompleteTx强核。未检查/控制变化不触发外部准备。
func (s *Service) completionPreparationTx(ctx context.Context, tx runtime.Tx, t taskState) (*completionPreparation, error) {
	if terminal(t) || t.Task.Control != "running" || t.PendingGoalCommand != "" || t.PendingCompletionID == "" {
		return nil, nil
	}
	for _, id := range t.Ancestors {
		ancestor, err := getTask(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if terminal(ancestor) || ancestor.Task.Control != "running" {
			return nil, nil
		}
	}
	if t.IncomingAllocationID != "" {
		var allocation IncomingAllocation
		if _, err := tx.Get(ctx, incoming, t.IncomingAllocationID, &allocation); err != nil {
			return nil, err
		}
		if allocation.Gate != "open" {
			return nil, nil
		}
	}
	if err := s.checkSubmitterTx(ctx, tx, t); err != nil {
		return nil, err
	}
	var pending completionIntent
	if _, err := tx.Get(ctx, completionIntents, t.PendingCompletionID, &pending); err != nil {
		return nil, err
	}
	if pending.State != "pending" || pending.TaskID != t.Task.TaskID || pending.DecisionID != t.PendingCompletionID || pending.GoalRevision != t.Task.GoalRevision || pending.ControlRevision != t.Task.ControlRevision {
		return nil, nil
	}
	if pending.Input.TaskID != t.Task.TaskID || pending.Input.ExpectedGoalRevision != t.Task.GoalRevision || len(pending.Input.ArtifactRefs) == 0 || len(pending.Input.ArtifactRefs) > 100 || len(pending.Input.CheckRefs) > 100 || len(pending.CheckRequestRefs) > 100 {
		return nil, api.E("dependency_unavailable", "original_completion_input_invalid")
	}
	for _, ref := range pending.CheckRequestRefs {
		var request CheckRequest
		if _, err := tx.Get(ctx, checkRequests, ref.ObjectID, &request); err != nil {
			return nil, err
		}
		if request.Input.TaskID != t.Task.TaskID || request.Input.GoalRevision != pending.GoalRevision {
			return nil, api.E("idempotency_conflict", "completion_check_source_changed")
		}
		if request.State != "checked" {
			return nil, nil
		}
	}
	// 已知未闭效果只等原事实，不能借完成准备重新取得行动权。
	related, err := s.fullRelations(ctx, tx, t.Task.TaskID)
	if err != nil {
		return nil, err
	}
	for _, relation := range related {
		if relation.Kind == "operation" && (!relation.Closed || relation.MayApplyLater || relation.Effect == "unknown") {
			return nil, nil
		}
	}
	return &completionPreparation{Task: t.Task, Pending: pending}, nil
}

func (s *Service) prepareCompletionJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, prepared completionPreparation, port CompletionGatePreparer) error {
	if err := s.preIO(ctx, store, scope, work); err != nil {
		return err
	}
	fresh, err := port.PrepareCompletionGate(ctx, scope, serviceAuth(scope), prepared.Task, prepared.Pending.Input)
	if err != nil {
		if deferred(err) {
			return s.wait(ctx, store, scope, work)
		}
		return err
	}
	if fresh == nil {
		return api.E("dependency_unavailable", "completion_preparation_context_missing")
	}
	if err = s.preIO(fresh, store, scope, work); err != nil {
		return err
	}
	return s.finish(fresh, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		current, err := getTask(fresh, tx, prepared.Task.TaskID)
		if err != nil {
			return err
		}
		expired, err := s.expireTx(fresh, tx, &current)
		if err != nil || expired || terminal(current) {
			return err
		}
		if current.PendingCompletionID != prepared.Pending.DecisionID {
			return nil
		}
		if err = s.lockTaskTree(fresh, tx, current.Task.TaskID); err != nil {
			return err
		}
		var pending completionIntent
		if _, err = tx.Get(fresh, completionIntents, current.PendingCompletionID, &pending); err != nil {
			return err
		}
		if !api.Equal(pending.Input, prepared.Pending.Input) || !api.Equal(pending.CheckRequestRefs, prepared.Pending.CheckRequestRefs) {
			return api.E("idempotency_conflict", "original_completion_input_changed")
		}
		_, err = s.resumeCompletionTx(fresh, tx, &current)
		return err
	})
}
