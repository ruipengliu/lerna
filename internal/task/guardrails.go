package task

import (
	"context"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const noProgressResume = "no_progress_limit: valid new input, goal change, usable result or original unknown resolution"

// guardrailTx只结束新推进；原执行效果与账务Job仍按其身份恢复。
func (s *Service) guardrailTx(ctx context.Context, tx runtime.Tx, t *taskState) (bool, error) {
	if terminal(*t) {
		return true, nil
	}
	if t.Continuations >= t.Policy.ContinuationLimit {
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
				for _, reason := range t.Task.WaitReasons {
					if strings.HasPrefix(reason.ResumeCondition, "continuation_limit:") {
						return true, nil
					}
				}
				if len(t.Task.WaitReasons) >= 100 {
					return false, api.E("overloaded", "wait_reason_capacity")
				}
				ref := decision.Intent.TaskRef
				ref.OwnerID, ref.ObjectID, ref.Revision = decision.Intent.BrainOwnerID, decision.Intent.DecisionID, 1
				t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: "dependency", ObjectRef: &ref, ResumeCondition: "continuation_limit: resolve original admitted decision; no new decision"})
				return true, s.saveTask(ctx, tx, t)
			}
		}
		t.Task.Status = "failed"
		t.Task.ControlRevision++
		t.Task.WaitReasons = []api.WaitReason{{Kind: "dependency", ResumeCondition: "goal closed: continuation_limit"}}
		if err := s.closeUnsent(ctx, tx, t); err != nil {
			return false, err
		}
		if err := s.controlDescendants(ctx, tx, t, true); err != nil {
			return false, err
		}
		if err := s.saveTask(ctx, tx, t); err != nil {
			return false, err
		}
		if err := s.controlJobs(ctx, tx, *t); err != nil {
			return false, err
		}
		return true, nil
	}
	if t.NoProgress < t.Policy.NoProgressLimit {
		waits := []api.WaitReason{}
		for _, reason := range t.Task.WaitReasons {
			if !strings.HasPrefix(reason.ResumeCondition, "no_progress_limit:") {
				waits = append(waits, reason)
			}
		}
		if len(waits) != len(t.Task.WaitReasons) {
			t.Task.WaitReasons = waits
			if err := s.saveTask(ctx, tx, t); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	for _, reason := range t.Task.WaitReasons {
		if strings.HasPrefix(reason.ResumeCondition, "no_progress_limit:") {
			return true, nil
		}
	}
	if len(t.Task.WaitReasons) >= 100 {
		return false, api.E("overloaded", "wait_reason_capacity")
	}
	t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: "dependency", ResumeCondition: noProgressResume})
	return true, s.saveTask(ctx, tx, t)
}
