package decision_engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"

	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// A rule/3 case is one bounded fixture evaluation. It proposes changes only;
// the current Task revisions and original durable-start fee are preserved.
func (s *Service) calculateProposalV3(ctx context.Context, work Work, snapshot Snapshot, processed []v.ContentRef, inputBytes int) completion {
	if (snapshot.Rule != "delta_only" && snapshot.Rule != "actions_four") || len(snapshot.MaterialRefs) < 2 {
		return failedCompletion("proposal_invalid", inputBytes, 0, 1)
	}
	replacement := snapshot.RequirementRefs[0]
	proposal := v.Proposal{
		DecisionRef: work.Record.Ref, SnapshotRef: snapshot.Ref,
		GoalRevision: snapshot.GoalRevision, ControlRevision: snapshot.ControlRevision,
		ProcessedSourceRefs: processed,
		RequirementDelta:    []v.RequirementDelta{{LocalKey: "condition-replacement", StatementRef: snapshot.MaterialRefs[0], RuleRef: snapshot.MaterialRefs[1], SourceRefs: slices.Clone(processed), Kind: "output", Required: true, ReplacesRef: &replacement}},
		Advance:             v.NewProposalAdvanceNone(v.ProposalAdvanceNone{}),
	}
	if snapshot.Rule == "actions_four" {
		if len(snapshot.CapabilityBindings) != 4 {
			return failedCompletion("proposal_invalid", inputBytes, 0, 1)
		}
		proposal.RequirementDelta = []v.RequirementDelta{}
		actions := make([]v.ProposalAction, 0, 4)
		for i, binding := range snapshot.CapabilityBindings {
			actions = append(actions, v.ProposalAction{LocalKey: v.ID("action-" + strconv.Itoa(i+1)), CapabilityRef: binding.CapabilityRef, BindingRef: binding.BindingRef, ArgumentsRef: binding.ArgumentsRef, Purpose: binding.Purpose, SourceRefs: slices.Clone(processed)})
		}
		proposal.Advance = v.NewProposalAdvanceActions(v.ProposalAdvanceActions{Actions: actions})
	}
	// Raw decoding is the same closed public codec used by callers. Source and
	// purpose inclusion follows here, rather than in a codec with database access.
	raw, err := json.Marshal(proposal)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, 0, 1)
	}
	decoded, err := v.Decode[v.Proposal](raw)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, len(raw), 1)
	}
	if err = s.validateProposalV3(ctx, work, snapshot, processed, decoded, &inputBytes); err != nil {
		reason := v.DecisionFailure("proposal_invalid")
		if errors.Is(err, ErrInputLimit) {
			reason = "input_over_limit"
		}
		return failedCompletion(reason, inputBytes, len(raw), 1)
	}
	raw, err = v.Encode(decoded)
	if err != nil {
		return failedCompletion("output_over_limit", inputBytes, len(raw), 1)
	}
	if !withinLimit(len(raw), work.Record.Input.Limits.MaxOutputBytes) {
		return failedCompletion("output_over_limit", inputBytes, len(raw), 1)
	}
	key := publicationPrefix(work.Record) + "/proposal"
	ref, err := s.config.Publisher.PlanPublication(ctx, key, raw, processed, work.Permission)
	if err != nil {
		return completion{inputBytes: inputBytes, outputBytes: len(raw), ruleSteps: 1, err: err}
	}
	prepared := PreparedV2{StartSequence: work.Record.StartSequence, InputDigest: work.Record.InputDigest, Artifacts: []PreparedArtifact{}, ProposalKey: key, ProposalRef: ref, Proposal: decoded, ProposalBytes: raw, Sources: processed}
	prepared.Digest, err = preparedV2Digest(prepared)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, len(raw), 1)
	}
	preview := work.Record
	preview.Status, preview.Proposal, preview.ProposalRef = "completed", &decoded, &ref
	preview.ArtifactRefs = []v.ContentRef{}
	public, err := preview.Public()
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, len(raw), 1)
	}
	if _, err = v.Encode(public); err != nil {
		return failedCompletion("output_over_limit", inputBytes, len(raw), 1)
	}
	return completion{preparedV2: &prepared, inputBytes: inputBytes, outputBytes: len(raw), ruleSteps: 1}
}

func (s *Service) validateProposalV3(ctx context.Context, work Work, snapshot Snapshot, processed []v.ContentRef, proposal v.Proposal, inputBytes *int) error {
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
			if !slices.Contains(snapshot.MaterialRefs, ref) {
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
			if !slices.Contains(snapshot.CapabilityBindings, binding) || !slices.Contains(snapshot.MaterialRefs, action.ArgumentsRef) {
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
	} else {
		return ErrForbidden
	}
	return ctx.Err()
}
