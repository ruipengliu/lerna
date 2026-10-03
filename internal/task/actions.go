package task

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) PrepareDecision(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in PreparedDecision) (api.DecisionDispatchIntent, error) {
	var out api.DecisionDispatchIntent
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { var e error; out, e = s.PrepareDecisionTx(ctx, tx, auth, in); return e })
	return out, err
}
func (s *Service) PrepareDecisionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in PreparedDecision) (api.DecisionDispatchIntent, error) {
	if !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return api.DecisionDispatchIntent{}, api.E("forbidden", "trusted_context_compiler_required")
	}
	var existing decisionState
	_, e := tx.Get(ctx, decisions, in.DecisionID, &existing)
	if e == nil {
		if !api.Equal(existing.Snapshot, in.Snapshot) || existing.Intent.CommandID != in.CommandID || existing.Intent.BrainOwnerID != in.BrainOwnerID {
			return api.DecisionDispatchIntent{}, api.E("idempotency_conflict", "digest_conflict")
		}
		return existing.Intent, nil
	}
	if !api.IsCode(e, "not_found") {
		return api.DecisionDispatchIntent{}, e
	}
	t, e := getTask(ctx, tx, in.Snapshot.TaskRef.ObjectID)
	if e != nil {
		return api.DecisionDispatchIntent{}, e
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return api.DecisionDispatchIntent{}, e
	}
	if t.PendingCompletionID != "" {
		return api.DecisionDispatchIntent{}, api.E("invalid_state", "completion_checks_pending")
	}
	snap := in.Snapshot
	if snap.TaskRef.TenantID != tx.Scope().TenantID || snap.TaskRef.OwnerID != tx.Scope().OwnerID || snap.TaskRef.Revision != t.Task.Revision || snap.GoalRevision != t.Task.GoalRevision || snap.ControlRevision != t.Task.ControlRevision || !api.Equal(snap.GoalRef, t.Task.GoalRef) || snap.RequirementsDigest != t.Task.RequirementsDigest || !api.Equal(snap.Requirements, t.Task.Requirements) || !api.Equal(snap.PolicyRef, t.Task.PolicyRef) {
		return api.DecisionDispatchIntent{}, api.E("revision_conflict", "stale_snapshot")
	}
	if snap.Purpose != "interpret_requirements" && snap.Purpose != "decide" {
		return api.DecisionDispatchIntent{}, invalid("invalid_snapshot_purpose")
	}
	if snap.Purpose == "decide" && t.Task.RequirementsState != "ready" {
		return api.DecisionDispatchIntent{}, api.E("invalid_state", "requirements_not_ready")
	}
	if t.Task.RequirementsState == "awaiting_input" {
		return api.DecisionDispatchIntent{}, api.E("invalid_state", "input_pending")
	}
	if snap.CountMode != "exact" && snap.CountMode != "upper_bound" && snap.CountMode != "estimate" {
		return api.DecisionDispatchIntent{}, invalid("invalid_count_mode")
	}
	if t.Policy.CostMode == "strict" && snap.CountMode == "estimate" {
		return api.DecisionDispatchIntent{}, api.E("forbidden", "unbounded_model_encoding")
	}
	if !api.ValidID(in.DecisionID) || !api.ValidID(in.CommandID) || !api.ValidID(in.BrainOwnerID) || !api.ValidID(snap.SnapshotID) || snap.Revision != 1 {
		return api.DecisionDispatchIntent{}, invalid("invalid_decision_identity")
	}

	if t.Continuations >= t.Policy.ContinuationLimit {
		return api.DecisionDispatchIntent{}, api.E("invalid_state", "continuation_limit")
	}
	if t.NoProgress >= t.Policy.NoProgressLimit {
		return api.DecisionDispatchIntent{}, api.E("invalid_state", "no_progress_limit")
	}
	if e = s.authorize(ctx, tx, auth, "task.snapshot", append([]api.ContentRef{snap.GoalRef, in.SnapshotRef}, snap.MaterialRefs...), snap.BindingRefs); e != nil {
		return api.DecisionDispatchIntent{}, e
	}
	intent := api.DecisionDispatchIntent{DecisionID: in.DecisionID, TaskRef: taskRef(tx, t), SnapshotRef: in.SnapshotRef, SnapshotRevision: snap.Revision, ModelProfileRef: snap.ModelProfileRef, BrainOwnerID: in.BrainOwnerID, CommandID: in.CommandID}
	intent.IntentHash, e = api.Digest(intent)
	if e != nil {
		return intent, e
	}
	if e = api.ValidateRecord("DecisionDispatchIntent", intent); e != nil {
		return intent, e
	}
	source := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: in.BrainOwnerID, ObjectID: in.DecisionID, Revision: 1}
	r, e := s.reserveTx(ctx, tx, &t, "brain_decision", source, in.CostBound)
	if e != nil {
		return intent, e
	}
	d := decisionState{Intent: intent, Snapshot: snap, ReservationID: r.ReservationID, Revision: 1}
	if e = tx.Create(ctx, decisions, in.DecisionID, t.Task.TaskID, d); e != nil {
		return intent, e
	}
	t.Continuations++
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return intent, e
	}
	if _, e = raise(ctx, tx, JobDispatchDecision, "decision/"+in.DecisionID, tx.Scope().Ref(in.DecisionID, 1)); e != nil {
		return intent, e
	}
	return intent, nil
}
func (s *Service) ConsumeProposal(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, p Proposal, report *ValidationReport) (Consumption, error) {
	var out Consumption
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		var e error
		out, e = s.ConsumeProposalTx(ctx, tx, auth, p, report)
		return e
	})
	return out, err
}
func (s *Service) ConsumeProposalTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, p Proposal, report *ValidationReport) (Consumption, error) {
	if !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return Consumption{}, api.E("forbidden", "trusted_preparer_required")
	}
	var d decisionState
	if _, e := tx.Get(ctx, decisions, p.DecisionID, &d); e != nil {
		return Consumption{}, e
	}
	t, e := getTask(ctx, tx, d.Snapshot.TaskRef.ObjectID)
	if e != nil {
		return Consumption{}, e
	}
	digest, e := api.Digest(p)
	if e != nil {
		return Consumption{}, e
	}
	key := t.Task.TaskID + "/" + p.DecisionID
	binding, e := tx.LookupKey(ctx, consumptions, key)
	if e == nil {
		if binding.Digest != digest {
			return Consumption{}, api.E("idempotency_conflict", "digest_conflict")
		}
		var out Consumption
		_, e = tx.Get(ctx, consumptions, binding.ObjectID, &out)
		return out, e
	}
	if !api.IsCode(e, "not_found") {
		return Consumption{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return Consumption{}, e
	}
	out := Consumption{TaskID: t.Task.TaskID, DecisionID: p.DecisionID, SnapshotRef: d.Intent.SnapshotRef, ExpectedGoalRevision: d.Snapshot.GoalRevision, ExpectedControlRevision: d.Snapshot.ControlRevision, Outcome: "adopted", AdmittedOperationIDs: []string{}, ReasonRef: p.ReasonRef, ReasonCodes: []string{}, DecidedAt: api.Time(now)}
	finish := func() (Consumption, error) {
		if e = tx.Create(ctx, consumptions, key, t.Task.TaskID, out); e != nil {
			return out, e
		}
		if e = tx.Bind(ctx, consumptions, key, key, digest); e != nil {
			return out, e
		}
		d.Consumed = true
		d.Revision++
		if e = tx.Put(ctx, decisions, p.DecisionID, d.Revision-1, d); e != nil {
			return out, e
		}
		return out, nil
	}
	if terminal(t) || t.Task.Control != "running" || d.Snapshot.GoalRevision != t.Task.GoalRevision || d.Snapshot.ControlRevision != t.Task.ControlRevision || t.PendingGoalCommand != "" || t.PendingCompletionID != "" {
		out.Outcome = "stale"
		out.ReasonCodes = append(out.ReasonCodes, "stale_snapshot")
		return finish()
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		out.Outcome = "stale"
		out.ReasonCodes = append(out.ReasonCodes, "current_gate_closed")
		return finish()
	}
	if p.RequirementDelta != nil {
		if report == nil {
			return out, api.E("dependency_unavailable", "requirement_validation_pending")
		}
		adoption, e := s.AdoptRequirementsTx(ctx, tx, auth, t.Task.TaskID, "decision", tx.Scope().Ref(p.DecisionID, 1), *p.RequirementDelta, *report)
		if e != nil {
			return out, e
		}
		out.AdoptionID = adoption.AdoptionID
		if adoption.Outcome == "accepted" {
			out.Outcome = "requirements_changed"
			return finish()
		}
		if adoption.Outcome == "rejected" {
			out.Outcome = "rejected"
			out.ReasonCodes = adoption.ReasonCodes
			return finish()
		}
	}
	switch p.Kind {
	case "refine_requirements":
		if p.RequirementDelta == nil {
			out.Outcome = "rejected"
			out.ReasonCodes = []string{"requirement_delta_required"}
		}
	case "act":
		businessErr := tx.Savepoint(ctx, func(inner runtime.Tx) error {
			var err error
			out.AdmittedOperationIDs, err = s.admitBatchTx(ctx, inner, auth, &t, d, p.Actions)
			return err
		})
		if businessErr != nil {
			if _, ok := businessErr.(*api.Error); !ok {
				return out, businessErr
			}
			out.Outcome = "rejected"
			out.AdmittedOperationIDs = []string{}
			out.ReasonCodes = []string{businessErr.Error()}
		}
	case "complete":
		businessErr := tx.Savepoint(ctx, func(inner runtime.Tx) error {
			var err error
			out.Outcome, err = s.consumeCompletionTx(ctx, inner, auth, t, p)
			return err
		})
		if businessErr != nil {
			if _, ok := businessErr.(*api.Error); !ok || api.IsCode(businessErr, "dependency_unavailable") || api.IsCode(businessErr, "accounting_unknown") {
				return out, businessErr
			}
			out.Outcome = "rejected"
			out.ReasonCodes = []string{businessErr.Error()}
		}
	case "request_input":
		if p.InputRequest == nil {
			out.Outcome = "rejected"
			out.ReasonCodes = []string{"input_request_required"}
		} else {
			if _, e = s.CreateInputTx(ctx, tx, auth, t.Task.TaskID, *p.InputRequest); e != nil {
				return out, e
			}
		}
	case "need_context":
		if t.ContextRounds >= t.Policy.ContextRoundLimit {
			out.Outcome = "rejected"
			out.ReasonCodes = []string{"context_round_limit"}
		} else {
			if e = s.authorize(ctx, tx, auth, "task.need_context", p.ContextRefs, nil); e != nil {
				return out, e
			}
			t.ContextRounds++
			if e = s.saveTask(ctx, tx, &t); e != nil {
				return out, e
			}
			if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
				return out, e
			}
		}
	case "fail":
		if p.FailureReason == "" {
			out.Outcome = "rejected"
			out.ReasonCodes = []string{"failure_basis_required"}
		} else {
			t.Task.Status = "failed"
			t.Task.ControlRevision++
			t.Task.WaitReasons = []api.WaitReason{{Kind: "failure", ResumeCondition: p.FailureReason}}
			if e = s.closeUnsent(ctx, tx, &t); e != nil {
				return out, e
			}
			if e = s.controlDescendants(ctx, tx, &t, true); e != nil {
				return out, e
			}
			if e = s.saveTask(ctx, tx, &t); e != nil {
				return out, e
			}
			if e = s.controlJobs(ctx, tx, t); e != nil {
				return out, e
			}
		}
	default:
		out.Outcome = "rejected"
		out.ReasonCodes = []string{"proposal_kind_unsupported"}
	}
	if out.Outcome == "rejected" {
		current, e := getTask(ctx, tx, t.Task.TaskID)
		if e != nil {
			return out, e
		}
		current.NoProgress++
		if e = s.saveTask(ctx, tx, &current); e != nil {
			return out, e
		}
	}
	return finish()
}
func (s *Service) admitBatchTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, t *taskState, d decisionState, actions []PreparedAction) ([]string, error) {
	if len(actions) < 1 || len(actions) > 4 {
		return nil, invalid("action_batch_limit")
	}
	if s.ports.ActionAuthorization == nil {
		return nil, api.E("unsupported", "action_authorization_not_configured")
	}
	if e := s.CheckCurrent(ctx, tx, *t, true); e != nil {
		return nil, e
	}
	rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
	if e != nil {
		return nil, e
	}
	if uint64(len(rows)+len(actions)) > s.config.MaxRelations {
		return nil, api.E("overloaded", "relation_capacity")
	}
	activeResources := map[string]bool{}
	for _, r := range rows {
		if r.Kind == "operation" && !r.Closed {
			for _, key := range r.ResourceKeys {
				activeResources[key] = true
			}
		}
	}
	preparedResources := map[string]bool{}
	ids := []string{}
	seen := map[string]bool{}
	for index, a := range actions {
		if !a.Independent || len(a.ResourceKeys) > 32 {
			return nil, invalid("dependent_action_batch")
		}
		if !api.ValidID(a.OperationID) || !api.ValidID(a.ExecutorID) || !api.ValidID(a.CommandID) || a.LogicalStepKey == "" || seen[a.OperationID] {
			return nil, invalid("invalid_action_identity")
		}
		seen[a.OperationID] = true
		for _, ref := range []api.ComponentRef{a.CapabilityRef, a.InstallLockRef} {
			if e = api.ValidateRecord("ComponentRef", ref); e != nil {
				return nil, e
			}
		}
		if e = runtime.CheckRef(tx.Scope(), a.BindingRef); e != nil {
			return nil, e
		}
		for _, key := range a.ResourceKeys {
			if key == "" || len(key) > 512 {
				return nil, invalid("resource_key_invalid")
			}
			if activeResources[key] || preparedResources[key] {
				return nil, api.E("invalid_state", "resource_conflict")
			}
			preparedResources[key] = true
		}
		purpose := "goal_action"
		if t.Task.RequirementsState != "ready" {
			if d.Snapshot.Purpose != "interpret_requirements" || !a.SafeRequirementCheck {
				return nil, api.E("invalid_state", "requirements_not_ready")
			}
			purpose = "requirement_check"
		}
		if e = s.authorize(ctx, tx, auth, "task.action", append([]api.ContentRef{a.ArgumentsRef, a.ResourcesRef}, a.ProcessedSourceRefs...), append([]api.ObjectRef{a.BindingRef}, a.UseIntentRefs...)); e != nil {
			return nil, e
		}
		for _, r := range a.RequirementRefs {
			found := false
			for _, req := range t.Task.Requirements {
				if req.RequirementID == r.RequirementID && req.Revision == r.Revision {
					found = true
				}
			}
			if !found {
				return nil, invalid("unknown_requirement")
			}
		}
		source := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: a.ExecutorID, ObjectID: a.OperationID, Revision: 1}
		reservation, e := s.reserveTx(ctx, tx, t, "execution_operation", source, a.CostBound)
		if e != nil {
			return nil, e
		}
		intent := OperationIntent{PreparedAction: a, TaskRef: taskRef(tx, *t), GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, AdmissionSourceKind: "decision", AdmissionSourceRef: tx.Scope().Ref(d.Intent.DecisionID, 1), SourcePosition: fmt.Sprintf("%d", index), AdmissionPurpose: purpose, ReservationRef: tx.Scope().Ref(reservation.ReservationID, 1), CommandRef: tx.Scope().Ref(a.CommandID, 1), Deadline: t.Task.Deadline}
		intent.IntentHash, e = api.Digest(intent)
		if e != nil {
			return nil, e
		}
		if e = s.ports.ActionAuthorization.AuthorizeAction(ctx, tx, auth, intent); e != nil {
			return nil, e
		}
		if e = tx.Create(ctx, intents, a.OperationID, t.Task.TaskID, intent); e != nil {
			return nil, e
		}
		if e = tx.Create(ctx, dispatches, a.OperationID, t.Task.TaskID, operationDispatch{OperationID: a.OperationID, Revision: 1}); e != nil {
			return nil, e
		}
		relation := relation{ID: relationID(t.Task.TaskID, "operation", a.ExecutorID, a.OperationID), Revision: 1, TaskID: t.Task.TaskID, Kind: "operation", Ref: source, Purpose: purpose, Effect: "unknown", MayApplyLater: true, ResourceKeys: a.ResourceKeys}
		if e = tx.Create(ctx, relations, relation.ID, t.Task.TaskID, relation); e != nil {
			return nil, e
		}
		if _, e = raise(ctx, tx, JobDispatchOperation, "operation/"+a.OperationID, tx.Scope().Ref(a.OperationID, 1)); e != nil {
			return nil, e
		}
		ids = append(ids, a.OperationID)
	}
	if e = s.updateSummary(ctx, tx, t); e != nil {
		return nil, e
	}
	if e = s.saveTask(ctx, tx, t); e != nil {
		return nil, e
	}
	return ids, nil
}

// MergeOperation 接受可信原 Executor 当前事实；未知/迟到效果保持独立于Task终态。
func (s *Service) MergeOperation(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, operation api.Operation) error {
	return s.transaction(ctx, store, scope, func(tx runtime.Tx) error { return s.MergeOperationTx(ctx, tx, auth, operation) })
}
func (s *Service) MergeOperationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, operation api.Operation) error {
	if !auth.HasRole("service") && auth.SubjectID != operation.OwnerID {
		return api.E("forbidden", "executor_identity_required")
	}
	if e := api.ValidateRecord("Operation", operation); e != nil {
		return e
	}
	if operation.TaskRef.TenantID != tx.Scope().TenantID || operation.TaskRef.OwnerID != tx.Scope().OwnerID {
		return api.E("forbidden", "wrong_task_owner")
	}
	t, e := getTask(ctx, tx, operation.TaskRef.ObjectID)
	if e != nil {
		return e
	}
	id := relationID(t.Task.TaskID, "operation", operation.OwnerID, operation.OperationID)
	var r relation
	if _, e = tx.Get(ctx, relations, id, &r); e != nil {
		return e
	}
	digest, e := api.Digest(operation)
	if e != nil {
		return e
	}
	if operation.Revision < r.SourceRevision {
		return nil
	}
	if operation.Revision == r.SourceRevision {
		if digest != r.SourceDigest {
			return api.E("idempotency_conflict", "digest_conflict")
		}
		return nil
	}
	mayApply, ok := operation.MayApplyLater.(bool)
	if !ok {
		if text, valid := operation.MayApplyLater.(string); !valid || text != "unknown" {
			return invalid("invalid_effect_lateness")
		}
		mayApply = true
	}
	r.SourceRevision = operation.Revision
	r.SourceDigest = digest
	r.Ref.Revision = operation.Revision
	r.Closed = operation.ExecutionState == "closed"
	r.Effect = operation.Effect
	r.MayApplyLater = mayApply
	r.Revision++
	if e = tx.Put(ctx, relations, id, r.Revision-1, r); e != nil {
		return e
	}
	if e = s.updateSummary(ctx, tx, &t); e != nil {
		return e
	}
	if r.Closed && r.Effect != "unknown" && !r.MayApplyLater {
		t.NoProgress = 0
	}
	if operation.ResultRef != nil {
		t.CurrentArtifactRefs = appendUniqueContent(t.CurrentArtifactRefs, *operation.ResultRef)
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return e
	}
	if _, e = raise(ctx, tx, JobReconcileOperation, "reconcile/"+operation.OperationID, tx.Scope().Ref(operation.OperationID, operation.Revision)); e != nil {
		return e
	}
	if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
		return e
	}
	return nil
}
