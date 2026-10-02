package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

type verificationState struct {
	Input       api.VerificationInput
	Candidate   *Candidate
	CandidateID string
	Pending     bool
}

func (c *Coordinator) verificationInput(ctx context.Context, u Unit, f frame) (verificationState, error) {
	v := verificationState{Input: api.VerificationInput{Artifacts: []api.ContentRef{}, Operations: []api.Operation{}, Checks: []api.CheckEvidence{}}}
	e := c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
		v.Input.Task = t.Task
		v.Input.Constraints = t.Constraints
		v.Pending = t.PendingDecision != ""
		rows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "candidate", Current: "candidate", Limit: 2})
		if e != nil {
			return e
		}
		if len(rows) > 1 {
			return durable.ErrInvariant
		}
		if len(rows) == 1 {
			candidate, e := Decode[Candidate](&rows[0])
			if e != nil {
				return e
			}
			if candidate.GoalRevision == t.Task.GoalRevision {
				v.Candidate = &candidate
				v.CandidateID = rows[0].ID
				v.Input.Artifacts = candidate.Artifacts
			}
		}
		rows, e = c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "check", Current: "@current", Limit: c.Config.Limits.Checks + 1})
		if e != nil {
			return e
		}
		if len(rows) > c.Config.Limits.Checks {
			return failure("precondition_failed", "current checks are incomplete")
		}
		for _, r := range rows {
			evidence, e := Decode[api.CheckEvidence](&r)
			if e != nil {
				return e
			}
			v.Input.Checks = append(v.Input.Checks, evidence)
		}
		// Only a bounded current operation projection is relevant to verification.
		rows, e = c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "operation", Current: "@current", Limit: c.Config.Limits.Facts + 1})
		if e != nil {
			return e
		}
		if len(rows) > c.Config.Limits.Facts {
			return failure("precondition_failed", "verification operations are incomplete")
		}
		for _, r := range rows {
			op, e := Decode[api.Operation](&r)
			if e != nil {
				return e
			}
			v.Input.Operations = append(v.Input.Operations, op)
		}
		return nil
	})
	if e == nil {
		v.Input.InputDigest = Hash(struct {
			Task, Goal   string
			Revision     int64
			Requirements []api.Requirement
			Constraints  []string
			Artifacts    []api.ContentRef
			Operations   []api.Operation
		}{v.Input.Task.TaskID, v.Input.Task.GoalRef.Hash, v.Input.Task.GoalRevision, v.Input.Task.Requirements, v.Input.Constraints, v.Input.Artifacts, v.Input.Operations})
	}
	return v, e
}
func (c *Coordinator) verify(ctx context.Context, u Unit, f frame) error {
	if f.Ref.ObjectKind == "defect" {
		return c.impactDefect(ctx, u, f)
	}
	v, e := c.verificationInput(ctx, u, f)
	if e != nil {
		return fmt.Errorf("verification input: %w", e)
	}
	if v.Input.Task.Status != "active" {
		return c.finish(ctx, u, f, "", true)
	}
	if v.Candidate == nil {
		err := c.changeWork(ctx, u, f, true, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
			if e := c.expireInputs(tx, ch, t); e != nil {
				return durable.Disposition{}, e
			}
			if !ch.Now.Before(parseTime(t.Task.Deadline)) {
				return durable.Done(), c.failTask(tx, ch, t, "deadline_exceeded")
			}
			if (t.NoProgressCount >= t.Policy.MaxNoProgress || t.RepairCount > t.Policy.MaxRepairs || t.ContinuationCount >= t.Policy.MaxContinuations) && t.PendingDecision == "" && len(t.Task.OpenEffects) == 0 {
				return durable.Done(), c.failTask(tx, ch, t, "automatic_limits_exhausted")
			}
			if t.PendingDecision == "" {
				if e := c.canAdvance(ch, t); e == nil {
					queue(ch, t, "decide", "task", t.Task.TaskID)
				}
			}
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "current_candidate_missing"), nil
		})
		if err != nil {
			return fmt.Errorf("verify without candidate: %w", err)
		}
		return nil
	}
	allowed, e := c.attempt(ctx, u, f)
	if e != nil || !allowed {
		return e
	}
	var bundle api.VerificationBundle
	e = c.external(ctx, u, f, "verification.prepare", f.Task.Task.TaskID, func() error { var e error; bundle, e = c.Ports.Verification.Prepare(ctx, f.Caller, v.Input); return e })
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if len(bundle.Checks)+1 > c.Config.Limits.Checks || len(bundle.Actions) > int(f.Task.Policy.MaxActions) || len(bundle.Limitations) > 100 {
		return c.finish(ctx, u, f, "verification_scope_incomplete", false)
	}
	refs := append([]api.ContentRef{}, v.Input.Artifacts...)
	components := []api.ComponentRef{}
	if bundle.Coverage != nil {
		cov := bundle.Coverage
		binding := f.Task.Policy.CoverageBinding
		if Hash(binding.RuleRef) != Hash(cov.RuleRef) || Hash(binding.EvaluatorRef) != Hash(cov.EvaluatorRef) {
			return c.finish(ctx, u, f, "unqualified_coverage_implementation", false)
		}
		if cov.InputDigest != v.Input.InputDigest || validateValue("ComponentRef", cov.RuleRef) != nil || validateValue("ComponentRef", cov.EvaluatorRef) != nil || cov.TaskID != v.Input.Task.TaskID || cov.GoalRevision != v.Input.Task.GoalRevision || Hash(cov.GoalRef) != Hash(v.Input.Task.GoalRef) || cov.RequirementsDigest != Hash(v.Input.Task.Requirements) || (cov.Verdict != "pass" && cov.Verdict != "fail" && cov.Verdict != "unknown") {
			return c.finish(ctx, u, f, "invalid_coverage_binding", false)
		}
		refs = append(refs, cov.ReportRef)
		components = append(components, cov.RuleRef, cov.EvaluatorRef)
	}
	seen := map[string]bool{}
	for _, check := range bundle.Checks {
		qualified := false
		for _, b := range f.Task.Policy.VerificationBindings {
			if Hash(b.RuleRef) == Hash(check.RuleRef) && Hash(b.EvaluatorRef) == Hash(check.Result.EvaluatorRef) && b.Basis == check.Result.Basis {
				qualified = true
			}
		}
		if !qualified {
			return c.finish(ctx, u, f, "unqualified_check_implementation", false)
		}
		if check.InputDigest != v.Input.InputDigest || validateValue("ComponentRef", check.RuleRef) != nil || seen[check.CheckID] || validateValue("Id", check.CheckID) != nil || check.TaskID != v.Input.Task.TaskID || check.GoalRevision != v.Input.Task.GoalRevision || check.Result.GoalRevision != check.GoalRevision || validateValue("ConditionResult", check.Result) != nil || check.AcceptanceCommandID != "" || check.Result.Basis == "user_accepted" {
			return c.finish(ctx, u, f, "invalid_check_binding", false)
		}
		seen[check.CheckID] = true
		req := requirement(v.Input.Task.Requirements, check.Result.RequirementID)
		if req == nil || Hash(req.RuleRef) != Hash(check.RuleRef) || !containsRef(v.Input.Artifacts, check.Result.ArtifactRef) {
			return c.finish(ctx, u, f, "invalid_check_scope", false)
		}
		refs = append(refs, check.ReportRef)
		refs = append(refs, check.Result.EvidenceRefs...)
		components = append(components, check.RuleRef, check.Result.EvaluatorRef)
	}
	unique := []api.ContentRef{}
	usedRefs := map[string]bool{}
	for _, ref := range refs {
		if e := validateValue("ContentRef", ref); e != nil {
			return c.finish(ctx, u, f, "invalid_evidence_reference", false)
		}
		key := Hash(ref)
		if !usedRefs[key] {
			usedRefs[key] = true
			unique = append(unique, ref)
		}
	}
	refs = unique
	if len(refs) > c.Config.Limits.Facts {
		return c.finish(ctx, u, f, "verification_reference_scope_incomplete", false)
	}
	bytes := int64(0)
	for _, ref := range refs {
		bytes += ref.ByteLength
		if bytes > int64(c.Config.Limits.Bytes) {
			return c.finish(ctx, u, f, "verification_byte_scope_incomplete", false)
		}
		if e = u.Step(ctx, func() error { _, e := c.content(ctx, f.Caller, ref); return e }); e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
	}
	// Proof use is separate from permission for starting another evaluation.
	if e = c.Ports.Authority.Current(ctx, f.Caller, api.Access{Method: "evidence.use", TaskID: f.Task.Task.TaskID, TargetID: f.Task.Task.TaskID, PolicyRef: &f.Task.Task.PolicyRef, ContentRefs: refs, Components: components}, c.Ports.Clock.Now()); e != nil {
		return c.finish(ctx, u, f, "authorization", false)
	}
	var actions []preparedAction
	if len(bundle.Actions) > 0 {
		proposal := api.Proposal{Kind: "act", Rationale: "fixed verifier action", EvidenceRefs: []api.ContentRef{}, Assumptions: []string{}}
		bodies := []json.RawMessage{}
		for _, a := range bundle.Actions {
			bodies = append(bodies, Raw(a))
		}
		proposal.Actions = &bodies
		prep, e := c.prepareProposal(ctx, u, f, proposal)
		if e != nil {
			return c.finish(ctx, u, f, "authorization", false)
		}
		actions = prep.Actions
	}
	gateRefs := map[string]bool{}
	for _, component := range components {
		if validateValue("ComponentRef", component) != nil {
			return c.finish(ctx, u, f, "invalid_evidence_version", false)
		}
		gateRefs[gateKey(component)] = true
	}
	gateIDs := []string{}
	for k := range gateRefs {
		gateIDs = append(gateIDs, k)
	}
	sort.Strings(gateIDs)
	if e = u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			if e := c.Repository.Read(tx); e != nil {
				return e
			}
			for _, id := range gateIDs {
				if _, e := c.Repository.Gate(tx, id, false, true); e != nil {
					return e
				}
			}
			return tx.Guard(u.Claim())
		}))
	}); e != nil {
		return e
	}
	return c.changeWork(ctx, u, f, true, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		for _, id := range gateIDs {
			if _, e := c.Repository.Gate(tx, id, true, false); e != nil {
				return durable.Disposition{}, e
			}
		}
		if terminal(t) {
			return durable.Done(), nil
		}
		if t.Task.GoalRevision != v.Input.Task.GoalRevision || Hash(t.Task.Requirements) != Hash(v.Input.Task.Requirements) || Hash(t.Task.GoalRef) != Hash(v.Input.Task.GoalRef) {
			queue(ch, t, "verify", "task", t.Task.TaskID)
			return durable.Done(), nil
		}
		candidateRows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "candidate", Current: "candidate", Limit: 2})
		if e != nil {
			return durable.Disposition{}, e
		}
		if len(candidateRows) != 1 || candidateRows[0].ID != v.CandidateID {
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "candidate_changed"), nil
		}
		// Original selections remain fixed for this goal/candidate, including fail.
		if e = c.selectChecks(tx, t, bundle.Checks, v.CandidateID); e != nil {
			return durable.Disposition{}, e
		}
		if len(bundle.AcceptanceRequirements) > 0 {
			if e = c.prepareAcceptance(tx, ch, t, v.CandidateID, *v.Candidate, bundle.AcceptanceRequirements); e != nil {
				return durable.Disposition{}, e
			}
		}
		if bundle.Coverage != nil {
			id := ID("coverage", t.Task.TaskID, strconv.FormatInt(t.Task.GoalRevision, 10), v.CandidateID, v.Input.InputDigest)
			r := rec("coverage", id, t.Task.TaskID, 1, "closed", true, *bundle.Coverage)
			r.CurrentKey = "coverage"
			existing, e := c.Repository.Get(tx, r.Kind, id)
			if e != nil {
				return durable.Disposition{}, e
			}
			if existing == nil {
				if e = c.Repository.Deselect(tx, t, r.Kind, r.CurrentKey); e != nil {
					return durable.Disposition{}, e
				}
				if e = c.Repository.Put(tx, t, r); e != nil {
					return durable.Disposition{}, e
				}
			} else if Hash(existing.Data) != Hash(r.Data) {
				return durable.Disposition{}, failure("revision_conflict", "fixed goal coverage changed content")
			}
		}
		for _, a := range actions {
			source := ID("check", t.Task.TaskID, strconv.FormatInt(t.Task.GoalRevision, 10), v.CandidateID, Hash(a.Preparation.Action))
			row, e := c.Repository.Get(tx, "check_action", source)
			if e != nil {
				return durable.Disposition{}, e
			}
			if row != nil {
				continue
			}
			if t.RepairCount >= t.Policy.MaxRepairs {
				continue
			}
			if e = c.admitActions(tx, ch, t, "check", source, []preparedAction{a}); e != nil {
				return durable.Disposition{}, e
			}
			if e = c.Repository.Put(tx, t, rec("check_action", source, t.Task.TaskID, 1, "closed", true, a)); e != nil {
				return durable.Disposition{}, e
			}
			t.RepairCount++
		}
		if e = c.acceptedChecks(tx, t, v.CandidateID); e != nil {
			return durable.Disposition{}, e
		}
		succeeded, e := c.complete(tx, ch, t, *v.Candidate, bundle.Limitations)
		if e != nil {
			return durable.Disposition{}, e
		}
		if succeeded {
			return durable.Done(), nil
		}
		if !ch.Now.Before(parseTime(t.Task.Deadline)) {
			return durable.Done(), c.failTask(tx, ch, t, "deadline_exceeded")
		}
		if t.PendingDecision == "" && len(t.Task.OpenEffects) == 0 && (t.NoProgressCount >= t.Policy.MaxNoProgress || t.RepairCount > t.Policy.MaxRepairs || t.ContinuationCount >= t.Policy.MaxContinuations) {
			return durable.Done(), c.failTask(tx, ch, t, "automatic_limits_exhausted")
		}
		t.Task.Revision++
		if e = c.persist(tx, ch, t); e != nil {
			return durable.Disposition{}, e
		}
		return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "current_verification_pending"), nil
	})
}
func requirement(rs []api.Requirement, id string) *api.Requirement {
	for i := range rs {
		if rs[i].RequirementID == id {
			return &rs[i]
		}
	}
	return nil
}
func containsRef(refs []api.ContentRef, ref api.ContentRef) bool {
	for _, r := range refs {
		if Hash(r) == Hash(ref) {
			return true
		}
	}
	return false
}

func (c *Coordinator) selectChecks(tx *durable.Tx, t *TaskState, checks []api.CheckEvidence, candidate string) error {
	if len(checks) == 0 {
		return nil
	}
	selected, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "check", Current: "@current", Limit: c.Config.Limits.Checks + 1})
	if e != nil {
		return e
	}
	if len(selected) > c.Config.Limits.Checks {
		return failure("precondition_failed", "selected check set incomplete")
	}
	byKey := map[string]api.CheckEvidence{}
	for _, r := range selected {
		v, e := Decode[api.CheckEvidence](&r)
		if e != nil {
			return e
		}
		byKey[r.CurrentKey] = v
	}
	ids := []string{}
	for _, v := range checks {
		ids = append(ids, v.CheckID)
	}
	existing, e := c.Repository.GetMany(tx, "check", ids)
	if e != nil {
		return e
	}
	byID := map[string]Record{}
	for _, r := range existing {
		byID[r.ID] = r
	}
	rows := []Record{}
	clear := []string{}
	for _, v := range checks {
		key := ID("selection", t.Task.TaskID, strconv.FormatInt(t.Task.GoalRevision, 10), candidate, v.Result.RequirementID)
		if old, ok := byKey[key]; ok && old.CheckID != v.CheckID {
			if old.AcceptanceCommandID != "" {
				continue
			}
			if old.Result.Verdict != "unknown" || old.InputDigest == v.InputDigest || Hash(old.RuleRef) != Hash(v.RuleRef) || Hash(old.Result.EvaluatorRef) != Hash(v.Result.EvaluatorRef) {
				return failure("precondition_failed", "fixed selected check needs new exact evidence inputs")
			}
			clear = append(clear, key)
		}
		row := rec("check", v.CheckID, t.Task.TaskID, 1, "closed", true, v)
		row.CurrentKey = key
		if old, ok := byID[v.CheckID]; ok {
			if old.TaskID != t.Task.TaskID || Hash(old.Data) != Hash(row.Data) {
				return failure("revision_conflict", "original check identity changed")
			}
			continue
		}
		rows = append(rows, row)
	}
	if e = c.Repository.DeselectKeys(tx, t, "check", clear); e != nil {
		return e
	}
	return c.Repository.PutMany(tx, t, rows)
}

func (c *Coordinator) prepareAcceptance(tx *durable.Tx, ch *Change, t *TaskState, candidateID string, candidate Candidate, requirements []string) error {
	if len(requirements) > 100 || len(candidate.Artifacts) != 1 {
		return failure("unsupported", "acceptance requires a finite single exact candidate")
	}
	seen := map[string]bool{}
	for _, id := range requirements {
		r := requirement(t.Task.Requirements, id)
		if r == nil || r.Kind != "quality" || seen[id] {
			return failure("invalid_output", "acceptance scope is not a unique declared quality condition")
		}
		seen[id] = true
	}
	if e := c.Ports.Authority.Current(tx.Context(), c.ownerCaller(t), api.Access{Method: "task.request_acceptance", TaskID: t.Task.TaskID, TargetID: t.Task.TaskID, PolicyRef: &t.Task.PolicyRef, ContentRefs: candidate.Artifacts}, ch.Now); e != nil {
		return e
	}
	source := ID("acceptance", candidateID, Hash(requirements))
	id := ID("request", t.Task.TaskID, source)
	old, e := c.Repository.Get(tx, "input_preparation", id)
	if e != nil {
		return e
	}
	if old != nil {
		return nil
	}
	question := "请确认是否接受这个版本的成果及指定质量条件"
	options := []string{"接受"}
	p := api.Proposal{Question: &question, Options: &options, RequiredMaterialRefs: &candidate.Artifacts}
	if e = c.prepareInput(tx, ch, t, source, p); e != nil {
		return e
	}
	r, e := c.Repository.Get(tx, "input_preparation", id)
	if e != nil {
		return e
	}
	prep, e := Decode[InputPreparation](r)
	if e != nil {
		return e
	}
	prep.Candidate = &candidate.Artifacts[0]
	prep.AcceptanceRequirements = requirements
	// This preparation is fixed before any publication, in this same transaction.
	// Store the acceptance variant directly, without overwriting an immutable row.
	return c.Repository.Put(tx, t, rec("acceptance_preparation", id, t.Task.TaskID, 1, "open", true, prep))
}
func (c *Coordinator) acceptedChecks(tx *durable.Tx, t *TaskState, candidate string) error {
	rows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "acceptance", Current: "@current", Limit: c.Config.Limits.Checks + 1})
	if e != nil {
		return e
	}
	if len(rows) > c.Config.Limits.Checks {
		return failure("precondition_failed", "acceptance projection is incomplete")
	}
	for _, r := range rows {
		input, e := Decode[InputState](&r)
		if e != nil {
			return e
		}
		if input.View.Kind != "acceptance" || input.GoalRevision != t.Task.GoalRevision || input.View.State != "consumed" || input.View.ConsumedBy == nil {
			continue
		}
		if input.View.CandidateHash == nil || input.View.CandidateRef == nil {
			continue
		}
		current, e := c.Repository.Get(tx, "candidate", candidate)
		if e != nil {
			return e
		}
		v, e := Decode[Candidate](current)
		if e != nil {
			return e
		}
		if !containsRef(v.Artifacts, *input.View.CandidateRef) {
			continue
		}
		for _, id := range input.AcceptanceRequirements {
			req := requirement(t.Task.Requirements, id)
			if req == nil || req.Kind != "quality" {
				continue
			}
			check := api.CheckEvidence{CheckID: ID("check", r.ID, id), TaskID: t.Task.TaskID, GoalRevision: t.Task.GoalRevision, RuleRef: req.RuleRef, ReportRef: input.View.QuestionRef, AcceptanceCommandID: *input.View.ConsumedBy, Result: api.ConditionResult{RequirementID: id, GoalRevision: t.Task.GoalRevision, ArtifactRef: *input.View.CandidateRef, Verdict: "pass", Basis: "user_accepted", EvidenceRefs: []api.ContentRef{input.View.QuestionRef}, EvaluatorRef: req.RuleRef}, ComponentCheckIDs: []string{}}
			// User acceptance is an independent fixed selected check, superseding
			// only the exact owner-authenticated quality request it consumed.
			key := ID("selection", t.Task.TaskID, strconv.FormatInt(t.Task.GoalRevision, 10), candidate, id)
			if e = c.Repository.Deselect(tx, t, "check", key); e != nil {
				return e
			}
			row := rec("check", check.CheckID, t.Task.TaskID, 1, "closed", true, check)
			row.CurrentKey = key
			if e = c.Repository.Put(tx, t, row); e != nil {
				return e
			}
		}
	}
	return nil
}

func (c *Coordinator) complete(tx *durable.Tx, ch *Change, t *TaskState, candidate Candidate, limitations []string) (bool, error) {
	if !t.ProjectionComplete || terminal(t) || candidate.GoalRevision != t.Task.GoalRevision || len(candidate.Artifacts) == 0 {
		return false, nil
	}
	coverageRows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "coverage", Current: "coverage", Limit: 2})
	if e != nil {
		return false, e
	}
	if len(coverageRows) != 1 {
		return false, nil
	}
	coverage, e := Decode[api.CoverageEvidence](&coverageRows[0])
	if e != nil {
		return false, e
	}
	if coverage.Verdict != "pass" || coverage.GoalRevision != t.Task.GoalRevision || coverage.RequirementsDigest != Hash(t.Task.Requirements) || Hash(coverage.GoalRef) != Hash(t.Task.GoalRef) {
		return false, nil
	}
	rows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "check", Current: "@current", Limit: c.Config.Limits.Checks + 1})
	if e != nil {
		return false, e
	}
	if len(rows)+1 > c.Config.Limits.Checks {
		return false, nil
	}
	checks := map[string]api.CheckEvidence{}
	byID := map[string]api.CheckEvidence{}
	for _, row := range rows {
		check, e := Decode[api.CheckEvidence](&row)
		if e != nil {
			return false, e
		}
		checks[check.Result.RequirementID] = check
		byID[check.CheckID] = check
	}
	if !acyclicChecks(byID) {
		return false, nil
	}
	refs := map[string]api.ComponentRef{gateKey(coverage.EvaluatorRef): coverage.EvaluatorRef}
	results := []api.ConditionResult{}
	basis := "verified"
	for _, req := range t.Task.Requirements {
		check, ok := checks[req.RequirementID]
		if !ok {
			if req.Required {
				return false, nil
			}
			continue
		}
		if check.GoalRevision != t.Task.GoalRevision || Hash(check.RuleRef) != Hash(req.RuleRef) || !containsRef(candidate.Artifacts, check.Result.ArtifactRef) || check.Result.Verdict != "pass" {
			if req.Required {
				return false, nil
			}
			continue
		}
		refs[gateKey(check.Result.EvaluatorRef)] = check.Result.EvaluatorRef
		results = append(results, check.Result)
		if check.Result.Basis == "user_accepted" {
			basis = "user_accepted"
		} else if check.Result.Basis == "assessed" && basis != "user_accepted" {
			basis = "assessed"
		}
	}
	if len(results) == 0 {
		return false, nil
	}
	keys := []string{}
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		revision, e := c.Repository.Gate(tx, k, true, false)
		if e != nil {
			return false, e
		}
		if revision == 0 {
			return false, failure("precondition_failed", "evidence gate was not initialized before use")
		}
		defects, e := c.Repository.Records(tx, Filter{TaskID: k, Kind: "defect", Limit: c.Config.Limits.Checks + 1})
		if e != nil {
			return false, e
		}
		if len(defects) > c.Config.Limits.Checks {
			return false, nil
		}
		for _, row := range defects {
			d, e := Decode[Defect](&row)
			if e != nil {
				return false, e
			}
			if d.TaskID != "" && d.TaskID != t.Task.TaskID || d.GoalRevision != 0 && d.GoalRevision != t.Task.GoalRevision {
				continue
			}
			for _, check := range checks {
				if Hash(check.Result.EvaluatorRef) == Hash(d.EvaluatorRef) && Hash(check.RuleRef) == Hash(d.RuleRef) && (d.ArtifactHash == "" || d.ArtifactHash == check.Result.ArtifactRef.Hash) {
					return false, nil
				}
			}
			if Hash(coverage.EvaluatorRef) == Hash(d.EvaluatorRef) && Hash(coverage.RuleRef) == Hash(d.RuleRef) {
				return false, nil
			}
		}
	}
	effects, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "effect", State: "open", Limit: c.Config.Limits.Effects + 1})
	if e != nil {
		return false, e
	}
	delegations, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "delegation", State: "open", Limit: c.Config.Limits.Children + 1})
	if e != nil {
		return false, e
	}
	if len(effects) != 0 || len(delegations) != 0 {
		return false, nil
	}
	result := api.Result{TaskID: t.Task.TaskID, GoalRevision: t.Task.GoalRevision, ArtifactRefs: candidate.Artifacts, CompletionBasis: basis, ConditionResults: results, Limitations: append(append([]string{}, limitations...), coverage.Limitations...), CompletedAt: stamp(ch.Now)}
	if validateValue("Result", result) != nil {
		return false, failure("invalid_output", "completion result does not match fixed contract")
	}
	if e = c.Repository.Put(tx, t, rec("result", t.Task.TaskID, t.Task.TaskID, 1, "closed", true, result)); e != nil {
		return false, e
	}
	t.Task.Status = "succeeded"
	t.Task.ControlRevision++
	t.Task.Revision++
	if e = c.Repository.Capacity(tx, t.SubjectID, -1, c.Config.Limits.ActivePerUser); e != nil {
		return false, e
	}
	if e = c.closeInputs(tx, t, "superseded"); e != nil {
		return false, e
	}
	if e = c.controls(tx, ch, t); e != nil {
		return false, e
	}
	if e = c.prepareExtraction(tx, ch, t, result); e != nil {
		return false, e
	}
	if t.AllocationID != "" {
		queue(ch, t, "settle", "receiver", t.AllocationID)
	}
	return true, c.persist(tx, ch, t)
}
func acyclicChecks(checks map[string]api.CheckEvidence) bool {
	states := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if states[id] == 1 {
			return false
		}
		if states[id] == 2 {
			return true
		}
		v, ok := checks[id]
		if !ok {
			return false
		}
		states[id] = 1
		for _, child := range v.ComponentCheckIDs {
			if !visit(child) {
				return false
			}
		}
		states[id] = 2
		return true
	}
	for id := range checks {
		if !visit(id) {
			return false
		}
	}
	return true
}
