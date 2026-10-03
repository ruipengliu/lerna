package task

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) Complete(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in CompleteInput) (api.Result, error) {
	var out api.Result
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { var e error; out, e = s.CompleteTx(ctx, tx, auth, in); return e })
	return out, err
}
func (s *Service) CompleteTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in CompleteInput) (api.Result, error) {
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return api.Result{}, e
	}
	if e = principal(auth, t); e != nil {
		return api.Result{}, e
	}
	if !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return api.Result{}, api.E("forbidden", "trusted_completion_required")
	}
	if t.Task.Status == "succeeded" {
		var result api.Result
		_, e = tx.Get(ctx, results, t.Task.ResultRef.ObjectID, &result)
		return result, e
	}
	if e = s.CheckCurrent(ctx, tx, t, false); e != nil {
		return api.Result{}, e
	}
	if t.PendingGoalCommand != "" || in.ExpectedGoalRevision != t.Task.GoalRevision {
		return api.Result{}, api.E("revision_conflict", "goal_changed")
	}
	if len(t.Task.Requirements) == 0 || t.Task.RequirementsState != "ready" || t.Task.CurrentCoverageRef == nil {
		return api.Result{}, api.E("invalid_state", "requirements_not_ready")
	}
	if len(in.ArtifactRefs) == 0 || len(in.ArtifactRefs) > 100 || len(in.CheckRefs) > 100 {
		return api.Result{}, invalid("bounded_artifacts_required")
	}
	if e = s.authorize(ctx, tx, auth, "task.complete", in.ArtifactRefs, nil); e != nil {
		return api.Result{}, e
	}
	var coverage api.GoalCoverage
	if e = tx.GetVersion(ctx, coverages, t.Task.CurrentCoverageRef.ObjectID, t.Task.CurrentCoverageRef.Revision, &coverage); e != nil {
		return api.Result{}, e
	}
	if coverage.GoalRevision != t.Task.GoalRevision || !api.Equal(coverage.GoalRef, t.Task.GoalRef) || coverage.RequirementsDigest != t.Task.RequirementsDigest || coverage.Verdict != "pass" || coverage.Applicability != "usable" {
		return api.Result{}, api.E("invalid_state", "coverage_not_current")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return api.Result{}, e
	}
	coverageAt, e := api.ParseTime(coverage.CheckedAt)
	if e != nil || coverageAt.After(now) || now.Sub(coverageAt) > time.Duration(t.Policy.MaxEvidenceStalenessSeconds)*time.Second {
		return api.Result{}, api.E("invalid_state", "coverage_expired")
	}
	selected := []api.ConditionResult{}
	checkRefs := []api.ObjectRef{*t.Task.CurrentCoverageRef}
	implementations := []api.ComponentRef{coverage.RuleRef, coverage.EvaluatorRef}
	basis := "verified"
	requested := map[string]api.ObjectRef{}
	for _, r := range in.CheckRefs {
		if e = runtime.CheckRef(tx.Scope(), r); e != nil {
			return api.Result{}, e
		}
		if r.OwnerID != tx.Scope().OwnerID {
			return api.Result{}, api.E("forbidden", "foreign_check")
		}
		requested[r.ObjectID] = r
	}
	for _, requirement := range t.Task.Requirements {
		if !requirement.Required {
			continue
		}
		key := t.Task.TaskID + "/" + fmt.Sprintf("%020d", t.Task.GoalRevision) + "/" + requirement.RequirementID
		var selection selectedCheck
		if _, e = tx.Get(ctx, selections, key, &selection); e != nil {
			return api.Result{}, api.E("invalid_state", "condition_check_missing")
		}
		if len(requested) > 0 {
			r, ok := requested[selection.Ref.ObjectID]
			if !ok || !api.Equal(r, selection.Ref) {
				return api.Result{}, api.E("revision_conflict", "check_selection_changed")
			}
		}
		var check api.ConditionResult
		if e = tx.GetVersion(ctx, checks, selection.Ref.ObjectID, selection.Ref.Revision, &check); e != nil {
			return api.Result{}, e
		}
		if check.GoalRevision != t.Task.GoalRevision || check.RequirementID != requirement.RequirementID || check.RequirementRevision != requirement.Revision || !api.Equal(check.RuleRef, requirement.RuleRef) || check.Verdict != "pass" || check.Applicability != "usable" {
			return api.Result{}, api.E("invalid_state", "condition_not_passed")
		}
		artifactOK := false
		for _, ref := range in.ArtifactRefs {
			if api.Equal(ref, check.ArtifactRef) {
				artifactOK = true
			}
		}
		if !artifactOK {
			return api.Result{}, api.E("revision_conflict", "artifact_changed")
		}
		observed, e := api.ParseTime(check.ObservedAt)
		if e != nil || observed.After(now) || now.Sub(observed) > time.Duration(t.Policy.MaxEvidenceStalenessSeconds)*time.Second {
			return api.Result{}, api.E("invalid_state", "evidence_expired")
		}
		rule, ok := s.rules[componentKey(check.RuleRef)]
		if !ok {
			return api.Result{}, api.E("dependency_unavailable", "rule_unavailable")
		}
		if rule.MaxObservationAgeSeconds != nil && now.Sub(observed) > time.Duration(*rule.MaxObservationAgeSeconds)*time.Second {
			return api.Result{}, api.E("invalid_state", "observation_expired")
		}
		if check.Basis == "user_accepted" {
			if requirement.Kind != "quality" || !rule.AllowUserAcceptance {
				return api.Result{}, api.E("forbidden", "acceptance_not_allowed")
			}
			basis = "user_accepted"
		} else if check.Basis == "assessed" && basis != "user_accepted" {
			basis = "assessed"
		}
		selected = append(selected, check)
		checkRefs = append(checkRefs, selection.Ref)
		implementations = append(implementations, check.RuleRef, check.EvaluatorRef)
	}
	if len(selected) == 0 {
		return api.Result{}, api.E("invalid_state", "required_conditions_empty")
	}
	if s.ports.Gate == nil {
		return api.Result{}, api.E("dependency_unavailable", "evidence_gate_unavailable")
	}
	if e = s.ports.Gate.Evidence(ctx, tx, t.Task, checkRefs, implementations); e != nil {
		return api.Result{}, e
	}
	related, e := s.fullRelations(ctx, tx, t.Task.TaskID)
	if e != nil {
		return api.Result{}, e
	}
	for _, r := range related {
		switch r.Kind {
		case "operation":
			if !r.Closed || r.MayApplyLater || r.Effect == "unknown" {
				return api.Result{}, api.E("effect_unknown", "target_effect_unclosed")
			}
		case "delegation":
			var d Delegation
			if _, e = tx.Get(ctx, delegations, r.Ref.ObjectID, &d); e != nil {
				return api.Result{}, e
			}
			if !d.GoalWorkClosed || !d.EffectsClosed {
				return api.Result{}, api.E("effect_unknown", "delegation_unclosed")
			}
		case "child":
			var child taskState
			if _, e = tx.Get(ctx, tasks, r.Ref.ObjectID, &child); e != nil {
				return api.Result{}, e
			}
			if !terminal(child) {
				return api.Result{}, api.E("invalid_state", "child_goal_open")
			}
		}
	}
	result := api.Result{TaskID: t.Task.TaskID, GoalRevision: t.Task.GoalRevision, ArtifactRefs: in.ArtifactRefs, CompletionBasis: basis, ConditionResults: selected, CoverageRef: *t.Task.CurrentCoverageRef, Limitations: in.Limitations, CompletedAt: api.Time(now), ResultID: api.NewID("result"), Revision: 1}
	if result.Limitations == nil {
		result.Limitations = []string{}
	}
	if e = api.ValidateRecord("Result", result); e != nil {
		return api.Result{}, e
	}
	if e = tx.Create(ctx, results, result.ResultID, t.Task.TaskID, result); e != nil {
		return api.Result{}, e
	}
	ref := tx.Scope().Ref(result.ResultID, 1)
	publication := resultPublication{ResultRef: ref, Revision: 1, UploadID: api.NewID("upload"), State: "pending", Notices: []string{}}
	if e = tx.Create(ctx, publications, result.ResultID, t.Task.TaskID, publication); e != nil {
		return api.Result{}, e
	}
	t.Task.Status = "succeeded"
	t.Task.ControlRevision++
	t.Task.ResultRef = &ref
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return api.Result{}, e
	}
	if e = s.controlJobs(ctx, tx, t); e != nil {
		return api.Result{}, e
	}
	if _, e = raise(ctx, tx, JobPublishResult, "result/"+result.ResultID, ref); e != nil {
		return api.Result{}, e
	}
	return result, nil
}
func (s *Service) Result(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, taskID string, in ResultInput) (ResultOutput, error) {
	t, e := s.readState(ctx, store, scope, auth, taskID, 0)
	if e != nil {
		return ResultOutput{}, e
	}
	if t.Task.Status != "succeeded" || t.Task.ResultRef == nil {
		return ResultOutput{}, api.E("invalid_state", "result_not_available")
	}
	ref := *t.Task.ResultRef
	if in.ResultRef != nil {
		if !api.Equal(ref, *in.ResultRef) {
			return ResultOutput{}, api.E("revision_conflict", "result_changed")
		}
	}
	var result api.Result
	if _, e = store.Read(ctx, scope, results, ref.ObjectID, ref.Revision, &result); e != nil {
		return ResultOutput{}, e
	}
	var pub resultPublication
	if _, e = store.Read(ctx, scope, publications, ref.ObjectID, 0, &pub); e != nil {
		return ResultOutput{}, e
	}
	return ResultOutput{Result: result, Publication: pub.State, ContentRef: pub.ContentRef, Notices: pub.Notices}, nil
}

const acceptanceRequests = "task.acceptance_requests"

type acceptanceRequest struct {
	RequirementRef api.RequirementRef `json:"requirement_ref"`
	Revision       uint64             `json:"revision"`
}

func (s *Service) CreateAcceptanceRequestTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID string, requirement api.RequirementRef, req api.InputRequest) (api.ObjectRef, error) {
	t, e := getTask(ctx, tx, taskID)
	if e != nil {
		return api.ObjectRef{}, e
	}
	var found *api.Requirement
	for i := range t.Task.Requirements {
		r := &t.Task.Requirements[i]
		if r.RequirementID == requirement.RequirementID && r.Revision == requirement.Revision {
			found = r
		}
	}
	if found == nil || found.Kind != "quality" {
		return api.ObjectRef{}, api.E("forbidden", "acceptance_not_allowed")
	}
	rule, ok := s.rules[componentKey(found.RuleRef)]
	if !ok || !rule.AllowUserAcceptance {
		return api.ObjectRef{}, api.E("forbidden", "acceptance_not_allowed")
	}
	if req.CandidateRef == nil || req.LimitationsRef == nil {
		return api.ObjectRef{}, invalid("acceptance_preview_required")
	}
	req.Purpose = "accept_quality"
	ref, e := s.CreateInputTx(ctx, tx, auth, taskID, req)
	if e != nil {
		return ref, e
	}
	e = tx.Create(ctx, acceptanceRequests, req.RequestID, taskID, acceptanceRequest{RequirementRef: requirement, Revision: 1})
	return ref, e
}
func (s *Service) AcceptTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AcceptInput) (AcceptOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return AcceptOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return AcceptOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return AcceptOutput{}, e
	}
	if terminal(t) {
		return AcceptOutput{}, api.E("invalid_state", "target_terminal")
	}
	if t.Task.GoalRevision != in.GoalRevision {
		return AcceptOutput{}, api.E("revision_conflict", "wrong_goal")
	}
	var req api.InputRequest
	rev, e := tx.Get(ctx, inputs, in.RequestRef.ObjectID, &req)
	if e != nil {
		return AcceptOutput{}, e
	}
	if req.Purpose != "accept_quality" || req.GoalRevision == nil || *req.GoalRevision != in.GoalRevision || req.Revision != in.RequestRef.Revision || req.TargetRef.ObjectID != in.TaskID || in.RequestRef.OwnerID != tx.Scope().OwnerID || in.RequestRef.TenantID != tx.Scope().TenantID {
		return AcceptOutput{}, api.E("revision_conflict", "wrong_request_version")
	}
	if req.State != "pending" {
		return AcceptOutput{}, api.E("invalid_state", "already_consumed")
	}
	if req.CandidateRef == nil || req.LimitationsRef == nil || !api.Equal(*req.CandidateRef, in.CandidateRef) || !api.Equal(*req.LimitationsRef, in.LimitationsRef) {
		return AcceptOutput{}, api.E("revision_conflict", "candidate_changed")
	}
	if e = s.authorize(ctx, tx, auth, "task.accept_result", []api.ContentRef{in.CandidateRef, in.LimitationsRef}, []api.ObjectRef{in.RequestRef}); e != nil {
		return AcceptOutput{}, e
	}
	var binding acceptanceRequest
	if _, e = tx.Get(ctx, acceptanceRequests, req.RequestID, &binding); e != nil {
		return AcceptOutput{}, e
	}
	var requirement *api.Requirement
	for i := range t.Task.Requirements {
		r := &t.Task.Requirements[i]
		if r.RequirementID == binding.RequirementRef.RequirementID && r.Revision == binding.RequirementRef.Revision {
			requirement = r
		}
	}
	if requirement == nil || requirement.Kind != "quality" {
		return AcceptOutput{}, api.E("forbidden", "acceptance_not_allowed")
	}
	rule, ok := s.rules[componentKey(requirement.RuleRef)]
	if !ok || !rule.AllowUserAcceptance {
		return AcceptOutput{}, api.E("forbidden", "acceptance_not_allowed")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return AcceptOutput{}, e
	}
	expiry, e := api.ParseTime(req.ExpiresAt)
	if e != nil || !now.Before(expiry) {
		return AcceptOutput{}, api.E("expired", "request_expired")
	}
	req.State = "answered"
	req.ConsumedBy = c.CommandID
	req.AnsweredAt = api.Time(now)
	req.AnswerRef = &in.CandidateRef
	req.Revision++
	if e = tx.Put(ctx, inputs, req.RequestID, rev, req); e != nil {
		return AcceptOutput{}, e
	}
	check := api.ConditionResult{CheckID: api.NewID("check"), TaskID: in.TaskID, GoalRevision: in.GoalRevision, RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision, ArtifactRef: in.CandidateRef, RuleRef: requirement.RuleRef, EvaluatorRef: requirement.RuleRef, Verdict: "pass", Applicability: "usable", Basis: "user_accepted", EvidenceRefs: []api.ContentRef{in.CandidateRef, in.LimitationsRef}, ObservedAt: api.Time(now), CheckedAt: api.Time(now), ScopeRef: in.LimitationsRef}
	trusted := auth
	trusted.Roles = append(append([]string{}, auth.Roles...), "evidence")
	ref, e := s.RecordCheckTx(ctx, tx, trusted, check)
	if e != nil {
		return AcceptOutput{}, e
	}
	t, e = getTask(ctx, tx, in.TaskID)
	if e != nil {
		return AcceptOutput{}, e
	}
	return AcceptOutput{TaskRef: taskRef(tx, t), CheckRef: ref}, nil
}
