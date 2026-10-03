package development

import (
	"context"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"golang.org/x/text/unicode/norm"
)

// InformationQuestion 是普通Goal.Body中的闭合参考问题，不是新的Task权威。
type InformationQuestion struct {
	Profile           string                      `json:"profile"`
	Sources           []InformationQuestionSource `json:"sources"`
	Claims            []providers.ReferenceClaim  `json:"claims"`
	MaxAgeSeconds     uint64                      `json:"max_age_seconds"`
	ObservedAtPointer string                      `json:"observed_at_pointer"`
}
type InformationQuestionSource struct {
	SourceRef api.ComponentRef `json:"source_ref"`
	URL       string           `json:"url"`
}
type InformationAnswer struct {
	Profile string                       `json:"profile"`
	Answers []providers.ReferencedAnswer `json:"answers"`
}
type informationContextPacket struct {
	Question     *InformationQuestion
	Rule         *api.ComponentRef
	Observations []providers.InformationObservation
}

func InformationQuestionSchema() api.Schema {
	bounded := func(max uint64) api.Schema { return api.Schema{"type": "string", "maxLength": max} }
	claim := api.Object(map[string]any{"key": bounded(64), "json_pointer": bounded(512), "expected_value": bounded(4096)}, "key", "json_pointer")
	source := api.Object(map[string]any{"source_ref": api.Ref("ComponentRef"), "url": bounded(2048)}, "source_ref", "url")
	return api.Object(map[string]any{"profile": api.Schema{"const": providers.ReferenceProfile}, "sources": api.Array(source, 1, 8), "claims": api.Array(claim, 1, 16), "max_age_seconds": api.Schema{"type": "integer", "minimum": 1, "maximum": 604800}, "observed_at_pointer": bounded(512)}, "profile", "sources", "claims", "max_age_seconds", "observed_at_pointer")
}

func decodeInformationQuestion(raw []byte) (InformationQuestion, error) {
	var question InformationQuestion
	validator, err := api.NewValidator(InformationQuestionSchema())
	if err != nil {
		return question, err
	}
	if err = validator.Validate(raw); err != nil {
		return question, err
	}
	if err = api.Decode(raw, &question); err != nil {
		return question, err
	}
	seenClaims := map[string]bool{}
	for _, claim := range question.Claims {
		if claim.Key == "" || len(claim.Key) > 64 || len(claim.JSONPointer) > 512 || claim.ExpectedValue != nil && len(*claim.ExpectedValue) > 4096 || seenClaims[claim.Key] || !strings.HasPrefix(claim.JSONPointer, "/") {
			return question, api.E("invalid_request", "information_question_claim_invalid")
		}
		seenClaims[claim.Key] = true
	}
	if len(question.ObservedAtPointer) > 512 || question.ObservedAtPointer != "" && !strings.HasPrefix(question.ObservedAtPointer, "/") {
		return question, api.E("invalid_request", "information_question_time_pointer_invalid")
	}
	seenSources := map[string]bool{}
	for _, source := range question.Sources {
		key, err := api.Digest(source)
		if err != nil {
			return question, err
		}
		if source.URL == "" || len(source.URL) > 2048 || seenSources[key] {
			return question, api.E("invalid_request", "information_question_source_invalid")
		}
		seenSources[key] = true
	}
	return question, nil
}

func (a *App) informationQuestion(ctx context.Context, scope runtime.Scope, goalRef api.ContentRef) (InformationQuestion, bool, error) {
	raw, err := a.goalBytes(ctx, scope, a.ServiceAuth, goalRef)
	if err != nil {
		return InformationQuestion{}, false, err
	}
	var goal brain.GoalSpec
	if api.Decode(raw, &goal) != nil || goal.Kind != "answer" {
		return InformationQuestion{}, false, nil
	}
	value, err := api.ParseJSONLimit([]byte(goal.Body), api.MaxJSONBytes)
	if err != nil {
		return InformationQuestion{}, false, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object["profile"] != providers.ReferenceProfile {
		return InformationQuestion{}, false, nil
	}
	if len(goal.Body) > 32<<10 {
		return InformationQuestion{}, true, api.E("invalid_request", "reference_question_bytes_limit")
	}
	if !a.Config.InformationReferenceAnswer {
		return InformationQuestion{}, true, api.E("unsupported", "information_reference_answer_not_configured")
	}
	question, err := decodeInformationQuestion([]byte(goal.Body))
	if err != nil {
		return question, true, err
	}
	for _, requested := range question.Sources {
		configured, err := a.configuredInformation(requested.SourceRef)
		if err != nil {
			return question, true, err
		}
		if configured.Source.BodyDriver() == nil {
			return question, true, api.E("unsupported", "reference_body_driver_required")
		}
	}
	return question, true, nil
}

// 只沿本Task已准入且效果关闭的原Operation/Attempt读来源，不接受模型自报Observation。
func (a *App) informationObservations(ctx context.Context, scope runtime.Scope, t api.Task, facts task.ContextFacts, question InformationQuestion) ([]providers.InformationObservation, error) {
	if len(facts.Operations) > 32 {
		return nil, api.E("dependency_unavailable", "reference_operation_scan_limit")
	}
	out := []providers.InformationObservation{}
	seen := map[string]bool{}
	for _, op := range facts.Operations {
		if op.Intent.GoalRevision != t.GoalRevision || op.Intent.TaskRef.ObjectID != t.TaskID || !op.Fact.Closed || op.Fact.MayApplyLater {
			continue
		}
		var admission actionAdmission
		_, err := a.Store.Read(ctx, scope, "platform.action_admissions", op.Intent.OperationID, 1, &admission)
		if api.IsCode(err, "not_found") {
			continue
		}
		if err != nil {
			return nil, err
		}
		if admission.Descriptor.Kind != providers.InformationBody || admission.Descriptor.Source == nil {
			continue
		}
		if !api.Equal(admission.Prepared.ArgumentsRef, op.Intent.ArgumentsRef) || !api.Equal(admission.Prepared.CapabilityRef, op.Intent.CapabilityRef) || !api.Equal(admission.Prepared.BindingRef, op.Intent.BindingRef) {
			return nil, api.E("forbidden", "reference_original_action_changed")
		}
		configured, err := a.configuredInformation(admission.Descriptor.Source.SourceRef)
		if err != nil {
			return nil, err
		}
		arguments, err := a.Memory.Read(ctx, scope, a.ServiceAuth, op.Intent.ArgumentsRef, providers.InformationPurpose)
		if err != nil {
			return nil, err
		}
		var input providers.BodyArguments
		if err = api.Decode(arguments, &input); err != nil {
			return nil, err
		}
		matched := false
		for _, requested := range question.Sources {
			matched = matched || api.Equal(requested.SourceRef, admission.Descriptor.Source.SourceRef) && requested.URL == input.URL
		}
		if !matched {
			continue
		}
		raw, err := a.query(ctx, "execution.get", op.Intent.OperationID, execution.OperationIDInput{OperationID: op.Intent.OperationID})
		if err != nil {
			return nil, err
		}
		var operation execution.OperationView
		if err = api.Decode(raw, &operation); err != nil {
			return nil, err
		}
		if !operation.Attempts.Exhausted || operation.Attempts.Partial || len(operation.Attempts.Gaps) > 0 || len(operation.Attempts.Items) > 16 {
			return nil, api.E("dependency_unavailable", "reference_attempt_collection_incomplete")
		}
		for _, attempt := range operation.Attempts.Items {
			if seen[attempt.AttemptID] {
				continue
			}
			if len(out) >= 16 {
				return nil, api.E("dependency_unavailable", "reference_evidence_limit")
			}
			observation, _, err := configured.Source.ReadEvidence(ctx, scope, a.ServiceAuth, providers.InformationEvidenceRef{SourceRef: configured.Config.Source.SourceRef, AttemptID: attempt.AttemptID})
			if err != nil {
				return nil, err
			}
			if observation.Action != providers.InformationBody || observation.URL != input.URL {
				return nil, api.E("forbidden", "reference_original_target_changed")
			}
			seen[attempt.AttemptID] = true
			out = append(out, observation)
		}
	}
	return out, nil
}

func (a *App) informationContext(ctx context.Context, scope runtime.Scope, t api.Task, facts task.ContextFacts) (informationContextPacket, []api.ContentRef, error) {
	question, handled, err := a.informationQuestion(ctx, scope, t.GoalRef)
	if err != nil || !handled {
		return informationContextPacket{}, nil, err
	}
	observations, err := a.informationObservations(ctx, scope, t, facts, question)
	if err != nil {
		return informationContextPacket{}, nil, err
	}
	sources := []api.ContentRef{}
	for _, o := range observations {
		if o.BodyRef != nil {
			sources = append(sources, *o.BodyRef)
		}
	}
	rule := component("source-reference-answer")
	return informationContextPacket{&question, &rule, observations}, uniqueSources(sources), nil
}

func (a *App) validateInformationRequirements(ctx context.Context, scope runtime.Scope, t api.Task, delta api.RequirementDelta) (task.ValidationReport, bool, error) {
	question, handled, err := a.informationQuestion(ctx, scope, t.GoalRef)
	out := task.ValidationReport{Valid: true, SemanticKeys: []string{}, ReasonCodes: []string{}}
	if err != nil || !handled {
		return out, handled, err
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, scope, a.ServiceAuth, t.TaskID)
	if err != nil {
		return out, true, err
	}
	goalSources, err := a.goalSourceEvidence(ctx, scope, t.GoalRef, facts.SourceRefs, nil)
	if err != nil {
		return out, true, err
	}
	sources := []api.ContentRef{t.GoalRef, delta.ReasonRef}
	for _, candidate := range delta.Candidates {
		if candidate.Kind != "quality" || !candidate.Required || candidate.RuleParametersRef == nil || len(candidate.OpenQuestions) > 0 || !api.Equal(candidate.RuleRef, component("source-reference-answer")) {
			out.Valid = false
			out.ReasonCodes = append(out.ReasonCodes, "original_reference_requirement_required")
		}
		statement, err := a.Memory.Read(ctx, scope, a.ServiceAuth, candidate.StatementRef, "task.context")
		if err != nil {
			return out, true, err
		}
		sources = append(sources, candidate.StatementRef)
		if candidate.RuleParametersRef != nil {
			parameters, err := a.Memory.Read(ctx, scope, a.ServiceAuth, *candidate.RuleParametersRef, "task.context")
			if err != nil {
				return out, true, err
			}
			actual, err := decodeInformationQuestion(parameters)
			if err != nil || !api.Equal(actual, question) {
				out.Valid = false
				out.ReasonCodes = append(out.ReasonCodes, "reference_question_changes_original_goal")
			}
			sources = append(sources, *candidate.RuleParametersRef)
		}
		matched := len(candidate.SourceRefs) > 0
		for _, candidateSource := range candidate.SourceRefs {
			original := false
			for _, goalSource := range goalSources {
				original = original || api.Equal(candidateSource, goalSource)
			}
			matched = matched && original
		}
		if !matched {
			out.Valid = false
			out.ReasonCodes = append(out.ReasonCodes, "original_goal_source_missing")
		}
		key, err := api.Digest(struct {
			Rule      api.ComponentRef    `json:"rule"`
			Question  InformationQuestion `json:"question"`
			Statement string              `json:"statement"`
			Required  bool                `json:"required"`
		}{candidate.RuleRef, question, norm.NFC.String(strings.Join(strings.Fields(strings.ToLower(string(statement))), " ")), candidate.Required})
		if err != nil {
			return out, true, err
		}
		out.SemanticKeys = append(out.SemanticKeys, key)
	}
	out.ReportRef, err = a.Publish(ctx, scope, a.ServiceAuth, stableID("content", "reference-validation/"+api.Hash(api.Raw(delta))), "application/json", api.Raw(out), sources, []api.ContentRef{})
	return out, true, err
}

func (a *App) informationCoverage(ctx context.Context, scope runtime.Scope, t api.Task) (api.GoalCoverage, bool, error) {
	question, handled, err := a.informationQuestion(ctx, scope, t.GoalRef)
	if err != nil || !handled {
		return api.GoalCoverage{}, handled, err
	}
	valid, required := true, false
	sources := []api.ContentRef{t.GoalRef}
	for _, requirement := range t.Requirements {
		if !requirement.Required {
			continue
		}
		if !api.Equal(requirement.RuleRef, component("source-reference-answer")) || requirement.RuleParametersRef == nil {
			valid = false
			continue
		}
		parameters, err := a.Memory.Read(ctx, scope, a.ServiceAuth, *requirement.RuleParametersRef, "task.context")
		if err != nil {
			return api.GoalCoverage{}, true, err
		}
		actual, err := decodeInformationQuestion(parameters)
		valid = valid && err == nil && api.Equal(actual, question)
		required = true
		sources = append(sources, *requirement.RuleParametersRef)
	}
	verdict := "fail"
	if valid && required {
		verdict = "pass"
	}
	report, err := a.Publish(ctx, scope, a.ServiceAuth, stableID("content", "reference-coverage/"+t.TaskID+"/"+t.RequirementsDigest), "application/json", api.Raw(struct {
		GoalRef            api.ContentRef      `json:"goal_ref"`
		RequirementsDigest string              `json:"requirements_digest"`
		Question           InformationQuestion `json:"question"`
		Verdict            string              `json:"verdict"`
	}{t.GoalRef, t.RequirementsDigest, question, verdict}), sources, []api.ContentRef{})
	if err != nil {
		return api.GoalCoverage{}, true, err
	}
	now, err := a.now(ctx, scope)
	return api.GoalCoverage{CoverageID: stableID("coverage", t.TaskID+"/"+t.RequirementsDigest), TaskRef: scope.Ref(t.TaskID, t.Revision), GoalRevision: t.GoalRevision, GoalRef: t.GoalRef, RequirementsDigest: t.RequirementsDigest, MappingReportRef: report, RuleRef: a.CoverageRule, EvaluatorRef: a.CoverageRule, Verdict: verdict, Applicability: "usable", CheckedAt: api.Time(now), Revision: 1}, true, err
}

type referenceEvidenceReader struct{ a *App }

func (r referenceEvidenceReader) ReadEvidence(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref providers.InformationEvidenceRef) (providers.InformationObservation, []byte, error) {
	configured, err := r.a.configuredInformation(ref.SourceRef)
	if err != nil {
		return providers.InformationObservation{}, nil, err
	}
	return configured.Source.ReadEvidence(ctx, scope, auth, ref)
}

func (a *App) checkInformationAnswer(ctx context.Context, scope runtime.Scope, t api.Task, req task.CheckRequest) (api.ConditionResult, bool, error) {
	var requirement *api.Requirement
	for i := range t.Requirements {
		candidate := &t.Requirements[i]
		if candidate.RequirementID == req.Input.RequirementRef.RequirementID && candidate.Revision == req.Input.RequirementRef.Revision {
			requirement = candidate
		}
	}
	if requirement == nil || !api.Equal(requirement.RuleRef, component("source-reference-answer")) {
		return api.ConditionResult{}, false, nil
	}
	question, handled, err := a.informationQuestion(ctx, scope, t.GoalRef)
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	if !handled || requirement.RuleParametersRef == nil {
		return api.ConditionResult{}, true, api.E("forbidden", "reference_original_question_required")
	}
	parameters, err := a.Memory.Read(ctx, scope, a.ServiceAuth, *requirement.RuleParametersRef, "task.context")
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	original, err := decodeInformationQuestion(parameters)
	if err != nil || !api.Equal(original, question) {
		return api.ConditionResult{}, true, api.E("forbidden", "reference_original_question_changed")
	}
	artifact, err := a.Memory.Read(ctx, scope, a.ServiceAuth, req.Input.ArtifactRef, "task.context")
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, scope, a.ServiceAuth, t.TaskID)
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	observations, err := a.informationObservations(ctx, scope, t, facts, question)
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	origins := []providers.InformationEvidenceRef{}
	evidence := append([]api.ContentRef{req.Input.ArtifactRef, *requirement.RuleParametersRef}, req.Input.EvidenceRefs...)
	for _, o := range observations {
		origins = append(origins, providers.InformationEvidenceRef{SourceRef: o.SourceRef, AttemptID: o.AttemptID})
		if o.BodyRef != nil {
			evidence = append(evidence, *o.BodyRef)
		}
	}
	evaluator, err := providers.NewReferenceEvaluator(providers.ReferenceConfig{Store: a.Store, Scope: scope, Reader: referenceEvidenceReader{a}})
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	assessment, err := evaluator.Assess(ctx, scope, a.ServiceAuth, providers.ReferenceQuestion{Claims: question.Claims, Evidence: origins, MaxAgeSeconds: question.MaxAgeSeconds, ObservedAtPointer: question.ObservedAtPointer})
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	var answer InformationAnswer
	validator, err := api.NewValidator(api.SchemaFor[InformationAnswer]())
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	valid := len(artifact) <= 128<<10 && validator.Validate(artifact) == nil && api.Decode(artifact, &answer) == nil && answer.Profile == providers.ReferenceProfile && assessment.Status == "supported" && len(answer.Answers) == len(question.Claims) && api.Equal(answer.Answers, assessment.Answers)
	// 这个严格参考profile要求原问题列出的每个源都支持每项，不把缺口藏入成功。
	for _, supported := range assessment.Answers {
		valid = valid && len(supported.Citations) == len(question.Sources)
	}
	verdict := "fail"
	if valid {
		verdict = "pass"
	}
	now, err := a.now(ctx, scope)
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	observed := now
	for _, supported := range assessment.Answers {
		for _, citation := range supported.Citations {
			at, err := api.ParseTime(citation.ObservedAt)
			if err != nil {
				return api.ConditionResult{}, true, err
			}
			if at.Before(observed) {
				observed = at
			}
		}
	}
	if now.Sub(observed) > time.Duration(question.MaxAgeSeconds)*time.Second {
		verdict = "fail"
	}
	scopeRef, err := a.Publish(ctx, scope, a.ServiceAuth, stableID("content", "reference-check/"+req.CheckID), "application/json", api.Raw(struct {
		TaskRef      api.ObjectRef                 `json:"task_ref"`
		GoalRevision uint64                        `json:"goal_revision"`
		Requirement  api.RequirementRef            `json:"requirement"`
		Artifact     api.ContentRef                `json:"artifact"`
		Question     InformationQuestion           `json:"question"`
		Assessment   providers.ReferenceAssessment `json:"assessment"`
		Verdict      string                        `json:"verdict"`
	}{scope.Ref(t.TaskID, t.Revision), t.GoalRevision, req.Input.RequirementRef, req.Input.ArtifactRef, question, assessment, verdict}), append(evidence, t.GoalRef), []api.ContentRef{})
	if err != nil {
		return api.ConditionResult{}, true, err
	}
	return api.ConditionResult{CheckID: req.CheckID, TaskID: t.TaskID, GoalRevision: t.GoalRevision, RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision, ArtifactRef: req.Input.ArtifactRef, RuleRef: requirement.RuleRef, EvaluatorRef: requirement.RuleRef, Verdict: verdict, Applicability: "usable", Basis: "verified", EvidenceRefs: uniqueSources(evidence), ObservedAt: api.Time(observed), ScopeRef: scopeRef, CheckedAt: api.Time(now)}, true, nil
}
