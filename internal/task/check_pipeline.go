package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const completionIntents = "task.completion_intents"

type completionIntent struct {
	Revision         uint64          `json:"revision"`
	DecisionID       string          `json:"decision_id"`
	TaskID           string          `json:"task_id"`
	GoalRevision     uint64          `json:"goal_revision"`
	ControlRevision  uint64          `json:"control_revision"`
	Input            CompleteInput   `json:"input"`
	CheckRequestRefs []api.ObjectRef `json:"check_request_refs"`
	State            string          `json:"state"`
}

func (s *Service) consumeCompletionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, t taskState, p Proposal) (string, error) {
	if len(p.CheckRequests) > 100 || len(p.ArtifactRefs) == 0 || len(p.ArtifactRefs) > 100 {
		return "", invalid("bounded_completion_requests_required")
	}
	refs := []api.ObjectRef{}
	for _, request := range p.CheckRequests {
		if request.TaskID != t.Task.TaskID || request.GoalRevision != t.Task.GoalRevision {
			return "", api.E("revision_conflict", "completion_check_goal_mismatch")
		}
		artifactPresent := false
		for _, artifact := range p.ArtifactRefs {
			if api.Equal(artifact, request.ArtifactRef) {
				artifactPresent = true
			}
		}
		if !artifactPresent {
			return "", api.E("revision_conflict", "completion_check_artifact_mismatch")
		}
		out, err := s.AttachTx(ctx, tx, auth, api.Command{TargetID: t.Task.TaskID}, request)
		if err != nil {
			return "", err
		}
		present := false
		for _, ref := range refs {
			present = present || api.Equal(ref, out.CheckRequestRef)
		}
		if !present {
			refs = append(refs, out.CheckRequestRef)
		}
	}
	input := CompleteInput{TaskID: t.Task.TaskID, ExpectedGoalRevision: t.Task.GoalRevision, ArtifactRefs: append([]api.ContentRef{}, p.ArtifactRefs...), Limitations: append([]string{}, p.Limitations...)}
	err := tx.Savepoint(ctx, func(inner runtime.Tx) error {
		_, err := s.CompleteTx(ctx, inner, auth, input)
		return err
	})
	if err == nil {
		return "completed", nil
	}
	if len(refs) == 0 || !(api.IsCode(err, "invalid_state") || api.IsCode(err, "effect_unknown") || api.IsCode(err, "dependency_unavailable")) {
		return "", err
	}
	current, err := getTask(ctx, tx, t.Task.TaskID)
	if err != nil {
		return "", err
	}
	intent := completionIntent{Revision: 1, DecisionID: p.DecisionID, TaskID: current.Task.TaskID, GoalRevision: current.Task.GoalRevision, ControlRevision: current.Task.ControlRevision, Input: input, CheckRequestRefs: refs, State: "pending"}
	if err = tx.Create(ctx, completionIntents, p.DecisionID, current.Task.TaskID, intent); err != nil {
		return "", err
	}
	current.PendingCompletionID = p.DecisionID
	ref := tx.Scope().Ref(p.DecisionID, 1)
	if len(current.Task.WaitReasons) >= 100 {
		return "", api.E("overloaded", "wait_reason_capacity")
	}
	current.Task.WaitReasons = append(current.Task.WaitReasons, api.WaitReason{Kind: "evidence", ObjectRef: &ref, ResumeCondition: "accurate pending checks and current completion gates"})
	if err = s.saveTask(ctx, tx, &current); err != nil {
		return "", err
	}
	return "awaiting_checks", nil
}

// resumeCompletionTx只沿原成果和目标/控制处理，pending观察不生成新Decision。
// 返回true表示本轮继续保留等待责任或已完成；实际检查/效果归并会再次唤醒advance。
func (s *Service) resumeCompletionTx(ctx context.Context, tx runtime.Tx, t *taskState) (bool, error) {
	if t.PendingCompletionID == "" {
		return false, nil
	}
	var pending completionIntent
	if _, err := tx.Get(ctx, completionIntents, t.PendingCompletionID, &pending); err != nil {
		return false, err
	}
	closeIntent := func(state string) error {
		pending.State = state
		pending.Revision++
		if err := tx.Put(ctx, completionIntents, pending.DecisionID, pending.Revision-1, pending); err != nil {
			return err
		}
		t.PendingCompletionID = ""
		waits := []api.WaitReason{}
		for _, reason := range t.Task.WaitReasons {
			if reason.ObjectRef == nil || reason.ObjectRef.ObjectID != pending.DecisionID {
				waits = append(waits, reason)
			}
		}
		t.Task.WaitReasons = waits
		return s.saveTask(ctx, tx, t)
	}
	if terminal(*t) || pending.GoalRevision != t.Task.GoalRevision || pending.ControlRevision != t.Task.ControlRevision {
		return false, closeIntent("superseded")
	}
	if t.Task.Control != "running" {
		return true, nil
	}
	for _, ref := range pending.CheckRequestRefs {
		var request CheckRequest
		if _, err := tx.Get(ctx, checkRequests, ref.ObjectID, &request); err != nil {
			return false, err
		}
		if request.Input.TaskID != t.Task.TaskID || request.Input.GoalRevision != pending.GoalRevision {
			return false, api.E("idempotency_conflict", "completion_check_source_changed")
		}
		if request.State != "checked" {
			return true, nil
		}
	}
	err := tx.Savepoint(ctx, func(inner runtime.Tx) error {
		_, err := s.CompleteTx(ctx, inner, serviceAuth(tx.Scope()), pending.Input)
		return err
	})
	if err == nil {
		pending.State = "completed"
		pending.Revision++
		if err = tx.Put(ctx, completionIntents, pending.DecisionID, pending.Revision-1, pending); err != nil {
			return false, err
		}
		*t, err = getTask(ctx, tx, t.Task.TaskID)
		return true, err
	}
	if api.IsCode(err, "effect_unknown") || api.IsCode(err, "dependency_unavailable") {
		return true, nil
	}
	if !api.IsCode(err, "invalid_state") && !api.IsCode(err, "revision_conflict") {
		return false, err
	}
	t.NoProgress++
	return false, closeIntent("rejected")
}
