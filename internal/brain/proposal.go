package brain

import (
	"encoding/json"
	"fmt"
	"github.com/ruipengliu/lerna/api"
	"regexp"
)

func ProposalSchema() api.Schema {
	base := map[string]any{"kind": api.String(), "reason_ref": api.Ref("ContentRef")}
	variant := func(kind string, extra map[string]any, required ...string) api.Schema {
		p := map[string]any{}
		for k, v := range base {
			p[k] = v
		}
		p["kind"] = map[string]any{"const": kind}
		for k, v := range extra {
			p[k] = v
		}
		return api.Object(p, append([]string{"kind", "reason_ref"}, required...)...)
	}
	return api.Schema{"oneOf": []any{
		variant("refine_requirements", map[string]any{"requirement_delta": api.Ref("RequirementDelta")}, "requirement_delta"),
		variant("act", map[string]any{"actions": api.Array(api.SchemaFor[ActionCandidate](), 1, 4), "requirement_delta": api.Ref("RequirementDelta")}, "actions"),
		variant("need_context", map[string]any{"lookups": api.Array(api.SchemaFor[ContextLookup](), 1, 3)}, "lookups"),
		variant("request_input", map[string]any{"question_ref": api.Ref("ContentRef"), "answer_schema_ref": api.Ref("ComponentRef"), "preview_refs": api.Array(api.Ref("ContentRef"), 1, 100), "purpose": api.Enum("clarify_goal", "supply_context")}, "question_ref", "answer_schema_ref", "preview_refs", "purpose"),
		variant("complete", map[string]any{"artifact_refs": api.Array(api.Ref("ContentRef"), 1, 100), "check_suggestions": api.Array(api.SchemaFor[CheckSuggestion](), 0, 100)}, "artifact_refs", "check_suggestions"),
		variant("fail", map[string]any{"reason_code": api.String(), "evidence_refs": api.Array(api.Ref("ContentRef"), 1, 100), "explanation_ref": api.Ref("ContentRef"), "resume_condition_ref": api.Ref("ContentRef")}, "reason_code", "evidence_refs", "explanation_ref"),
	}}
}

var localName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validateGenerated(g Generated) error {
	if len(g.Contents) == 0 || len(g.Contents) > 20 {
		return api.E("invalid_request", "publication_limit")
	}
	if e := api.ValidateAmounts(g.Usage); e != nil {
		return e
	}
	found := map[string]bool{}
	total := 0
	for _, c := range g.Contents {
		if !localName.MatchString(c.LocalID) || found[c.LocalID] || c.MediaType == "" {
			return api.E("invalid_request", "invalid_local_content")
		}
		if c.ContentLocalID != "" && !found[c.ContentLocalID] {
			return api.E("invalid_request", "publication_dependency_invalid")
		}
		found[c.LocalID] = true
		total += len(c.Body)
		if total > api.MaxJSONBytes {
			return api.E("invalid_request", "output_over_limit")
		}
	}
	if !found[g.Draft.ReasonLocalID] {
		return api.E("invalid_request", "reason_missing")
	}
	d := g.Draft
	switch d.Kind {
	case "refine_requirements":
		if len(d.Requirements) == 0 || len(d.Requirements) > 100 || len(d.Actions) > 0 || len(d.ArtifactLocalIDs) > 0 {
			return api.E("invalid_request", "invalid_refinement")
		}
		for _, r := range d.Requirements {
			if !localName.MatchString(r.CandidateKey) || !found[r.StatementLocalID] || r.ParametersLocalID != "" && !found[r.ParametersLocalID] {
				return api.E("invalid_request", "invalid_requirement_content")
			}
		}
	case "act":
		if len(d.Actions) < 1 || len(d.Actions) > 4 || len(d.Requirements) > 0 || len(d.ArtifactLocalIDs) > 0 {
			return api.E("invalid_request", "invalid_action_count")
		}
		keys := map[string]bool{}
		for _, a := range d.Actions {
			if !localName.MatchString(a.LocalKey) || keys[a.LocalKey] || !found[a.ArgumentsLocalID] {
				return api.E("invalid_request", "invalid_action_local_key")
			}
			keys[a.LocalKey] = true
		}
	case "complete":
		if len(d.ArtifactLocalIDs)+len(d.ExistingArtifactRefs) == 0 || len(d.Actions) > 0 || len(d.Requirements) > 0 {
			return api.E("invalid_request", "invalid_completion")
		}
		for _, id := range d.ArtifactLocalIDs {
			if !found[id] {
				return api.E("invalid_request", "artifact_missing")
			}
		}
	case "request_input":
		if !found[d.QuestionLocalID] || d.AnswerSchemaRef == nil || d.Purpose != "clarify_goal" && d.Purpose != "supply_context" || len(d.Actions) > 0 {
			return api.E("invalid_request", "invalid_input_request")
		}
	case "fail":
		if d.ReasonCode == "" || len(d.Actions) > 0 {
			return api.E("invalid_request", "invalid_failure")
		}
	default:
		return api.E("unsupported", "proposal_kind_not_supported")
	}
	return nil
}
func publicationBytes(d decision, c pendingContent) ([]byte, error) {
	if c.ContentLocalID == "" {
		return []byte(c.Body), nil
	}
	var referenced *api.ContentRef
	for _, p := range d.Publications {
		if p.LocalID == c.ContentLocalID {
			referenced = p.Ref
			break
		}
	}
	if referenced == nil {
		return nil, api.E("dependency_unavailable", "publication_dependency_pending")
	}
	var body map[string]json.RawMessage
	if e := api.Decode([]byte(c.Body), &body); e != nil {
		return nil, e
	}
	if _, exists := body["content_ref"]; exists {
		return nil, api.E("invalid_request", "local_content_reference_collision")
	}
	body["content_ref"] = api.Raw(*referenced)
	return api.Raw(body), nil
}
func materialize(d decision) (Proposal, error) {
	refs := map[string]api.ContentRef{}
	for _, p := range d.Publications {
		if p.Ref == nil {
			return Proposal{}, fmt.Errorf("unpublished local content")
		}
		refs[p.LocalID] = *p.Ref
	}
	draft := d.Generated.Draft
	out := Proposal{Kind: draft.Kind, ReasonRef: refs[draft.ReasonLocalID]}
	switch draft.Kind {
	case "refine_requirements":
		delta := api.RequirementDelta{BaseGoalRevision: d.Snapshot.GoalRevision, ReasonRef: out.ReasonRef, Candidates: []api.RequirementCandidate{}}
		for _, r := range draft.Requirements {
			c := api.RequirementCandidate{CandidateKey: r.CandidateKey, Kind: r.Kind, StatementRef: refs[r.StatementLocalID], SourceRefs: []api.SourceEvidence{{ContentRef: d.Snapshot.GoalRef, SourceKind: "user_input"}}, Origin: "derived", RuleRef: r.RuleRef, Required: r.Required, OpenQuestions: []string{}}
			if r.ParametersLocalID != "" {
				ref := refs[r.ParametersLocalID]
				c.RuleParametersRef = &ref
			}
			delta.Candidates = append(delta.Candidates, c)
		}
		out.RequirementDelta = &delta
	case "act":
		out.Actions = []ActionCandidate{}
		for _, a := range draft.Actions {
			out.Actions = append(out.Actions, ActionCandidate{LocalKey: a.LocalKey, CapabilityRef: a.CapabilityRef, BindingRef: a.BindingRef, ArgumentsRef: refs[a.ArgumentsLocalID], ProcessedSourceRefs: append(append([]api.ContentRef{}, d.Encoding.ProcessedSources...), publicationRefs(d)...), DisclosedSourceRefs: []api.ContentRef{}})
		}
	case "complete":
		out.ArtifactRefs = append([]api.ContentRef{}, draft.ExistingArtifactRefs...)
		for _, id := range draft.ArtifactLocalIDs {
			out.ArtifactRefs = append(out.ArtifactRefs, refs[id])
		}
		for _, r := range d.Snapshot.Requirements {
			out.CheckSuggestions = append(out.CheckSuggestions, CheckSuggestion{RequirementRef: api.RequirementRef{RequirementID: r.RequirementID, Revision: r.Revision}, EvidenceRefs: out.ArtifactRefs})
		}
	case "request_input":
		question := refs[draft.QuestionLocalID]
		out.QuestionRef = &question
		out.AnswerSchemaRef = draft.AnswerSchemaRef
		out.PreviewRefs = []api.ContentRef{question, d.Snapshot.GoalRef}
		out.Purpose = draft.Purpose
	case "fail":
		out.ReasonCode = draft.ReasonCode
		out.EvidenceRefs = []api.ContentRef{out.ReasonRef}
		ref := out.ReasonRef
		out.ExplanationRef = &ref
	}
	return out, nil
}

func publicationRefs(d decision) []api.ContentRef {
	out := []api.ContentRef{}
	for _, c := range d.Publications {
		if c.Ref != nil {
			out = append(out, *c.Ref)
		}
	}
	return out
}
