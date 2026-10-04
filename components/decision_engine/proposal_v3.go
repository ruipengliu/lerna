package decision_engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// A rule/3 case is one bounded fixture evaluation. It proposes changes only;
// the current Task revisions and original durable-start fee are preserved.
func (s *Service) calculateProposalV3(ctx context.Context, work Work, snapshot Snapshot, processed []v.ContentRef, first []byte, inputBytes int) completion {
	artifactOutput := 0
	if (snapshot.Rule != "delta_only" && snapshot.Rule != "actions_four" && snapshot.Rule != "input_request" && snapshot.Rule != "delta_candidate_result" && snapshot.Rule != "cannot_continue" && snapshot.Rule != "invalid_actions_depends_on") || len(snapshot.MaterialRefs) < 2 {
		return failedCompletion("proposal_invalid", inputBytes, artifactOutput, 1)
	}
	replacement := snapshot.RequirementRefs[0]
	proposal := v.Proposal{
		DecisionRef: work.Record.Ref, SnapshotRef: snapshot.Ref,
		GoalRevision: snapshot.GoalRevision, ControlRevision: snapshot.ControlRevision,
		ProcessedSourceRefs: processed,
		RequirementDelta:    []v.RequirementDelta{{LocalKey: "condition-replacement", StatementRef: snapshot.MaterialRefs[0], RuleRef: snapshot.MaterialRefs[1], SourceRefs: slices.Clone(processed), Kind: "output", Required: true, ReplacesRef: &replacement}},
		Advance:             v.NewProposalAdvanceNone(v.ProposalAdvanceNone{}),
	}
	if snapshot.Rule == "actions_four" || snapshot.Rule == "invalid_actions_depends_on" {
		if len(snapshot.CapabilityBindings) != 4 {
			return failedCompletion("proposal_invalid", inputBytes, artifactOutput, 1)
		}
		proposal.RequirementDelta = []v.RequirementDelta{}
		actions := make([]v.ProposalAction, 0, 4)
		for i, binding := range snapshot.CapabilityBindings {
			actions = append(actions, v.ProposalAction{LocalKey: v.ID("action-" + strconv.Itoa(i+1)), CapabilityRef: binding.CapabilityRef, BindingRef: binding.BindingRef, ArgumentsRef: binding.ArgumentsRef, Purpose: binding.Purpose, SourceRefs: slices.Clone(processed)})
		}
		proposal.Advance = v.NewProposalAdvanceActions(v.ProposalAdvanceActions{Actions: actions})
	}
	if snapshot.Rule == "input_request" {
		if len(snapshot.MaterialRefs) < 3 || len(snapshot.AnswerSchemaRefs) != 1 {
			return failedCompletion("proposal_invalid", inputBytes, artifactOutput, 1)
		}
		proposal.RequirementDelta = []v.RequirementDelta{}
		proposal.Advance = v.NewProposalAdvanceInputRequest(v.ProposalAdvanceInputRequest{QuestionRef: snapshot.MaterialRefs[2], AnswerSchemaRef: snapshot.AnswerSchemaRefs[0], Purpose: "clarification", PreviewRefs: []v.ContentRef{snapshot.MaterialRefs[0]}})
	}
	if snapshot.Rule == "cannot_continue" {
		proposal.RequirementDelta = []v.RequirementDelta{}
		proposal.Advance = v.NewProposalAdvanceCannotContinue(v.ProposalAdvanceCannotContinue{Reason: "fixture has no further applicable action", MissingRequirements: []v.RequirementRef{snapshot.RequirementRefs[0]}})
	}
	artifacts := []PreparedArtifact{}
	if snapshot.Rule == "delta_candidate_result" {
		body := append([]byte("fixture result: "), first...)
		key := publicationPrefix(work.Record) + "/artifact/0"
		ref, err := s.config.Publisher.PlanPublication(ctx, key, body, processed, work.Permission)
		if err != nil {
			return completion{inputBytes: inputBytes, outputBytes: len(body), ruleSteps: 1, err: err}
		}
		artifacts = append(artifacts, PreparedArtifact{Key: key, Ref: ref, Bytes: body})
		artifactOutput = len(body)
		evidence := make([]v.ProposalEvidence, 0, len(snapshot.RequirementRefs))
		for _, requirement := range snapshot.RequirementRefs {
			evidence = append(evidence, v.ProposalEvidence{RequirementRef: requirement, EvidenceRefs: []v.ContentRef{ref}})
		}
		proposal.Advance = v.NewProposalAdvanceCandidateResult(v.ProposalAdvanceCandidateResult{ArtifactRefs: []v.ContentRef{ref}, Evidence: evidence, Limitations: []string{}})
	}
	// Raw decoding is the same closed public codec used by callers. Source and
	// purpose inclusion follows here, rather than in a codec with database access.
	raw, err := json.Marshal(proposal)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, artifactOutput, 1)
	}
	if snapshot.Rule == "invalid_actions_depends_on" {
		// The immutable case deliberately emits a dependent action. This raw
		// field goes through the caller's closed decoder, never a canned failure.
		raw = bytes.Replace(raw, []byte(`"local_key":"action-2"`), []byte(`"local_key":"action-2","depends_on":["action-1"]`), 1)
	}
	decoded, err := v.Decode[v.Proposal](raw)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, artifactOutput+len(raw), 1)
	}
	if err = s.validateProposalV3(ctx, work, snapshot, processed, decoded, artifacts, &inputBytes); err != nil {
		reason := v.DecisionFailure("proposal_invalid")
		if errors.Is(err, ErrInputLimit) {
			reason = "input_over_limit"
		}
		return failedCompletion(reason, inputBytes, artifactOutput+len(raw), 1)
	}
	raw, err = v.Encode(decoded)
	if err != nil {
		return failedCompletion("output_over_limit", inputBytes, artifactOutput+len(raw), 1)
	}
	if !withinLimit(artifactOutput+len(raw), work.Record.Input.Limits.MaxOutputBytes) {
		return failedCompletion("output_over_limit", inputBytes, artifactOutput+len(raw), 1)
	}
	key := publicationPrefix(work.Record) + "/proposal"
	ref, err := s.config.Publisher.PlanPublication(ctx, key, raw, processed, work.Permission)
	if err != nil {
		return completion{inputBytes: inputBytes, outputBytes: artifactOutput + len(raw), ruleSteps: 1, err: err}
	}
	prepared := PreparedV2{StartSequence: work.Record.StartSequence, InputDigest: work.Record.InputDigest, Artifacts: artifacts, ProposalKey: key, ProposalRef: ref, Proposal: decoded, ProposalBytes: raw, Sources: processed}
	prepared.Digest, err = preparedV2Digest(prepared)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, artifactOutput+len(raw), 1)
	}
	preview := work.Record
	preview.Status, preview.Proposal, preview.ProposalRef = "completed", &decoded, &ref
	preview.ArtifactRefs = make([]v.ContentRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		preview.ArtifactRefs = append(preview.ArtifactRefs, artifact.Ref)
	}
	public, err := preview.Public()
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, artifactOutput+len(raw), 1)
	}
	if _, err = v.Encode(public); err != nil {
		return failedCompletion("output_over_limit", inputBytes, artifactOutput+len(raw), 1)
	}
	return completion{preparedV2: &prepared, inputBytes: inputBytes, outputBytes: artifactOutput + len(raw), ruleSteps: 1}
}

func (s *Service) validateProposalV3(ctx context.Context, work Work, snapshot Snapshot, processed []v.ContentRef, proposal v.Proposal, artifacts []PreparedArtifact, inputBytes *int) error {
	if proposal.DecisionRef != work.Record.Ref || proposal.SnapshotRef != snapshot.Ref || proposal.GoalRevision != snapshot.GoalRevision || proposal.ControlRevision != snapshot.ControlRevision || !reflect.DeepEqual(proposal.ProcessedSourceRefs, processed) {
		return ErrForbidden
	}
	if proposal.DisclosedSourceRefs != nil {
		for _, ref := range *proposal.DisclosedSourceRefs {
			if !slices.Contains(processed, ref) {
				return ErrForbidden
			}
		}
	}
	cap, err := strconv.ParseInt(string(work.Record.Input.Limits.MaxInputBytes), 10, 64)
	if err != nil {
		return ErrInputLimit
	}
	for _, delta := range proposal.RequirementDelta {
		if delta.ReplacesRef != nil && !slices.Contains(snapshot.RequirementRefs, *delta.ReplacesRef) {
			return ErrForbidden
		}
		for _, ref := range delta.SourceRefs {
			if !slices.Contains(processed, ref) {
				return ErrForbidden
			}
		}
		for _, ref := range []v.ContentRef{delta.StatementRef, delta.RuleRef} {
			if !slices.Contains(proposalMaterialRefs(snapshot), ref) {
				return ErrForbidden
			}
			body, err := s.config.Source.ReadMaterial(ctx, ref, "rule.condition", work.Permission, cap-int64(*inputBytes))
			*inputBytes += len(body)
			if err != nil {
				return err
			}
			if hash(body) != ref.Hash || strconv.Itoa(len(body)) != string(ref.ByteLength) {
				return ErrForbidden
			}
		}
	}
	if _, ok := proposal.Advance.AsNone(); ok {
		if len(proposal.RequirementDelta) == 0 {
			return ErrForbidden
		}
	} else if advance, ok := proposal.Advance.AsActions(); ok {
		if !withinLimit(len(advance.Actions), work.Record.Input.Limits.MaxActions) {
			return ErrForbidden
		}
		for _, action := range advance.Actions {
			binding := CapabilityBinding{CapabilityRef: action.CapabilityRef, BindingRef: action.BindingRef, ArgumentsRef: action.ArgumentsRef, Purpose: action.Purpose}
			if !slices.Contains(snapshot.CapabilityBindings, binding) || !slices.Contains(proposalMaterialRefs(snapshot), action.ArgumentsRef) {
				return ErrForbidden
			}
			for _, ref := range action.SourceRefs {
				if !slices.Contains(processed, ref) {
					return ErrForbidden
				}
			}
			body, err := s.config.Source.ReadMaterial(ctx, action.ArgumentsRef, action.Purpose, work.Permission, cap-int64(*inputBytes))
			*inputBytes += len(body)
			if err != nil {
				return err
			}
			if hash(body) != action.ArgumentsRef.Hash || strconv.Itoa(len(body)) != string(action.ArgumentsRef.ByteLength) {
				return ErrForbidden
			}
		}
	} else if candidate, ok := proposal.Advance.AsCandidateResult(); ok {
		known := make([]v.ContentRef, 0, len(artifacts))
		for _, artifact := range artifacts {
			known = append(known, artifact.Ref)
		}
		for _, ref := range candidate.ArtifactRefs {
			if !slices.Contains(known, ref) {
				return ErrForbidden
			}
		}
		for _, evidence := range candidate.Evidence {
			if !slices.Contains(snapshot.RequirementRefs, evidence.RequirementRef) {
				return ErrForbidden
			}
			for _, ref := range evidence.EvidenceRefs {
				if slices.Contains(known, ref) {
					continue
				}
				if !slices.Contains(proposalMaterialRefs(snapshot), ref) {
					return ErrForbidden
				}
				body, err := s.config.Source.ReadMaterial(ctx, ref, "rule.evidence", work.Permission, cap-int64(*inputBytes))
				*inputBytes += len(body)
				if err != nil {
					return err
				}
				if hash(body) != ref.Hash || strconv.Itoa(len(body)) != string(ref.ByteLength) {
					return ErrForbidden
				}
			}
		}
	} else if request, ok := proposal.Advance.AsInputRequest(); ok {
		if request.Purpose != "clarification" || !slices.Contains(snapshot.MaterialRefs, request.QuestionRef) || !slices.Contains(snapshot.AnswerSchemaRefs, request.AnswerSchemaRef) {
			return ErrForbidden
		}
		materials := []struct {
			ref     v.ContentRef
			purpose string
		}{{request.QuestionRef, "rule.question"}, {request.AnswerSchemaRef, "rule.answer_schema"}}
		for _, ref := range request.PreviewRefs {
			if !slices.Contains(snapshot.MaterialRefs, ref) {
				return ErrForbidden
			}
			materials = append(materials, struct {
				ref     v.ContentRef
				purpose string
			}{ref, "rule.preview"})
		}
		for _, material := range materials {
			body, err := s.config.Source.ReadMaterial(ctx, material.ref, material.purpose, work.Permission, cap-int64(*inputBytes))
			*inputBytes += len(body)
			if err != nil {
				return err
			}
			if hash(body) != material.ref.Hash || strconv.Itoa(len(body)) != string(material.ref.ByteLength) {
				return ErrForbidden
			}
			if material.purpose == "rule.answer_schema" {
				if err := validateFixtureAnswerSchema(body); err != nil {
					return err
				}
			}
		}
	} else if cannot, ok := proposal.Advance.AsCannotContinue(); ok {
		for _, ref := range cannot.MissingRequirements {
			if !slices.Contains(snapshot.RequirementRefs, ref) {
				return ErrForbidden
			}
		}
		if cannot.ArtifactRefs != nil {
			for _, ref := range *cannot.ArtifactRefs {
				if !slices.ContainsFunc(artifacts, func(artifact PreparedArtifact) bool { return artifact.Ref == ref }) {
					return ErrForbidden
				}
			}
		}
	} else {
		return ErrForbidden
	}
	return ctx.Err()
}

// Rule/3 processes the entire finite declared fixture material set, including
// schema and argument refs that need not also occur in MaterialRefs. No refs
// are silently clipped to fit Proposal's source bound.
func proposalMaterialRefs(snapshot Snapshot) []v.ContentRef {
	refs := slices.Clone(snapshot.MaterialRefs)
	for _, binding := range snapshot.CapabilityBindings {
		if !slices.Contains(refs, binding.ArgumentsRef) {
			refs = append(refs, binding.ArgumentsRef)
		}
	}
	for _, ref := range snapshot.AnswerSchemaRefs {
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs
}

// The fixture accepts a closed string schema with a finite maxLength (1..256)
// and an optional bounded minLength. Schema metadata uses JSON numbers; this
// grammar never resolves URLs/$ref or loads code and rejects duplicate keys.
func validateFixtureAnswerSchema(body []byte) error {
	if len(body) > 4096 {
		return ErrForbidden
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.UseNumber()
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return ErrForbidden
	}
	seen := map[string]bool{}
	minimum, maximum := 0, 0
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return ErrForbidden
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ErrForbidden
		}
		seen[key] = true
		value, err := d.Token()
		if err != nil {
			return ErrForbidden
		}
		switch key {
		case "type":
			if value != "string" {
				return ErrForbidden
			}
		case "minLength", "maxLength":
			number, ok := value.(json.Number)
			if !ok {
				return ErrForbidden
			}
			n, err := strconv.Atoi(string(number))
			if err != nil || n < 0 || n > 256 {
				return ErrForbidden
			}
			if key == "minLength" {
				minimum = n
			} else {
				maximum = n
			}
		default:
			return ErrForbidden
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || !seen["type"] || !seen["maxLength"] || maximum < 1 || minimum > maximum {
		return ErrForbidden
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrForbidden
	}
	return nil
}
