package fetchcheck

import (
	"context"
	"fmt"
	"lerna/adapters/fetchoutput"
	"lerna/adapters/researchcontext"
	"lerna/adapters/researchlineage"
	"lerna/adapters/taskcontent"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"time"
)

type researchOutcomeReader interface {
	actionOutcome(context.Context, tasks.Qualification, string) (fetch.Outcome, bool, error)
}

// Page evidence does not authorize the original goal or submitted sources.
// Keep both checks at model disclosure and publication boundaries.
type researchAnswerInput struct {
	evidence brain.Context
	output   *answers.ContentAccess
}

func (c researchAnswerInput) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	if err := c.output.Validate(ctx, t, location); err != nil {
		return brain.Input{}, err
	}
	return c.evidence.Assemble(ctx, t, location, limit)
}
func (c researchAnswerInput) Validate(ctx context.Context, t tasks.Task, location string) error {
	if err := c.output.Validate(ctx, t, location); err != nil {
		return err
	}
	return c.evidence.Validate(ctx, t, location)
}

// prepareResearchAnswerInput binds one reserved decision's evidence and complete
// source lineage together. Context filtering must never discard retained sources.
func prepareResearchAnswerInput(ctx context.Context, h *harness, port *tasks.ActionPort, reserved tasks.RunSnapshot, location string, prior researchOutcomeReader) (*researchAnswerInput, error) {
	if reserved.Actions == nil || len(reserved.Actions.Actions) == 0 {
		return nil, fmt.Errorf("answer phase has no settled actions")
	}
	// Settled outcomes are immutable. The same serial task host may reuse facts
	// already observed during actions; a reopened host must read them again.
	// Current Content/source authority is still checked separately at each use.
	outcomes := make(map[string]fetch.Outcome, len(reserved.Actions.Actions))
	for _, action := range reserved.Actions.Actions {
		var observed fetch.Outcome
		var known bool
		var err error
		if prior != nil {
			observed, known, err = prior.actionOutcome(ctx, tasks.QualificationOf(reserved), action.OperationID)
		} else {
			observed, known, err = readResearchOutcome(ctx, h, port, tasks.QualificationOf(reserved), action.OperationID)
		}
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf("missing settled action result")
		}
		outcomes[action.OperationID] = observed
	}
	original := outcomes[reserved.Actions.Actions[len(reserved.Actions.Actions)-1].OperationID]
	search := []string{}
	pages := []string{}
	failures := []string{}
	failureFacts := map[string]fetch.Outcome{}
	for _, action := range reserved.Actions.Actions {
		observed := outcomes[action.OperationID]
		// Successful discovery is not page evidence. Its finite failure, however,
		// belongs in the same governed gap path as a failed page acquisition.
		if action.Descriptor != h.cap.Digest() && observed.Status == "acquired" {
			search = append(search, observed.Reference)
			continue
		}
		if observed.Status == "acquired" {
			pages = append(pages, observed.Reference)
		} else if fetch.IsFailureStatus(observed.Status) {
			ref := reserved.ExecutionReports[action.OperationID].Reference
			if ref == "" {
				return nil, fmt.Errorf("missing acquisition failure artifact")
			}
			failures = append(failures, ref)
			failureFacts[ref] = observed
		} else {
			return nil, fmt.Errorf("unresolved acquisition status")
		}
	}
	if len(failures) == 0 && !h.answerFromSearch {
		search = nil
	}
	if len(pages) == 0 && len(failures) == 0 && original.Status == "acquired" && reserved.Actions.Actions[len(reserved.Actions.Actions)-1].Descriptor != h.cap.Digest() {
		pages = nil
		search = []string{original.Reference}
	}
	content, err := taskcontent.New(h.content, port, tasks.QualificationOf(reserved))
	if err != nil {
		return nil, err
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: location}
	evidence, err := meteredResearchEvidenceAt(h, port, reserved, location)
	if err != nil {
		return nil, err
	}
	access, err := fetchoutput.New(content, binding, h.clock)
	if err != nil {
		return nil, err
	}
	reader, err := h.searchEvidence(evidence)
	if err != nil {
		return nil, err
	}
	failureCapability := h.cap
	failureCapability.Location = location
	input, err := researchcontext.New(evidence, reader, researchFailures{reader: access.Failures(h.token, failureCapability), expected: failureFacts}, reserved.Task, location, researchcontext.References{AnswerFromSearch: h.answerFromSearch, Search: search, Pages: pages, Failures: failures})
	if err != nil {
		return nil, err
	}
	goal := &wire.ContentSource{Kind: "task-goal", Key: "inline", Revision: 1}
	output, err := answers.NewContentAccess(content, h.policy, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, goal, "task", h.clock, time.Minute)
	if err != nil {
		return nil, err
	}
	dependencies := []string{}
	for _, action := range reserved.Actions.Actions {
		observed := outcomes[action.OperationID]
		ref := observed.Reference
		if observed.Status != "acquired" {
			ref = reserved.ExecutionReports[action.OperationID].Reference
		}
		dependencies = append(dependencies, ref)
	}
	lineage, err := researchlineage.New(content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, reserved.Task, "task", dependencies)
	if err != nil {
		return nil, err
	}
	output, err = output.WithLineage(lineage)
	if err != nil {
		return nil, err
	}
	return &researchAnswerInput{evidence: input, output: output}, nil
}

// Compare the controlled failure artifact with the original operation facts at
// the actual read boundary, without adding another unmetered preflight read.
type researchFailures struct {
	reader   researchcontext.FailureEvidence
	expected map[string]fetch.Outcome
}

func (r researchFailures) ReadFailure(ctx context.Context, ref string) (fetch.Outcome, error) {
	expected, ok := r.expected[ref]
	if !ok {
		return fetch.Outcome{}, fetch.Invalid
	}
	out, err := r.reader.ReadFailure(ctx, ref)
	if err != nil {
		return fetch.Outcome{}, err
	}
	if out != expected {
		return fetch.Outcome{}, fetch.Unavailable
	}
	return out, nil
}
