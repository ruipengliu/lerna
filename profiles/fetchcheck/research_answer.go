package fetchcheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/answers"
	"lerna/brain"
	"lerna/internal/randomid"
	"lerna/tasks"
	"time"
)

func processResearchAnswer(ctx context.Context, h *harness, port *tasks.ActionPort, run tasks.RunSnapshot, model brain.Model, prior researchOutcomeReader, recovering bool) (brain.EvidenceAnswer, error) {
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
	input, err := prepareResearchAnswerInput(ctx, h, port, reserved, location, prior)
	if err != nil {
		return brain.EvidenceAnswer{}, err
	}
	// The publication port already validates original task inputs through
	// output. Supply the evidence context separately to avoid duplicating
	// that same original-input observation at this boundary.
	publication, err := answers.BindEvidencePort(generations, input.output, location, input.evidence)
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
	writer, err := brain.NewEvidenceAnswer(model, input, input.output, generations, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: outputBytes, SettlementTimeout: time.Second})
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
	return actual, nil
}
