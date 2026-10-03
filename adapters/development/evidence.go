package development

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"golang.org/x/text/unicode/norm"
	"strings"
	"time"
)

type evidenceBridge struct{ a *App }

func (e evidenceBridge) ValidateRequirements(ctx context.Context, s runtime.Scope, t api.Task, d api.RequirementDelta) (task.ValidationReport, error) {
	if out, handled, err := e.a.validateInformationRequirements(ctx, s, t, d); handled || err != nil {
		return out, err
	}
	out := task.ValidationReport{Valid: true, SemanticKeys: []string{}, ReasonCodes: []string{}}
	facts, er := e.a.Task.ContextFacts(ctx, e.a.Store, s, e.a.ServiceAuth, t.TaskID)
	if er != nil {
		return out, er
	}
	goalSources, er := e.a.goalSourceEvidence(ctx, s, t.GoalRef, facts.SourceRefs, nil)
	if er != nil {
		return out, er
	}
	raw, er := e.a.goalBytes(ctx, s, e.a.ServiceAuth, t.GoalRef)
	if er != nil {
		return out, er
	}
	var goal brain.GoalSpec
	v, er := api.NewValidator(brain.GoalSchema())
	if er != nil {
		return out, er
	}
	if v.Validate(raw) != nil || api.Decode(raw, &goal) != nil {
		out.Valid = false
		out.ReasonCodes = append(out.ReasonCodes, "goal_template_not_supported")
	}
	for _, candidate := range d.Candidates {
		if candidate.RuleParametersRef == nil || !candidate.Required || len(candidate.OpenQuestions) > 0 || !api.Equal(candidate.RuleRef, e.a.ArtifactRule) && !api.Equal(candidate.RuleRef, e.a.SavedRule) {
			out.Valid = false
			out.ReasonCodes = append(out.ReasonCodes, "unregistered_or_incomplete_requirement")
		}
		statement, err := e.a.Memory.Read(ctx, s, e.a.ServiceAuth, candidate.StatementRef, "task.context")
		if err != nil {
			return out, err
		}
		var params brain.RuleParameters
		if candidate.RuleParametersRef != nil {
			bytes, err := e.a.Memory.Read(ctx, s, e.a.ServiceAuth, *candidate.RuleParametersRef, "task.context")
			if err != nil {
				return out, err
			}
			decodeErr := api.Decode(bytes, &params)
			expected, preferenceErr := e.a.validatePreferenceParameters(ctx, facts, goal, params)
			if preferenceErr != nil && !api.IsCode(preferenceErr, "forbidden") {
				return out, preferenceErr
			}
			if decodeErr != nil || preferenceErr != nil || params.Kind != goal.Kind || params.ExpectedHash != api.Hash(expected) || params.ExpectedLength != uint64(len(expected)) || params.SavePath != goal.SavePath {
				out.Valid = false
				out.ReasonCodes = append(out.ReasonCodes, "requirement_changes_original_goal")
			}
		}
		sourceMatched := len(candidate.SourceRefs) > 0
		for _, source := range candidate.SourceRefs {
			originalMatched := false
			for _, original := range goalSources {
				originalMatched = originalMatched || api.Equal(source, original)
			}
			sourceMatched = sourceMatched && originalMatched
		}
		if !sourceMatched {
			out.Valid = false
			out.ReasonCodes = append(out.ReasonCodes, "original_goal_source_missing")
		}
		semantic, _ := api.Digest(struct {
			Kind       string               `json:"kind"`
			Rule       api.ComponentRef     `json:"rule"`
			Parameters brain.RuleParameters `json:"parameters"`
			Statement  string               `json:"statement"`
			Required   bool                 `json:"required"`
		}{candidate.Kind, candidate.RuleRef, params, norm.NFC.String(strings.Join(strings.Fields(strings.ToLower(string(statement))), " ")), candidate.Required})
		out.SemanticKeys = append(out.SemanticKeys, semantic)
	}
	reportBytes := api.Raw(struct {
		Valid   bool           `json:"valid"`
		Keys    []string       `json:"keys"`
		Reasons []string       `json:"reasons"`
		GoalRef api.ContentRef `json:"goal_ref"`
	}{out.Valid, out.SemanticKeys, out.ReasonCodes, t.GoalRef})
	sources := []api.ContentRef{t.GoalRef, d.ReasonRef}
	for _, c := range d.Candidates {
		sources = append(sources, c.StatementRef)
		if c.RuleParametersRef != nil {
			sources = append(sources, *c.RuleParametersRef)
		}
	}
	out.ReportRef, er = e.a.Publish(ctx, s, e.a.ServiceAuth, stableID("content", "validation/"+api.Hash(api.Raw(d))), "application/json", reportBytes, sources, []api.ContentRef{})
	return out, er
}
func (e evidenceBridge) Coverage(ctx context.Context, s runtime.Scope, t api.Task) (api.GoalCoverage, error) {
	if out, handled, err := e.a.informationCoverage(ctx, s, t); handled || err != nil {
		return out, err
	}
	raw, er := e.a.goalBytes(ctx, s, e.a.ServiceAuth, t.GoalRef)
	if er != nil {
		return api.GoalCoverage{}, er
	}
	var goal brain.GoalSpec
	validator, _ := api.NewValidator(brain.GoalSchema())
	valid := validator.Validate(raw) == nil && api.Decode(raw, &goal) == nil
	artifact, saved := false, false
	for _, r := range t.Requirements {
		artifact = artifact || r.Required && api.Equal(r.RuleRef, e.a.ArtifactRule)
		saved = saved || r.Required && api.Equal(r.RuleRef, e.a.SavedRule)
	}
	valid = valid && artifact && (goal.Kind != "report" || saved)
	verdict := "fail"
	if valid {
		verdict = "pass"
	}
	reportBody := api.Raw(struct {
		GoalRef            api.ContentRef `json:"goal_ref"`
		RequirementsDigest string         `json:"requirements_digest"`
		ArtifactRequired   bool           `json:"artifact_required"`
		SavedRequired      bool           `json:"saved_required"`
		Verdict            string         `json:"verdict"`
	}{t.GoalRef, t.RequirementsDigest, artifact, saved, verdict})
	report, er := e.a.Publish(ctx, s, e.a.ServiceAuth, stableID("content", "coverage/"+t.TaskID+"/"+t.RequirementsDigest), "application/json", reportBody, []api.ContentRef{t.GoalRef}, []api.ContentRef{})
	if er != nil {
		return api.GoalCoverage{}, er
	}
	now, er := e.a.now(ctx, s)
	if er != nil {
		return api.GoalCoverage{}, er
	}
	return api.GoalCoverage{CoverageID: stableID("coverage", t.TaskID+"/"+t.RequirementsDigest), TaskRef: s.Ref(t.TaskID, t.Revision), GoalRevision: t.GoalRevision, GoalRef: t.GoalRef, RequirementsDigest: t.RequirementsDigest, MappingReportRef: report, RuleRef: e.a.CoverageRule, EvaluatorRef: e.a.CoverageRule, Verdict: verdict, Applicability: "usable", CheckedAt: api.Time(now), Revision: 1}, nil
}
func (e evidenceBridge) Check(ctx context.Context, s runtime.Scope, t api.Task, req task.CheckRequest) (api.ConditionResult, error) {
	if out, handled, err := e.a.checkInformationAnswer(ctx, s, t, req); handled || err != nil {
		return out, err
	}
	in := req.Input
	var requirement *api.Requirement
	for i := range t.Requirements {
		r := &t.Requirements[i]
		if r.RequirementID == in.RequirementRef.RequirementID && r.Revision == in.RequirementRef.Revision {
			requirement = r
		}
	}
	if requirement == nil || requirement.RuleParametersRef == nil {
		return api.ConditionResult{}, api.E("revision_conflict", "requirement_changed")
	}
	ctx, er := e.a.prepareCompletionCheck(ctx, s, t, req, *requirement.RuleParametersRef)
	if er != nil {
		return api.ConditionResult{}, er
	}
	paramsBytes, er := e.a.Memory.Read(ctx, s, e.a.ServiceAuth, *requirement.RuleParametersRef, "task.context")
	if er != nil {
		return api.ConditionResult{}, er
	}
	var params brain.RuleParameters
	if er = api.Decode(paramsBytes, &params); er != nil {
		return api.ConditionResult{}, er
	}
	artifact, er := e.a.Memory.Read(ctx, s, e.a.ServiceAuth, in.ArtifactRef, "task.context")
	if er != nil {
		return api.ConditionResult{}, er
	}
	verdict := "fail"
	valid := api.Hash(artifact) == params.ExpectedHash && uint64(len(artifact)) == params.ExpectedLength
	observedAt, er := e.a.now(ctx, s)
	if er != nil {
		return api.ConditionResult{}, er
	}
	evidence := append([]api.ContentRef{in.ArtifactRef, *requirement.RuleParametersRef}, in.EvidenceRefs...)
	var fileBasis *savedDeviceBasis
	if api.Equal(requirement.RuleRef, e.a.SavedRule) {
		facts, er := e.a.Task.ContextFacts(ctx, e.a.Store, s, e.a.ServiceAuth, t.TaskID)
		if er != nil {
			return api.ConditionResult{}, er
		}
		remote, er := e.a.savedDeviceEvidence(ctx, s, t, params, facts)
		if er != nil {
			return api.ConditionResult{}, er
		}
		if remote.Handled {
			valid = valid && remote.Valid
			evidence = append(evidence, remote.Evidence...)
			fileBasis = remote.Basis
			if !remote.ObservedAt.IsZero() {
				observedAt = remote.ObservedAt
			}
		} else {
			saved, readback := false, false
			for _, op := range facts.Operations {
				if strings.HasSuffix(op.Intent.LogicalStepKey, "/save_report") {
					saved = op.Fact.Closed && op.Fact.Effect == "applied" && !op.Fact.MayApplyLater
				}
				if strings.HasSuffix(op.Intent.LogicalStepKey, "/verify_file") {
					readback = op.Fact.Closed && op.Fact.Effect != "unknown" && !op.Fact.MayApplyLater
				}
			}
			actual, er := e.a.Files.Read(ctx, params.SavePath)
			if er != nil {
				return api.ConditionResult{}, er
			}
			valid = valid && saved && readback && api.Hash(actual.Data) == params.ExpectedHash && uint64(len(actual.Data)) == params.ExpectedLength
			observedAt, er = api.ParseTime(actual.ObservedAt)
			if er != nil {
				return api.ConditionResult{}, er
			}
		}
	}
	if valid {
		verdict = "pass"
	}
	now, er := e.a.now(ctx, s)
	if er != nil {
		return api.ConditionResult{}, er
	}
	scopeBody := api.Raw(struct {
		TaskRef      api.ObjectRef        `json:"task_ref"`
		GoalRevision uint64               `json:"goal_revision"`
		Requirement  api.RequirementRef   `json:"requirement"`
		Artifact     api.ContentRef       `json:"artifact"`
		Parameters   brain.RuleParameters `json:"parameters"`
		Verdict      string               `json:"verdict"`
		FileBasis    *savedDeviceBasis    `json:"file_basis,omitempty"`
	}{s.Ref(t.TaskID, t.Revision), t.GoalRevision, in.RequirementRef, in.ArtifactRef, params, verdict, fileBasis})
	scopeRef, er := e.a.Publish(ctx, s, e.a.ServiceAuth, stableID("content", "check/"+req.CheckID), "application/json", scopeBody, append(evidence, t.GoalRef), []api.ContentRef{})
	if er != nil {
		return api.ConditionResult{}, er
	}
	return api.ConditionResult{CheckID: req.CheckID, TaskID: t.TaskID, GoalRevision: t.GoalRevision, RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision, ArtifactRef: in.ArtifactRef, RuleRef: requirement.RuleRef, EvaluatorRef: requirement.RuleRef, Verdict: verdict, Applicability: "usable", Basis: "verified", EvidenceRefs: uniqueSources(evidence), ObservedAt: api.Time(observedAt), ScopeRef: scopeRef, CheckedAt: api.Time(now)}, nil
}
func (e evidenceBridge) RegisterCoverage(ctx context.Context, tx runtime.Tx, t api.Task, c api.GoalCoverage) error {
	_, er := e.a.Governance.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CoverageID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: stableID("requirement", "coverage/"+t.TaskID), RequirementRevision: c.GoalRevision, ArtifactRef: c.GoalRef, ScopeRef: c.MappingReportRef, ObservedAt: c.CheckedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: component("coverage-config"), InstallLockRef: e.a.InstallLock, DependentCheckRefs: []api.ObjectRef{}, Verdict: c.Verdict, Basis: "verified", ReportRef: c.MappingReportRef, EvidenceRefs: []api.ContentRef{c.MappingReportRef, c.GoalRef}})
	return er
}
func (e evidenceBridge) RegisterCheck(ctx context.Context, tx runtime.Tx, t api.Task, c api.ConditionResult) error {
	_, er := e.a.Governance.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CheckID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: c.RequirementID, RequirementRevision: c.RequirementRevision, ArtifactRef: c.ArtifactRef, ScopeRef: c.ScopeRef, ObservedAt: c.ObservedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: component("check-config"), InstallLockRef: e.a.InstallLock, DependentCheckRefs: []api.ObjectRef{}, Verdict: c.Verdict, Basis: c.Basis, ReportRef: c.ScopeRef, EvidenceRefs: c.EvidenceRefs})
	return er
}
func (e evidenceBridge) BindResult(ctx context.Context, tx runtime.Tx, t api.Task, result api.Result, checks []api.ObjectRef) error {
	refs := []governance.CheckReference{}
	for _, r := range checks {
		refs = append(refs, governance.CheckReference{CheckRef: r})
	}
	ref := tx.Scope().Ref(result.ResultID, result.Revision)
	_, er := e.a.Governance.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: refs, MaxStalenessSeconds: 0, ResultRef: &ref})
	return er
}
func (a *App) now(ctx context.Context, s runtime.Scope) (time.Time, error) {
	var now time.Time
	status, e := a.Store.Within(ctx, s, []string{"platform"}, func(tx runtime.Tx) error { var e error; now, e = tx.Now(ctx); return e })
	if status == runtime.CommitUnknown {
		return now, runtime.ErrCommitUnknown
	}
	return now, e
}
