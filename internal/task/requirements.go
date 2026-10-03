package task

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) AdoptRequirements(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, taskID, sourceKind string, source api.ObjectRef, delta api.RequirementDelta, report ValidationReport) (api.RequirementAdoption, error) {
	var out api.RequirementAdoption
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		var e error
		out, e = s.AdoptRequirementsTx(ctx, tx, auth, taskID, sourceKind, source, delta, report)
		return e
	})
	return out, err
}
func (s *Service) AdoptRequirementsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID, sourceKind string, source api.ObjectRef, delta api.RequirementDelta, report ValidationReport) (api.RequirementAdoption, error) {
	t, e := getTask(ctx, tx, taskID)
	if e != nil {
		return api.RequirementAdoption{}, e
	}
	if e = principal(auth, t); e != nil {
		return api.RequirementAdoption{}, e
	}
	if !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return api.RequirementAdoption{}, api.E("forbidden", "trusted_interpretation_required")
	}
	if e = runtime.CheckRef(tx.Scope(), source); e != nil {
		return api.RequirementAdoption{}, e
	}
	if sourceKind != "decision" && sourceKind != "command" && sourceKind != "template" {
		return api.RequirementAdoption{}, invalid("invalid_requirement_source")
	}
	key := taskID + "/" + sourceKind + "/" + refKey(source)
	digest, e := api.Digest(delta)
	if e != nil {
		return api.RequirementAdoption{}, e
	}
	existing, e := tx.LookupKey(ctx, adoptions, key)
	if e == nil {
		if existing.Digest != digest {
			return api.RequirementAdoption{}, api.E("idempotency_conflict", "digest_conflict")
		}
		var a api.RequirementAdoption
		_, e = tx.Get(ctx, adoptions, existing.ObjectID, &a)
		return a, e
	}
	if !errors.Is(e, runtime.ErrNotFound) && !api.IsCode(e, "not_found") {
		return api.RequirementAdoption{}, e
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return api.RequirementAdoption{}, e
	}
	if delta.BaseGoalRevision != t.Task.GoalRevision {
		return api.RequirementAdoption{}, api.E("revision_conflict", "goal_changed")
	}
	if sourceKind == "decision" {
		var d decisionState
		if _, e = tx.Get(ctx, decisions, source.ObjectID, &d); e != nil {
			return api.RequirementAdoption{}, e
		}
		if d.Snapshot.TaskRef.ObjectID != taskID || d.Snapshot.GoalRevision != t.Task.GoalRevision || d.Snapshot.ControlRevision != t.Task.ControlRevision {
			return api.RequirementAdoption{}, api.E("revision_conflict", "stale_snapshot")
		}
	}
	if uint64(len(delta.Candidates)) > t.Policy.MaxRequirements || len(delta.Candidates) > 100 || len(report.SemanticKeys) != len(delta.Candidates) {
		return api.RequirementAdoption{}, invalid("invalid_requirement_delta")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return api.RequirementAdoption{}, e
	}
	a := api.RequirementAdoption{AdoptionID: api.NewID("adoption"), TaskRef: taskRef(tx, t), SourceKind: sourceKind, SourceRef: source, BaseGoalRevision: t.Task.GoalRevision, ResultGoalRevision: t.Task.GoalRevision, Outcome: "unchanged", Mappings: []api.RequirementMapping{}, ValidationReportRef: report.ReportRef, ReasonCodes: []string{}, DecidedAt: api.Time(now)}
	if a.SourceKind == "template" {
		a.SourceKind = "command"
	}
	rejection := func(reason string) (api.RequirementAdoption, error) {
		a.Outcome = "rejected"
		a.ReasonCodes = append(a.ReasonCodes, reason)
		if e = tx.Create(ctx, adoptions, a.AdoptionID, taskID, a); e != nil {
			return a, e
		}
		if e = tx.Bind(ctx, adoptions, key, a.AdoptionID, digest); e != nil {
			return a, e
		}
		return a, nil
	}
	if e = contentScope(tx.Scope(), report.ReportRef); e != nil {
		return a, e
	}
	if !report.Valid {
		return rejection("requirement_validation_failed")
	}
	next := append([]api.Requirement{}, t.Task.Requirements...)
	nextKeys := append([]string{}, t.SemanticKeys...)
	if len(next) != len(nextKeys) {
		return a, api.E("dependency_unavailable", "requirement_index_incomplete")
	}
	seenLocal, seenKeys, seenReplace := map[string]bool{}, map[string]bool{}, map[string]bool{}
	changed := false
	newDefinitions := []api.Requirement{}
	for i, c := range delta.Candidates {
		if e = api.ValidateRecord("RequirementCandidate", c); e != nil {
			return rejection("invalid_requirement_candidate")
		}
		if len(c.OpenQuestions) > 0 {
			t.Task.RequirementsState = "awaiting_input"
			if e = s.saveTask(ctx, tx, &t); e != nil {
				return a, e
			}
			return rejection("requirement_ambiguous")
		}
		semantic := report.SemanticKeys[i]
		if semantic == "" || len(semantic) > 512 || seenLocal[c.CandidateKey] || seenKeys[semantic] {
			return rejection("ambiguous_requirement_delta")
		}
		seenLocal[c.CandidateKey] = true
		seenKeys[semantic] = true
		rule, ok := s.rules[componentKey(c.RuleRef)]
		if !ok || rule.Kind != c.Kind {
			return rejection("unknown_rule")
		}
		if c.Origin == "explicit_user" && !c.Required {
			return rejection("explicit_requirement_not_required")
		}
		sourceOK := false
		for _, sr := range c.SourceRefs {
			if e = contentScope(tx.Scope(), sr.ContentRef); e != nil {
				return a, e
			}
			for _, original := range t.SourceRefs {
				if api.Equal(sr.ContentRef, original.ContentRef) {
					sourceOK = true
				}
			}
		}
		if !sourceOK {
			return rejection("invalid_source")
		}
		match := -1
		if c.ReplacesRequirementID != "" {
			if c.ReplacesRevision == nil || seenReplace[c.ReplacesRequirementID] {
				return rejection("ambiguous_requirement_delta")
			}
			seenReplace[c.ReplacesRequirementID] = true
			for j, r := range next {
				if r.RequirementID == c.ReplacesRequirementID {
					match = j
					break
				}
			}
			if match < 0 || next[match].Revision != *c.ReplacesRevision {
				return rejection("replaces_revision_changed")
			}
			old := next[match]
			if old.Required && !c.Required || old.Origin == "explicit_user" && c.Kind != old.Kind {
				return rejection("requirement_scope_lowered")
			}
		} else {
			for j, k := range nextKeys {
				if k == semantic {
					if match != -1 {
						return rejection("ambiguous_requirement_delta")
					}
					match = j
				}
			}
		}
		if match >= 0 && nextKeys[match] == semantic {
			r := next[match]
			a.Mappings = append(a.Mappings, api.RequirementMapping{CandidateKey: c.CandidateKey, RequirementID: r.RequirementID, Revision: r.Revision})
			continue
		}
		r := api.Requirement{RequirementID: api.NewID("requirement"), Revision: 1, Kind: c.Kind, StatementRef: c.StatementRef, SourceRefs: c.SourceRefs, Origin: c.Origin, RuleRef: c.RuleRef, RuleParametersRef: c.RuleParametersRef, Required: c.Required, AdoptionID: a.AdoptionID}
		if match >= 0 {
			r.RequirementID = next[match].RequirementID
			r.Revision = next[match].Revision + 1
			next[match] = r
			nextKeys[match] = semantic
		} else {
			next = append(next, r)
			nextKeys = append(nextKeys, semantic)
		}
		if uint64(len(next)) > t.Policy.MaxRequirements {
			return rejection("requirement_limit")
		}
		newDefinitions = append(newDefinitions, r)
		a.Mappings = append(a.Mappings, api.RequirementMapping{CandidateKey: c.CandidateKey, RequirementID: r.RequirementID, Revision: r.Revision})
		changed = true
	}
	if changed {
		for _, r := range newDefinitions {
			if e = tx.Create(ctx, requirements, fmt.Sprintf("%s/%020d", r.RequirementID, r.Revision), taskID, r); e != nil {
				return a, e
			}
		}
		t.Task.Requirements = next
		t.SemanticKeys = nextKeys
		t.Task.GoalRevision++
		t.Task.ControlRevision++
		t.Task.RequirementsState = "validating"
		t.Task.CurrentCoverageRef = nil
		t.NoProgress = 0
		t.Task.RequirementsDigest, e = requirementsDigest(next)
		if e != nil {
			return a, e
		}
		a.Outcome = "accepted"
		a.ResultGoalRevision = t.Task.GoalRevision
		if e = s.closeUnsent(ctx, tx, &t); e != nil {
			return a, e
		}
		if e = s.saveTask(ctx, tx, &t); e != nil {
			return a, e
		}
		if e = s.saveGoal(ctx, tx, t, "requirements_adopted", source); e != nil {
			return a, e
		}
		if e = s.controlJobs(ctx, tx, t); e != nil {
			return a, e
		}
		if _, e = raise(ctx, tx, JobCoverage, "coverage/"+taskID, taskRef(tx, t)); e != nil {
			return a, e
		}
	}
	if e = tx.Create(ctx, adoptions, a.AdoptionID, taskID, a); e != nil {
		return a, e
	}
	if e = tx.Bind(ctx, adoptions, key, a.AdoptionID, digest); e != nil {
		return a, e
	}
	return a, nil
}
func (s *Service) StoreCoverage(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, coverage api.GoalCoverage) (api.ObjectRef, error) {
	var out api.ObjectRef
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { var e error; out, e = s.StoreCoverageTx(ctx, tx, auth, coverage); return e })
	return out, err
}
func (s *Service) StoreCoverageTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.GoalCoverage) (api.ObjectRef, error) {
	if !auth.HasRole("service") && !auth.HasRole("evidence") {
		return api.ObjectRef{}, api.E("forbidden", "trusted_evidence_required")
	}
	t, e := getTask(ctx, tx, c.TaskRef.ObjectID)
	if e != nil {
		return api.ObjectRef{}, e
	}
	if e = runtime.CheckRef(tx.Scope(), c.TaskRef); e != nil {
		return api.ObjectRef{}, e
	}
	if c.TaskRef.OwnerID != tx.Scope().OwnerID {
		return api.ObjectRef{}, api.E("forbidden", "wrong_task_owner")
	}
	if e = api.ValidateRecord("GoalCoverage", c); e != nil {
		return api.ObjectRef{}, e
	}
	if c.GoalRevision != t.Task.GoalRevision || !api.Equal(c.GoalRef, t.Task.GoalRef) || c.RequirementsDigest != t.Task.RequirementsDigest {
		return api.ObjectRef{}, api.E("revision_conflict", "coverage_stale")
	}
	if terminal(t) {
		return api.ObjectRef{}, api.E("invalid_state", "target_terminal")
	}
	if len(t.Task.Requirements) == 0 {
		return api.ObjectRef{}, api.E("invalid_state", "empty_requirements")
	}
	if s.ports.Gate == nil {
		return api.ObjectRef{}, api.E("dependency_unavailable", "evidence_gate_unavailable")
	}
	if gate, ok := s.ports.Gate.(EvidenceRegistration); ok {
		if e = gate.RegisterCoverage(ctx, tx, t.Task, c); e != nil {
			return api.ObjectRef{}, e
		}
	}
	ref := tx.Scope().Ref(c.CoverageID, c.Revision)
	if e = s.ports.Gate.Evidence(ctx, tx, t.Task, []api.ObjectRef{ref}, []api.ComponentRef{c.RuleRef, c.EvaluatorRef}); e != nil {
		return api.ObjectRef{}, e
	}
	var old api.GoalCoverage
	rev, e := tx.Get(ctx, coverages, c.CoverageID, &old)
	if e == nil {
		if !api.Equal(old, c) {
			return api.ObjectRef{}, api.E("idempotency_conflict", "digest_conflict")
		}
		return tx.Scope().Ref(c.CoverageID, rev), nil
	}
	if !api.IsCode(e, "not_found") {
		return api.ObjectRef{}, e
	}
	if c.Revision != 1 {
		return api.ObjectRef{}, invalid("initial_coverage_revision")
	}
	if e = tx.Create(ctx, coverages, c.CoverageID, t.Task.TaskID, c); e != nil {
		return api.ObjectRef{}, e
	}
	t.Task.CurrentCoverageRef = &ref
	if c.Verdict == "pass" && c.Applicability == "usable" {
		t.Task.RequirementsState = "ready"
	} else {
		t.Task.RequirementsState = "validating"
		t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: "evidence", ObjectRef: &ref, ResumeCondition: "current full-goal coverage passes"})
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return api.ObjectRef{}, e
	}
	if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
		return api.ObjectRef{}, e
	}
	return ref, nil
}
func (s *Service) AttachTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AttachInput) (AttachOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return AttachOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return AttachOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return AttachOutput{}, e
	}
	if in.GoalRevision != t.Task.GoalRevision {
		return AttachOutput{}, api.E("revision_conflict", "wrong_goal")
	}
	if terminal(t) {
		return AttachOutput{}, api.E("invalid_state", "target_terminal")
	}
	found := false
	for _, r := range t.Task.Requirements {
		if r.RequirementID == in.RequirementRef.RequirementID && r.Revision == in.RequirementRef.Revision {
			found = true
		}
	}
	if !found {
		return AttachOutput{}, invalid("unknown_requirement")
	}
	if len(in.EvidenceRefs) == 0 || len(in.EvidenceRefs) > 100 {
		return AttachOutput{}, invalid("evidence_required")
	}
	if e = s.authorize(ctx, tx, auth, "task.attach_evidence", append([]api.ContentRef{in.ArtifactRef}, in.EvidenceRefs...), nil); e != nil {
		return AttachOutput{}, e
	}
	sort.Slice(in.EvidenceRefs, func(i, j int) bool {
		a, _ := api.Digest(in.EvidenceRefs[i])
		b, _ := api.Digest(in.EvidenceRefs[j])
		return a < b
	})
	digest, e := api.Digest(in)
	if e != nil {
		return AttachOutput{}, e
	}
	existing, e := tx.LookupKey(ctx, checkRequests, digest)
	if e == nil {
		return AttachOutput{TaskRef: taskRef(tx, t), CheckRequestRef: tx.Scope().Ref(existing.ObjectID, 1)}, nil
	}
	if !api.IsCode(e, "not_found") {
		return AttachOutput{}, e
	}
	req := CheckRequest{CheckID: api.NewID("check"), Revision: 1, Input: in, State: "pending", CreatorID: auth.SubjectID}
	if e = tx.Create(ctx, checkRequests, req.CheckID, in.TaskID, req); e != nil {
		return AttachOutput{}, e
	}
	if e = tx.Bind(ctx, checkRequests, digest, req.CheckID, digest); e != nil {
		return AttachOutput{}, e
	}
	if _, e = raise(ctx, tx, JobCheck, "check/"+req.CheckID, tx.Scope().Ref(req.CheckID, 1)); e != nil {
		return AttachOutput{}, e
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return AttachOutput{}, e
	}
	return AttachOutput{TaskRef: taskRef(tx, t), CheckRequestRef: tx.Scope().Ref(req.CheckID, 1)}, nil
}
func (s *Service) RecordCheck(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, c api.ConditionResult) (api.ObjectRef, error) {
	var out api.ObjectRef
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { var e error; out, e = s.RecordCheckTx(ctx, tx, auth, c); return e })
	return out, err
}
func (s *Service) RecordCheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.ConditionResult) (api.ObjectRef, error) {
	if !auth.HasRole("service") && !auth.HasRole("evidence") {
		return api.ObjectRef{}, api.E("forbidden", "trusted_evidence_required")
	}
	t, e := getTask(ctx, tx, c.TaskID)
	if e != nil {
		return api.ObjectRef{}, e
	}
	if e = api.ValidateRecord("ConditionResult", c); e != nil {
		return api.ObjectRef{}, e
	}
	if c.GoalRevision != t.Task.GoalRevision {
		return api.ObjectRef{}, api.E("revision_conflict", "wrong_goal")
	}
	var req *api.Requirement
	for i := range t.Task.Requirements {
		r := &t.Task.Requirements[i]
		if r.RequirementID == c.RequirementID && r.Revision == c.RequirementRevision {
			req = r
			break
		}
	}
	if req == nil || !api.Equal(req.RuleRef, c.RuleRef) {
		return api.ObjectRef{}, invalid("unknown_requirement")
	}
	if req.Kind == "effect" && c.Basis == "user_accepted" {
		return api.ObjectRef{}, api.E("forbidden", "acceptance_not_allowed")
	}
	if c.Verdict == "pass" && c.Applicability == "usable" && c.ObservedAt == "" {
		return api.ObjectRef{}, invalid("observation_required")
	}
	if e = contentScope(tx.Scope(), c.ArtifactRef); e != nil {
		return api.ObjectRef{}, e
	}
	var old api.ConditionResult
	rev, e := tx.Get(ctx, checks, c.CheckID, &old)
	if e == nil {
		if !api.Equal(old, c) {
			return api.ObjectRef{}, api.E("idempotency_conflict", "digest_conflict")
		}
		return tx.Scope().Ref(c.CheckID, rev), nil
	}
	if !api.IsCode(e, "not_found") {
		return api.ObjectRef{}, e
	}
	if s.ports.Gate == nil {
		return api.ObjectRef{}, api.E("dependency_unavailable", "evidence_gate_unavailable")
	}
	if gate, ok := s.ports.Gate.(EvidenceRegistration); ok {
		if e = gate.RegisterCheck(ctx, tx, t.Task, c); e != nil {
			return api.ObjectRef{}, e
		}
	}
	ref := tx.Scope().Ref(c.CheckID, 1)
	if e = s.ports.Gate.Evidence(ctx, tx, t.Task, []api.ObjectRef{ref}, []api.ComponentRef{c.RuleRef, c.EvaluatorRef}); e != nil {
		return api.ObjectRef{}, e
	}
	if e = tx.Create(ctx, checks, c.CheckID, t.Task.TaskID, c); e != nil {
		return api.ObjectRef{}, e
	}
	// 按准确条件选择最新可信检查，不在完成时从历史任取一个 pass。
	key := t.Task.TaskID + "/" + fmt.Sprintf("%020d", t.Task.GoalRevision) + "/" + c.RequirementID
	var selected selectedCheck
	oldRev, e := tx.Get(ctx, selections, key, &selected)
	if e == nil {
		selected.Revision++
		selected.Ref = ref
		if e = tx.Put(ctx, selections, key, oldRev, selected); e != nil {
			return api.ObjectRef{}, e
		}
	} else if api.IsCode(e, "not_found") {
		if e = tx.Create(ctx, selections, key, t.Task.TaskID, selectedCheck{Revision: 1, Ref: ref}); e != nil {
			return api.ObjectRef{}, e
		}
	} else {
		return api.ObjectRef{}, e
	}
	t.CurrentArtifactRefs = appendUniqueContent(t.CurrentArtifactRefs, c.ArtifactRef)
	t.NoProgress = 0
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return api.ObjectRef{}, e
	}
	if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
		return api.ObjectRef{}, e
	}
	return ref, nil
}

const selections = "task.check_selections"

type selectedCheck struct {
	Revision uint64        `json:"revision"`
	Ref      api.ObjectRef `json:"ref"`
}

func appendUniqueContent(refs []api.ContentRef, ref api.ContentRef) []api.ContentRef {
	for _, r := range refs {
		if api.Equal(r, ref) {
			return refs
		}
	}
	return append(refs, ref)
}
