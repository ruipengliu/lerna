package fetchcheck

import (
	"context"
	"fmt"
	"lerna/brain"
	"lerna/fetch"
	"lerna/profiles/searchcheck"
	"net/http"
	"sync/atomic"
	"time"
)

type researchVerification struct {
	actions, decisions           int
	searchRequests, pageRequests int32
	searchHits, pageHits         *atomic.Int32
}

// checkPreparedResearchRun owns fixture expectations and simulated restarts.
// Both CLI checks and tests use the same runtime stages as real research tasks.
func checkPreparedResearchRun(ctx context.Context, h *harness, spec researchRunSpec, mode string, material *searchcheck.Case, reopen bool, verification *researchVerification) (record ResearchRecord, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	verifyFixture := spec.AnswerModel == nil
	if verifyFixture {
		spec.AnswerModel = decisionFixtureModel{evidence: true, missingCost: mode == "insufficient"}
	}
	run, err := startResearch(ctx, h, spec)
	defer run.close()
	var reopened *harness
	defer func() {
		if reopened != nil {
			reopened.close()
		}
	}()
	defer func() {
		if failure != nil {
			record = run.failureRecord()
			if verification != nil && record.UsageStatus == "snapshot" {
				record.Usage.SearchRequests = uint64(verification.searchHits.Load())
				record.Usage.PageRequests = uint64(verification.pageHits.Load())
			}
		}
	}()
	if err != nil {
		return ResearchRecord{}, err
	}
	ready, port, model, runner, searchConfig := run.ready, run.port, run.model, run.runner, run.searchConfig
	if verification != nil {
		if len(ready.Actions.Actions) != verification.actions || model.calls != verification.decisions || verification.searchHits.Load() != verification.searchRequests || verification.pageHits.Load() != verification.pageRequests {
			decisionError := ""
			if n := len(ready.Actions.Decisions); n > 0 && ready.Actions.Decisions[n-1].Record != nil {
				decisionError = ready.Actions.Decisions[n-1].Record.Error
			}
			return ResearchRecord{}, fmt.Errorf("search-fetch loop: state=%s reason=%s actions=%d models=%d queries=%d decision_error=%s execute_error=%v search_execute_error=%v err=%v", ready.Task.State, ready.Task.StopReason, len(ready.Actions.Actions), model.calls, len(ready.Actions.Queries), decisionError, run.env.lastError, run.env.search.lastError, err)
		}
		if run.networkCharged != uint32(verification.actions) {
			return ResearchRecord{}, fmt.Errorf("search/fetch did not share the original budget")
		}
	}
	task := run.task
	if material != nil {
		pageIndex := 0
		for _, action := range ready.Actions.Actions {
			if action.Descriptor != h.cap.Digest() {
				continue
			}
			observed, known, err := h.attempts.Outcome(ctx, task.Ref.Namespace, action.OperationID)
			if err != nil || !known {
				return ResearchRecord{}, fmt.Errorf("missing frozen page outcome")
			}
			if pageIndex >= len(material.Pages) {
				return ResearchRecord{}, fmt.Errorf("unexpected extra frozen page")
			}
			expected := material.Pages[pageIndex]
			pageIndex++
			if expected.Status == http.StatusOK {
				acquired, err := h.evidence.Read(ctx, observed.Reference)
				if err != nil || string(acquired.Body) != expected.Body || acquired.FetchedAt.IsZero() {
					return ResearchRecord{}, fmt.Errorf("actual acquired material differs from the frozen source")
				}
			} else if observed.Status == "acquired" {
				return ResearchRecord{}, fmt.Errorf("failed frozen page became acquired evidence")
			}
		}
		if pageIndex != len(material.Pages) {
			return ResearchRecord{}, fmt.Errorf("a frozen candidate was skipped")
		}
	}
	if reopen {
		h, ready, err = reopenResearchCheckpoint(ctx, h, ready)
		run.h = h
		if err != nil {
			return ResearchRecord{}, err
		}
		reopened = h
		port, err = h.work.Actions(ready.Actions.Limits)
		if err != nil {
			return ResearchRecord{}, err
		}
		h.clock.advance(11 * time.Second)
		// A restored answer phase must not revisit the action environment or
		// invoke even a fresh model. The original task already settled them.
		model = &researchActionModel{searchResults: searchConfig.MaxResults, searchBytes: searchConfig.MaxBytes, searchTimeout: searchConfig.TimeoutMS, pageBytes: h.pageMaxBytes}
		restoredHost := &fetchActionHost{h: h, directory: run.env.directory}
		runner, err = brain.NewActions(model, restoredHost, port)
		if err != nil {
			return ResearchRecord{}, err
		}
		restored, err := runner.Run(ctx, task.Ref)
		if err != nil || model.calls != 0 || restored.Task.ModelUsedRequests != ready.Task.ModelUsedRequests {
			return ResearchRecord{}, fmt.Errorf("restored answer phase repeated action planning: %v", err)
		}
		ready = restored
	}

	run.h, run.ready, run.port = h, ready, port
	if reopen {
		run.prior = nil
	}
	record, err = run.finish(ctx, spec.AnswerModel)
	if err != nil {
		return ResearchRecord{}, err
	}
	if verifyFixture {
		if err := verifyResearchFixtureAnswer(record.Answer, run, mode == "insufficient"); err != nil {
			return ResearchRecord{}, err
		}
	}
	callsBeforeReplay := model.calls
	if _, err = runner.Run(ctx, task.Ref); err != nil || model.calls != callsBeforeReplay || verification != nil && (verification.searchHits.Load() != verification.searchRequests || verification.pageHits.Load() != verification.pageRequests) {
		return ResearchRecord{}, fmt.Errorf("completed research task replayed work")
	}
	if reopen {
		complete := run.complete
		if len(ready.Work) != 1 || len(complete.Work) != 1 || complete.Work[0].Generation <= ready.Work[0].Generation {
			return ResearchRecord{}, fmt.Errorf("answer recovery did not fence the expired worker")
		}
		record.Recovery = &ResearchRecovery{OriginalWorkerGeneration: uint64(ready.Work[0].Generation), ResumedWorkerGeneration: uint64(complete.Work[0].Generation), ActionModelCallsAfterReopen: uint64(model.calls)}
	}
	return record, nil
}

// Expected fixture shape comes from the original action observations, without
// spending another query or letting model input/output define its own oracle.
func verifyResearchFixtureAnswer(actual brain.EvidenceAnswer, run *researchRun, missingCost bool) error {
	pages, search, failures := []string{}, []string{}, []string{}
	for _, action := range run.ready.Actions.Actions {
		observed, known := run.env.outcomes[action.OperationID]
		if !known {
			return fmt.Errorf("missing original acquisition observation")
		}
		if observed.Status == "acquired" {
			if action.Descriptor == run.h.cap.Digest() {
				pages = append(pages, observed.Reference)
			} else {
				search = append(search, observed.Reference)
			}
		} else if fetch.IsFailureStatus(observed.Status) {
			failures = append(failures, run.ready.ExecutionReports[action.OperationID].Reference)
		}
	}
	expectedStatus := "answerable"
	if missingCost {
		expectedStatus = "insufficient"
	}
	if len(pages) > 1 {
		expectedStatus = "conflicting"
	}
	if len(failures) > 0 {
		expectedStatus = "fetch_failed"
	}
	if len(pages) == 0 && len(failures) == 0 && len(search) > 0 {
		expectedStatus = "insufficient"
		search = search[len(search)-1:]
	}
	if actual.Status != expectedStatus {
		return fmt.Errorf("published answer lost observed outcome")
	}
	if expectedStatus == "conflicting" {
		if len(actual.Claims) != 2 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != 2 {
			return fmt.Errorf("answer discarded a conflicting source")
		}
		for i, claim := range actual.Claims {
			if len(claim.Citations) != 1 || claim.Citations[0].Source != pages[i] {
				return fmt.Errorf("conflict citation changed")
			}
		}
	}
	if expectedStatus == "insufficient" {
		refs := search
		if missingCost {
			refs = pages
		}
		if len(refs) != 1 || len(actual.Claims) != 0 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != 1 || actual.Gaps[0].Sources[0] != refs[0] {
			return fmt.Errorf("insufficient evidence became an asserted answer or lost its source")
		}
	}
	if expectedStatus == "fetch_failed" {
		if len(pages) == 0 && len(actual.Claims) != 0 || len(actual.Gaps) != 1 || len(actual.Gaps[0].Sources) != len(failures) {
			return fmt.Errorf("failed page became a claim or lost a failure source")
		}
		for i, ref := range failures {
			if actual.Gaps[0].Sources[i] != ref {
				return fmt.Errorf("answer replaced a failure source")
			}
		}
	}
	return nil
}
