package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 调用方已锁原Task及预算；Decision的创建/消费也先锁该Task。
// 只等待本轮目标/控制的未消费提案，不阻止旧轮收尾、效果观察或迟到费用。
func (s *Service) currentDecisionPendingTx(ctx context.Context, tx runtime.Tx, t taskState) (bool, error) {
	rows, err := tx.List(ctx, decisions, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
	if err != nil {
		return false, err
	}
	if len(rows) > int(s.config.MaxRelations) {
		return false, api.E("dependency_unavailable", "decision_collection_incomplete")
	}
	for _, row := range rows {
		var decision decisionState
		if err := row.Decode(&decision); err != nil {
			return false, err
		}
		if !decision.Consumed && decision.Snapshot.GoalRevision == t.Task.GoalRevision && decision.Snapshot.ControlRevision == t.Task.ControlRevision {
			return true, nil
		}
	}
	return false, nil
}
