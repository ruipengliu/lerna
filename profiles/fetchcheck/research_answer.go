package fetchcheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"lerna/internal/randomid"
	"lerna/tasks"
	"time"
)

func finishResearchAnswer(ctx context.Context, h *harness, port *tasks.ActionPort, run tasks.RunSnapshot, model brain.Model, missingCost bool) (brain.EvidenceAnswer, error) {
	return publishResearchAnswer(ctx, h, port, run, model, missingCost, true, nil)
}

type researchOutcomeReader interface {
	actionOutcome(context.Context, tasks.Qualification, string) (fetch.Outcome, bool, error)
}

func publishResearchAnswer(ctx context.Context, h *harness, port *tasks.ActionPort, run tasks.RunSnapshot, model brain.Model, missingCost, verifyFixture bool, prior researchOutcomeReader) (brain.EvidenceAnswer, error) {
	return processResearchAnswer(ctx, h, port, run, model, missingCost, verifyFixture, prior, false)
}

func processResearchAnswer(ctx context.Context, h *harness, port *tasks.ActionPort, run tasks.RunSnapshot, model brain.Model, missingCost, verifyFixture bool, prior researchOutcomeReader, recovering bool) (brain.EvidenceAnswer, error) {
	location := model.Capabilities().Location
	inputTokens := max(uint64(2048), model.Capabilities().InputUpper)
	outputTokens := researchOutputLimit(h.answerOutputTokens)
	outputBytes := 4096
	if outputTokens > 512 {
		outputBytes = brain.MaxAnswerBytes
	}
	generations, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: inputTokens, OutputTokens: outputTokens})
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	reserved := run
	if recovering {
		if len(run.Generations) != 1 || run.Generations[0].Limits != (tasks.GenerationLimits{Requests: 1, InputTokens: inputTokens, OutputTokens: outputTokens}) {
			return brain.EvidenceAnswer{}, brain.Error("RESULT_UNRESOLVED")
		}
		op, err := randomid.New()
		if err != nil {
			return brain.EvidenceAnswer{}, err
		}
		// Renew only the original generation; expiry or replacement must not
		// create another generation to stand in for its unknown model outcome.
		reserved, err = generations.Commit(ctx, tasks.WorkChange{Kind: "renew", ChangeID: op, Qualification: tasks.QualificationOf(run)})
		if err != nil {
			return brain.EvidenceAnswer{}, err
		}
	} else {
		current, err := port.EnsureLease(ctx, tasks.QualificationOf(run))
		if err != nil {
			return brain.EvidenceAnswer{}, err
		}
		generations = generations.WithOutputIdentities(h.operation)
		reserved, err = generations.ReserveDecision(ctx, current)
		if err != nil {
			return brain.EvidenceAnswer{}, err
		}
	}
	stopLease := keepResearchAnswerLease(ctx, generations, tasks.QualificationOf(reserved))
	defer stopLease()
	if reserved.Actions == nil || len(reserved.Actions.Actions) == 0 {
		return brain.EvidenceAnswer{}, fmt.Errorf("answer phase has no settled actions")
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
			return brain.EvidenceAnswer{}, err
		}
		if !known {
			return brain.EvidenceAnswer{}, fmt.Errorf("missing settled action result")
		}
		outcomes[action.OperationID] = observed
	}
	original := outcomes[reserved.Actions.Actions[len(reserved.Actions.Actions)-1].OperationID]
	expectedStatus := "answerable"
	if missingCost {
		expectedStatus = "insufficient"
	}
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
				return brain.EvidenceAnswer{}, fmt.Errorf("missing acquisition failure artifact")
			}
			failures = append(failures, ref)
			failureFacts[ref] = observed
		} else {
			return brain.EvidenceAnswer{}, fmt.Errorf("unresolved acquisition status")
		}
	}
	if len(pages) > 1 {
		expectedStatus = "conflicting"
	}
	if len(failures) > 0 {
		expectedStatus = "fetch_failed"
	}
	if len(failures) == 0 && !h.answerFromSearch {
		search = nil
	}
	if len(pages) == 0 && len(failures) == 0 && original.Status == "acquired" && reserved.Actions.Actions[len(reserved.Actions.Actions)-1].Descriptor != h.cap.Digest() {
		expectedStatus = "insufficient"
		pages = nil
		search = []string{original.Reference}
	}
	content, err := taskcontent.New(h.content, port, tasks.QualificationOf(reserved))
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: location}
	evidence, err := meteredResearchEvidenceAt(h, port, reserved, location)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	access, err := fetchoutput.New(content, binding, h.clock)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	reader, err := h.searchEvidence(evidence)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	failureCapability := h.cap
	failureCapability.Location = location
	input, err := researchcontext.New(evidence, reader, researchFailures{reader: access.Failures(h.token, failureCapability), expected: failureFacts}, reserved.Task, location, researchcontext.References{AnswerFromSearch: h.answerFromSearch, Search: search, Pages: pages, Failures: failures})
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	goal := &wire.ContentSource{Kind: "task-goal", Key: "inline", Revision: 1}
	output, err := answers.NewContentAccess(content, h.policy, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, goal, "task", h.clock, time.Minute)
	if err != nil {
		return brain.EvidenceAnswer{}, err
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
		return brain.EvidenceAnswer{}, err
	}
	output, err = output.WithLineage(lineage)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	// The publication port already validates original task inputs through
	// output. Supply the evidence context separately to avoid duplicating
	// that same original-input observation at this boundary.
	publication, err := answers.BindEvidencePort(generations, output, location, input)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	// A reservation with no begun request can still dispatch its first request.
	// BeginRequest remains the atomic single-use gate if recovery callers race.
	// Begun or settled requests only recover saved output; never regenerate them.
	if recovering && (reserved.Generations[0].Started != 0 || reserved.Generations[0].Settled) {
		_, err := publication.Recover(ctx, run.Task.Ref)
		return brain.EvidenceAnswer{}, err
	}
	controlledInput := researchAnswerInput{evidence: input, taskInputs: output}
	writer, err := brain.NewEvidenceAnswer(model, controlledInput, output, generations, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: outputBytes, SettlementTimeout: time.Second})
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	g := reserved.Generations[len(reserved.Generations)-1]
	proposed, err := writer.Decide(ctx, tasks.DecisionInput{Task: reserved.Task, Work: reserved.Work[0], Generation: g})
	if err != nil {
		observed, observeErr := h.core.Load(ctx, reserved.Task.Ref)
		if observeErr == nil && observed.Actions != nil {
			return brain.EvidenceAnswer{}, fmt.Errorf("research answer decision failed: queries=%d/%d action_phase=%d state=%s cause=%w", len(observed.Actions.Queries), observed.Actions.Limits.MaxQueries, len(run.Actions.Queries), observed.Task.State, err)
		}
		return brain.EvidenceAnswer{}, err
	}
	// The writer returned a known saved proposal. Publish it directly; recovery
	// is for interrupted writers whose saved result must first be rediscovered.
	change := fmt.Sprintf("%x", sha256.Sum256([]byte(g.OutputOperation)))
	expectedRequests := run.Task.ModelUsedRequests + run.Task.ModelReservedRequests
	if !recovering {
		expectedRequests++
	}
	complete, err := publication.Commit(ctx, tasks.WorkChange{Kind: "complete", ChangeID: change, Qualification: g.Qualification, Finished: true, Proposal: proposed})
	if err != nil || complete.Task.State != "COMPLETED" || complete.Task.Result != proposed.Result || complete.Task.ModelUsedRequests+complete.Task.ModelReservedRequests != expectedRequests || complete.Actions == nil || len(complete.Actions.Actions) != len(run.Actions.Actions) {
		observed, observeErr := h.core.Load(ctx, reserved.Task.Ref)
		if observeErr == nil && observed.Actions != nil {
			return brain.EvidenceAnswer{}, fmt.Errorf("combined action and answer task failed: queries=%d/%d action_phase=%d state=%s cause=%v", len(observed.Actions.Queries), observed.Actions.Limits.MaxQueries, len(run.Actions.Queries), observed.Task.State, err)
		}
		return brain.EvidenceAnswer{}, fmt.Errorf("combined action and answer task failed: %v", err)
	}
	raw, err := h.access.Read(ctx, h.token, complete.Task.Result, h.cap)
	var actual brain.EvidenceAnswer
	if err != nil || json.Unmarshal(raw, &actual) != nil {
		return brain.EvidenceAnswer{}, fmt.Errorf("published answer lost observed outcome")
	}
	if complete.ExecutionReports[complete.Actions.Actions[0].OperationID].Reference == "" {
		return brain.EvidenceAnswer{}, fmt.Errorf("lost original execution report")
	}
	if !verifyFixture {
		return actual, nil
	}
	if actual.Status != expectedStatus {
		return brain.EvidenceAnswer{}, fmt.Errorf("published answer lost observed outcome")
	}
	if expectedStatus == "conflicting" {
		if len(actual.Claims) != 2 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != 2 {
			return brain.EvidenceAnswer{}, fmt.Errorf("answer discarded a conflicting source")
		}
		for i, claim := range actual.Claims {
			if len(claim.Citations) != 1 || claim.Citations[0].Source != pages[i] {
				return brain.EvidenceAnswer{}, fmt.Errorf("conflict citation changed")
			}
		}
	}
	if expectedStatus == "insufficient" {
		refs := search
		if missingCost {
			refs = pages
		}
		if len(refs) != 1 || len(actual.Claims) != 0 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != 1 || actual.Gaps[0].Sources[0] != refs[0] {
			return brain.EvidenceAnswer{}, fmt.Errorf("insufficient evidence became an asserted answer or lost its source")
		}
	}
	if expectedStatus == "fetch_failed" {
		if len(pages) == 0 && len(actual.Claims) != 0 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != len(failures) {
			return brain.EvidenceAnswer{}, fmt.Errorf("failed page became a claim or lost a failure source")
		}
		for i, ref := range failures {
			if actual.Gaps[0].Sources[i] != ref {
				return brain.EvidenceAnswer{}, fmt.Errorf("answer replaced a failure source")
			}
		}
	}
	if complete.ExecutionReports[complete.Actions.Actions[0].OperationID].Reference == "" {
		return brain.EvidenceAnswer{}, fmt.Errorf("lost original execution report")
	}
	return actual, nil
}
