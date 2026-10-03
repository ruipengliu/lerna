package task

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) controlTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ControlInput) (TaskOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return TaskOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return TaskOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return TaskOutput{}, e
	}
	if e = cas(c, t); e != nil {
		return TaskOutput{}, e
	}
	if in.Reason == "" {
		return TaskOutput{}, invalid("reason_required")
	}
	desired := ""
	switch c.Method {
	case "task.pause":
		desired = "paused"
	case "task.resume":
		desired = "running"
	case "task.cancel":
		desired = "cancelled"
	default:
		return TaskOutput{}, invalid("unknown_control")
	}
	if terminal(t) {
		if t.Task.Status == "cancelled" && desired == "cancelled" {
			return output(tx, t), nil
		}
		return TaskOutput{}, api.E("invalid_state", "target_terminal")
	}
	if desired == "cancelled" {
		t.Task.Status = "cancelled"
	} else if t.Task.Control == desired {
		return output(tx, t), nil
	} else {
		t.Task.Control = desired
	}
	t.Task.ControlRevision++
	if e = s.lockTaskTree(ctx, tx, t.Task.TaskID); e != nil {
		return TaskOutput{}, e
	}
	if e = s.closeUnsent(ctx, tx, &t); e != nil {
		return TaskOutput{}, e
	}
	if e = s.controlDescendants(ctx, tx, &t, desired == "cancelled"); e != nil {
		return TaskOutput{}, e
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return TaskOutput{}, e
	}
	if e = s.controlJobs(ctx, tx, t); e != nil {
		return TaskOutput{}, e
	}
	if e = queueJob(ctx, tx, JobAdvance, "advance/"+in.TaskID, taskRef(tx, t)); e != nil {
		return TaskOutput{}, e
	}
	return output(tx, t), nil
}

// controlDescendants 持有原 owner 完整且有界子树；不从公开分页推断控制范围。
func (s *Service) controlDescendants(ctx context.Context, tx runtime.Tx, root *taskState, cancel bool) error {
	queue := []string{root.Task.TaskID}
	seen := map[string]bool{root.Task.TaskID: true}
	count := uint64(0)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		rows, e := s.fullRelations(ctx, tx, id)
		if e != nil {
			return e
		}
		for _, r := range rows {
			if r.Kind == "delegation" {
				var d Delegation
				if _, e = tx.Get(ctx, delegations, r.Ref.ObjectID, &d); e != nil {
					return e
				}
				if cancel && !d.CloseRequested {
					d.CloseRequested = true
					d.Revision++
					if e = tx.Put(ctx, delegations, d.DelegationID, d.Revision-1, d); e != nil {
						return e
					}
					if e = queueJob(ctx, tx, JobDelegation, "delegation/"+d.DelegationID, tx.Scope().Ref(d.DelegationID, d.Revision)); e != nil {
						return e
					}
				}
				continue
			}
			if r.Kind != "child" || seen[r.Ref.ObjectID] {
				continue
			}
			seen[r.Ref.ObjectID] = true
			child, e := getTask(ctx, tx, r.Ref.ObjectID)
			if e != nil {
				return e
			}
			if terminal(child) {
				continue
			}
			count++
			if count > s.config.MaxActiveSubtree {
				return api.E("dependency_unavailable", "subtree_index_capacity")
			}
			queue = append(queue, child.Task.TaskID)
			if cancel {
				child.Task.Status = "cancelled"
			}
			child.Task.ControlRevision++
			if e = s.closeUnsent(ctx, tx, &child); e != nil {
				return e
			}
			if e = s.saveTask(ctx, tx, &child); e != nil {
				return e
			}
			if e = s.controlJobs(ctx, tx, child); e != nil {
				return e
			}
		}
	}
	return nil
}

type operationDispatch struct {
	OperationID       string               `json:"operation_id"`
	Revision          uint64               `json:"revision"`
	Sent              bool                 `json:"sent"`
	PermanentlyClosed bool                 `json:"permanently_closed"`
	ReceiptKnown      bool                 `json:"receipt_known"`
	Window            *api.ControlSnapshot `json:"window,omitempty"`
}

const dispatches = "task.operation_dispatches"

func (s *Service) closeUnsent(ctx context.Context, tx runtime.Tx, t *taskState) error {
	rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
	if e != nil {
		return e
	}
	for _, r := range rows {
		if r.Kind != "operation" || r.Closed {
			continue
		}
		var d operationDispatch
		if _, e = tx.Get(ctx, dispatches, r.Ref.ObjectID, &d); e != nil {
			return e
		}
		if d.Sent {
			continue
		}
		d.PermanentlyClosed = true
		d.Revision++
		if e = tx.Put(ctx, dispatches, d.OperationID, d.Revision-1, d); e != nil {
			return e
		}
		r.Closed = true
		r.MayApplyLater = false
		r.Effect = "not_applied"
		r.Revision++
		if e = tx.Put(ctx, relations, r.ID, r.Revision-1, r); e != nil {
			return e
		}
	}
	return s.updateSummary(ctx, tx, t)
}
func (s *Service) reviseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ReviseInput) (TaskOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return TaskOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return TaskOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return TaskOutput{}, e
	}
	if e = cas(c, t); e != nil {
		return TaskOutput{}, e
	}
	if terminal(t) {
		return TaskOutput{}, api.E("invalid_state", "target_terminal")
	}
	if in.BaseGoalRevision != t.Task.GoalRevision {
		return TaskOutput{}, api.E("revision_conflict", "goal_changed")
	}
	if t.PendingGoalCommand != "" {
		return TaskOutput{}, api.E("invalid_state", "goal_update_pending")
	}
	if e = s.lockTaskTree(ctx, tx, t.Task.TaskID); e != nil {
		return TaskOutput{}, e
	}
	if e = s.authorize(ctx, tx, auth, "task.revise", []api.ContentRef{in.GoalRef}, []api.ObjectRef{in.SourceRef}); e != nil {
		return TaskOutput{}, e
	}
	if !auth.HasRole("service") && in.SourceRef.ObjectID != c.CommandID && in.SourceRef.ObjectID != auth.SubjectID {
		return TaskOutput{}, api.E("forbidden", "untrusted_revision")
	}
	t.Task.GoalRef = in.GoalRef
	t.InitialGoalRef = in.GoalRef
	t.Amendments = []api.ContentRef{}
	t.SourceRefs = []api.SourceEvidence{{ContentRef: in.GoalRef, SourceKind: "user_input", SubmissionRef: &in.SourceRef}}
	if e = s.reviseGoal(ctx, tx, &t, in.SourceRef); e != nil {
		return TaskOutput{}, e
	}
	return output(tx, t), nil
}
func (s *Service) reviseGoal(ctx context.Context, tx runtime.Tx, t *taskState, cause api.ObjectRef) error {
	t.PendingCompletionID = ""
	t.Task.GoalRevision++
	t.Task.ControlRevision++
	t.Task.Requirements = []api.Requirement{}
	t.SemanticKeys = []string{}
	digest, e := requirementsDigest(t.Task.Requirements)
	if e != nil {
		return e
	}
	t.Task.RequirementsDigest = digest
	t.Task.RequirementsState = "collecting"
	t.Task.CurrentCoverageRef = nil
	t.CurrentArtifactRefs = []api.ContentRef{}
	t.NoProgress = 0
	t.ContextRounds = 0
	if e = s.closeUnsent(ctx, tx, t); e != nil {
		return e
	}
	if e = s.controlDescendants(ctx, tx, t, true); e != nil {
		return e
	}
	if e = s.saveTask(ctx, tx, t); e != nil {
		return e
	}
	if e = s.saveGoal(ctx, tx, *t, "user_revision", cause); e != nil {
		return e
	}
	if e = s.controlJobs(ctx, tx, *t); e != nil {
		return e
	}
	e = queueJob(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, *t))
	return e
}
func (s *Service) steerTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SteerInput) (TaskOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return TaskOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return TaskOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return TaskOutput{}, e
	}
	if terminal(t) {
		return TaskOutput{}, api.E("invalid_state", "target_terminal")
	}
	if in.BaseGoalRevision != t.Task.GoalRevision {
		return TaskOutput{}, api.E("revision_conflict", "goal_changed")
	}
	if t.PendingGoalCommand != "" {
		return TaskOutput{}, api.E("invalid_state", "goal_update_pending")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return TaskOutput{}, e
	}
	deadline, e := api.ParseTime(in.PrepareDeadline)
	if e != nil || !now.Before(deadline) {
		return TaskOutput{}, invalid("invalid_prepare_deadline")
	}
	if e = s.authorize(ctx, tx, auth, "task.steer", []api.ContentRef{in.AmendmentRef}, []api.ObjectRef{in.SourceSubmissionRef}); e != nil {
		return TaskOutput{}, e
	}
	pending := pendingSteer{Input: in, Revision: 1, CommandID: c.CommandID, SubjectID: auth.SubjectID, UploadID: api.NewID("upload"), State: "pending"}
	if e = tx.Create(ctx, steers, c.CommandID, in.TaskID, pending); e != nil {
		return TaskOutput{}, e
	}
	if len(t.Amendments) >= 100 {
		return TaskOutput{}, invalid("goal_amendment_limit")
	}
	t.PendingGoalCommand = c.CommandID
	t.Task.RequirementsState = "collecting"
	t.Task.ControlRevision++
	if e = s.closeUnsent(ctx, tx, &t); e != nil {
		return TaskOutput{}, e
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return TaskOutput{}, e
	}
	if e = s.controlJobs(ctx, tx, t); e != nil {
		return TaskOutput{}, e
	}
	if e = queueJob(ctx, tx, JobSteer, "steer/"+c.CommandID, tx.Scope().Ref(c.CommandID, 1)); e != nil {
		return TaskOutput{}, e
	}
	return output(tx, t), nil
}
func (s *Service) CreateInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID string, req api.InputRequest) (api.ObjectRef, error) {
	if err := runtime.CheckRef(tx.Scope(), req.TargetRef); err != nil {
		return api.ObjectRef{}, err
	}
	t, e := getTask(ctx, tx, taskID)
	if e != nil {
		return api.ObjectRef{}, e
	}
	if e = principal(auth, t); e != nil {
		return api.ObjectRef{}, e
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return api.ObjectRef{}, e
	}
	if req.TargetRef.ObjectID != taskID || req.TargetRef.OwnerID != tx.Scope().OwnerID || req.GoalRevision == nil || *req.GoalRevision != t.Task.GoalRevision || req.State != "pending" {
		return api.ObjectRef{}, invalid("invalid_input_request")
	}
	if _, ok := s.answerSchemas[componentKey(req.AnswerSchemaRef)]; !ok {
		return api.ObjectRef{}, api.E("unsupported", "answer_schema_not_registered")
	}
	req.State = "pending"
	// 本owner固定准确创建时版本，其中包含已经提交的内部子Task根路径。
	req.TargetRef = taskRef(tx, t)
	req.Revision = 1
	req.OwnerID = tx.Scope().OwnerID
	req.TenantID = tx.Scope().TenantID
	if e = api.ValidateRecord("InputRequest", req); e != nil {
		return api.ObjectRef{}, e
	}
	if e = tx.Create(ctx, inputs, req.RequestID, taskID, req); e != nil {
		return api.ObjectRef{}, e
	}
	if req.Purpose == "clarify_goal" {
		t.Task.RequirementsState = "awaiting_input"
	}
	t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: "input", ObjectRef: func() *api.ObjectRef { r := tx.Scope().Ref(req.RequestID, req.Revision); return &r }(), ResumeCondition: "exact request answered"})
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return api.ObjectRef{}, e
	}
	return tx.Scope().Ref(req.RequestID, 1), nil
}
func (s *Service) consumeInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in InputAnswer, completeGoal *api.ContentRef) (InputOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return InputOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return InputOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return InputOutput{}, e
	}
	if terminal(t) {
		return InputOutput{}, api.E("invalid_state", "target_terminal")
	}
	if in.GoalRevision != t.Task.GoalRevision {
		return InputOutput{}, api.E("revision_conflict", "wrong_goal")
	}
	if e = s.lockTaskTree(ctx, tx, t.Task.TaskID); e != nil {
		return InputOutput{}, e
	}
	var req api.InputRequest
	rev, e := tx.Get(ctx, inputs, in.RequestRef.ObjectID, &req)
	if e != nil {
		return InputOutput{}, e
	}
	if in.RequestRef.OwnerID != tx.Scope().OwnerID || in.RequestRef.TenantID != tx.Scope().TenantID || in.RequestRef.Revision != rev || req.TargetRef.ObjectID != in.TaskID || req.GoalRevision == nil || *req.GoalRevision != in.GoalRevision {
		return InputOutput{}, api.E("revision_conflict", "wrong_request_version")
	}
	if req.State != "pending" {
		return InputOutput{}, api.E("invalid_state", "already_consumed")
	}
	if req.Purpose == "accept_quality" {
		return InputOutput{}, api.E("invalid_state", "acceptance_method_required")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return InputOutput{}, e
	}
	expiry, e := api.ParseTime(req.ExpiresAt)
	if e != nil || !now.Before(expiry) {
		return InputOutput{}, api.E("expired", "request_expired")
	}
	if e = s.authorize(ctx, tx, auth, "task.input", []api.ContentRef{in.AnswerRef}, []api.ObjectRef{in.RequestRef}); e != nil {
		return InputOutput{}, e
	}
	req.State = "answered"
	req.AnswerRef = &in.AnswerRef
	req.ConsumedBy = c.CommandID
	req.AnsweredAt = api.Time(now)
	req.Revision++
	if e = tx.Put(ctx, inputs, req.RequestID, rev, req); e != nil {
		return InputOutput{}, e
	}
	t.Amendments = append(t.Amendments, in.AnswerRef)
	t.SourceRefs = append(t.SourceRefs, api.SourceEvidence{ContentRef: in.AnswerRef, SourceKind: "user_input", SubmissionRef: &in.RequestRef})
	t.Task.WaitReasons = []api.WaitReason{}
	if req.Purpose == "clarify_goal" {
		if completeGoal == nil {
			return InputOutput{}, api.E("dependency_unavailable", "complete_goal_pending")
		}
		if e = contentScope(tx.Scope(), *completeGoal); e != nil {
			return InputOutput{}, e
		}
		t.Task.GoalRef = *completeGoal
		if e = s.reviseGoal(ctx, tx, &t, in.RequestRef); e != nil {
			return InputOutput{}, e
		}
	} else {
		t.Task.ControlRevision++
		t.NoProgress = 0
		if e = s.closeUnsent(ctx, tx, &t); e != nil {
			return InputOutput{}, e
		}
		if e = s.saveTask(ctx, tx, &t); e != nil {
			return InputOutput{}, e
		}
		if e = queueJob(ctx, tx, JobAdvance, "advance/"+in.TaskID, taskRef(tx, t)); e != nil {
			return InputOutput{}, e
		}
	}

	return InputOutput{TaskRef: taskRef(tx, t), RequestRef: in.RequestRef, ConsumedRequestRef: func() *api.ObjectRef { r := tx.Scope().Ref(req.RequestID, req.Revision); return &r }(), State: "consumed"}, nil
}
func (s *Service) ControlWindowTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ControlWindowInput) (api.ControlSnapshot, error) {
	if e := target(c, in.TaskID); e != nil {
		return api.ControlSnapshot{}, e
	}
	if !auth.HasRole("executor") && !auth.HasRole("service") {
		return api.ControlSnapshot{}, api.E("forbidden", "executor_identity_required")
	}
	if auth.SubjectID != in.ReceiverID && !auth.HasRole("service") {
		return api.ControlSnapshot{}, api.E("forbidden", "executor_mismatch")
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return api.ControlSnapshot{}, e
	}
	if t.Task.GoalRevision != in.ExpectedGoalRevision || t.Task.ControlRevision != in.ExpectedControlRevision {
		return api.ControlSnapshot{}, api.E("revision_conflict", "control_changed")
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return api.ControlSnapshot{}, e
	}
	if e = runtime.CheckRef(tx.Scope(), in.OperationRef); e != nil {
		return api.ControlSnapshot{}, e
	}
	if in.OperationRef.OwnerID != in.ReceiverID {
		return api.ControlSnapshot{}, api.E("forbidden", "operation_receiver_mismatch")
	}
	var intent OperationIntent
	if _, e = tx.Get(ctx, intents, in.OperationRef.ObjectID, &intent); e != nil {
		return api.ControlSnapshot{}, e
	}
	if intent.TaskRef.ObjectID != in.TaskID || intent.ExecutorID != in.ReceiverID || intent.GoalRevision != t.Task.GoalRevision || intent.ControlRevision != t.Task.ControlRevision {
		return api.ControlSnapshot{}, api.E("revision_conflict", "operation_control_stale")
	}
	var dispatch operationDispatch
	if _, e = tx.Get(ctx, dispatches, intent.OperationID, &dispatch); e != nil {
		return api.ControlSnapshot{}, e
	}
	if dispatch.PermanentlyClosed {
		return api.ControlSnapshot{}, api.E("invalid_state", "operation_entry_closed")
	}
	var relation relation
	if _, e = tx.Get(ctx, relations, relationID(in.TaskID, "operation", in.ReceiverID, intent.OperationID), &relation); e != nil {
		return api.ControlSnapshot{}, e
	}
	if relation.Closed {
		return api.ControlSnapshot{}, api.E("invalid_state", "operation_entry_closed")
	}
	found := false
	for _, owner := range t.ControlTargets {
		if owner == in.ReceiverID {
			found = true
		}
	}
	if !found {
		return api.ControlSnapshot{}, api.E("forbidden", "unknown_control_target")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return api.ControlSnapshot{}, e
	}
	window, e := s.controlSnapshot(ctx, tx, t, now)
	if e != nil {
		return window, e
	}
	if e = tx.Create(ctx, windows, window.WindowID, t.Task.TaskID, window); e != nil {
		return window, e
	}
	return window, nil
}
func (s *Service) controlSnapshot(ctx context.Context, tx runtime.Tx, t taskState, now time.Time) (api.ControlSnapshot, error) {
	if s.ports.ControlProof == nil {
		return api.ControlSnapshot{}, api.E("unsupported", "control_proof_not_configured")
	}
	control, status := t.Task.Control, t.Task.Status
	for _, id := range t.Ancestors {
		a, e := getTask(ctx, tx, id)
		if e != nil {
			return api.ControlSnapshot{}, e
		}
		if terminal(a) {
			status = "cancelled"
		}
		if a.Task.Control == "paused" {
			control = "paused"
		}
	}
	before := now.Add(s.config.ControlWindow)
	deadline, e := api.ParseTime(t.Task.Deadline)
	if e != nil {
		return api.ControlSnapshot{}, e
	}
	// 负控制只传播停止事实，不授予行动入口；目标截止后仍可签有限停止窗口。
	if status == "active" && control == "running" && deadline.Before(before) {
		before = deadline
	}
	if !now.Before(before) {
		return api.ControlSnapshot{}, api.E("expired", "control_window_expired")
	}
	window := api.ControlSnapshot{OrchestratorID: tx.Scope().OwnerID, TaskID: t.Task.TaskID, GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, Status: status, Control: control, IssuedAt: api.Time(now), StartBefore: api.Time(before), WindowID: api.NewID("window")}
	window.ProofRef, e = s.ports.ControlProof.SealControl(ctx, tx, window)
	if e != nil {
		return api.ControlSnapshot{}, e
	}
	if e = s.checkSourceProof(tx.Scope(), window.ProofRef); e != nil {
		return api.ControlSnapshot{}, e
	}
	if e = api.ValidateRecord("ControlSnapshot", window); e != nil {
		return api.ControlSnapshot{}, e
	}
	return window, nil
}

func (s *Service) checkSourceProof(scope runtime.Scope, ref api.ContentRef) error {
	if err := api.ValidateRecord("ContentRef", ref); err != nil {
		return err
	}
	if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID {
		return api.E("forbidden", "source_proof_owner_mismatch")
	}
	return nil
}
func (s *Service) expireTx(ctx context.Context, tx runtime.Tx, t *taskState) (bool, error) {
	if terminal(*t) {
		return false, nil
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return false, e
	}
	deadline, e := api.ParseTime(t.Task.Deadline)
	if e != nil {
		return false, e
	}
	if now.Before(deadline) {
		return false, nil
	}
	if e = s.lockTaskTree(ctx, tx, t.Task.TaskID); e != nil {
		return false, e
	}
	t.Task.Status = "failed"
	t.Task.ControlRevision++
	t.Task.WaitReasons = []api.WaitReason{{Kind: "dependency", ResumeCondition: "goal closed: deadline_exceeded"}}
	if e = s.closeUnsent(ctx, tx, t); e != nil {
		return false, e
	}
	if e = s.controlDescendants(ctx, tx, t, true); e != nil {
		return false, e
	}
	if e = s.saveTask(ctx, tx, t); e != nil {
		return false, e
	}
	if e = s.controlJobs(ctx, tx, *t); e != nil {
		return false, e
	}
	return true, nil
}
func goalID(taskID string, revision uint64) string { return fmt.Sprintf("%s/%020d", taskID, revision) }
