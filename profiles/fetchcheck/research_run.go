package fetchcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/catalogauth"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/executionlocal"
	"lerna/adapters/fetchexecution"
	"lerna/adapters/fetchtask"
	"lerna/adapters/sqlitecatalog"
	"lerna/brain"
	"lerna/catalog"
	"lerna/execution"
	"lerna/fetch"
	"lerna/internal/randomid"
	"lerna/sdk"
	"lerna/tasks"
	"path/filepath"
	"strings"
	"time"
)

// researchRunSpec supplies an already configured host with task inputs.
type researchRunSpec struct {
	Resume                       tasks.Ref            // Existing public runtime identity; never submit again.
	PersistTask                  bool                 // Public runtime records identity before any external action.
	SearchConfig                 searchProviderConfig // Zero MaxBytes retains the reference default.
	Goal, Query, SearchEndpoint  string
	Queries, NetworkLimit, Steps uint32
	AnswerModel                  brain.Model
	ModelTokens                  uint64 // Zero retains the reference task budget.
}

// researchRun owns the action-to-answer handoff and catalog lifetime.
// It stays private; public callers run the whole task through runPreparedResearch.
type researchRun struct {
	h              *harness
	task           tasks.Task
	store          *sqlitecatalog.Store
	port           *tasks.ActionPort
	model          *researchActionModel
	runner         *brain.ActionBrain
	env            *researchActionHost
	ready          tasks.RunSnapshot
	complete       tasks.RunSnapshot
	prior          researchOutcomeReader
	searchConfig   searchProviderConfig
	networkCharged uint32
}

func (r *researchRun) close() {
	if r.store != nil {
		r.store.Close()
	}
}

func runPreparedResearch(ctx context.Context, h *harness, spec researchRunSpec) (record ResearchRecord, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run, err := startResearch(ctx, h, spec)
	defer run.close()
	defer func() {
		if failure != nil {
			record = run.failureRecord()
		}
	}()
	if err != nil {
		return ResearchRecord{}, err
	}
	return run.finish(ctx, spec.AnswerModel)
}

// startResearch stops at Core's durable answer qualification. It neither
// simulates restarts nor chooses a fixture model or expected result.
func startResearch(ctx context.Context, h *harness, spec researchRunSpec) (*researchRun, error) {
	r := &researchRun{h: h}
	if h == nil || spec.AnswerModel == nil || spec.Goal == "" || spec.Query == "" || spec.Queries < 1 || spec.Queries > 128 || spec.NetworkLimit < 1 || spec.NetworkLimit > 128 || spec.Steps < 1 || spec.Steps > 12 {
		return r, fetch.Invalid
	}
	goal, query, queries, networkLimit, steps := spec.Goal, spec.Query, spec.Queries, spec.NetworkLimit, spec.Steps
	searchConfig := spec.SearchConfig
	if searchConfig.MaxResults == 0 {
		searchConfig.MaxResults = 4
	}
	if searchConfig.TimeoutMS == 0 {
		searchConfig.TimeoutMS = 1000
	}
	if searchConfig.MaxBytes == 0 {
		searchConfig.MaxBytes = 1024
	}
	if searchConfig.Recipient == "" {
		searchConfig.Recipient = "local"
	}
	r.searchConfig = searchConfig
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: steps, MaxQueries: queries, InputTokens: 8192, OutputTokens: 1024})
	if err != nil {
		return r, err
	}
	content, err := h.access.WithQueries(port)
	if err != nil {
		return r, err
	}
	scope, err := acquisitionQueries(h, port, h.cap)
	if err != nil {
		return r, err
	}
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		return r, err
	}
	h.target, err = fetchexecution.New(h.http, h.attempts, h.evidence, h.auth, fetchexecution.Config{Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: int64(h.pageMaxBytes), MaxRequests: min(uint32(2), networkLimit), TaskLimit: networkLimit, Timeout: time.Second})
	if err != nil {
		return r, err
	}
	h.target, err = h.target.WithObservations(scope)
	if err != nil {
		return r, err
	}
	h.exec, err = execution.New(h.grants, h.work, content, h.target, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return r, err
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	searchHost, err := bindConfiguredSearchHost(h, spec.SearchEndpoint, networkLimit, port, searchConfig)
	if err != nil {
		return r, err
	}
	for _, service := range []*execution.Service{h.exec, searchHost.h.exec} {
		if err := h.lifetime.track(service); err != nil {
			return r, err
		}
	}
	source := catalog.Source{Kind: "capability", Key: "fetch", Revision: 1}
	rules := []contentpolicy.Rule{}
	for _, s := range []catalog.Source{source, {Kind: "query", Key: "fetch-goal", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: s.Kind, Key: s.Key, Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(5 * time.Minute).Unix()})
	}
	policy, err := contentpolicy.New(rules)
	if err != nil {
		return r, err
	}
	store, err := sqlitecatalog.Open(filepath.Join(h.root, "fetch-catalog.db"), func(e catalog.Entry) error {
		if e.Ref.Digest != h.cap.Digest() && e.Ref.Digest != searchHost.h.cap.Digest() {
			return fetch.Denied
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	r.store = store
	entry := catalog.Entry{Source: source, Ref: catalog.Ref{Namespace: "local", Name: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, Digest: h.cap.Digest()}, Title: "Acquire bounded evidence", Category: "fetch", ResourceType: "web", Purpose: "task", Location: "local", Resource: "root", Preconditions: "Authorized source and bounded input", Effects: "Retain actual HTTP evidence", Unsupported: "Arbitrary sources", Guarantees: "Original operation recovery", Available: true, Capability: h.cap}
	searchEntry := entry
	searchEntry.Capability = searchHost.h.cap
	searchEntry.Ref.Name = searchHost.h.cap.Name
	searchEntry.Ref.Digest = searchHost.h.cap.Digest()
	searchEntry.Ref.Implementation = searchHost.h.cap.Implementation
	if spec.Resume.TaskID == "" {
		if _, err = store.Replace(ctx, 0, []catalog.Entry{entry, searchEntry}); err != nil {
			return r, err
		}
	}
	directory, err := catalog.New(store, catalogauth.Adapter{Authority: h.auth, Policy: policy, Clock: h.clock, QuerySource: catalog.Source{Kind: "query", Key: "fetch-goal", Revision: 1}}, catalog.Config{MaxScan: 2, MaxPage: 2, MaxCandidates: 2, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: h.token, Purpose: "task", Location: "local"})
	if err != nil {
		return r, err
	}
	var task tasks.Task
	if spec.Resume.TaskID != "" {
		task, err = h.core.Get(ctx, h.token, spec.Resume)
		if err != nil {
			return r, err
		}
		if task.Goal != goal || task.Constraints.MaxSteps != steps || task.Constraints.ModelRequests != 3 || task.Constraints.ModelTokens != spec.ModelTokens {
			return r, fetch.Invalid
		}
	} else {
		raw, _ := json.Marshal(map[string]any{"query": query})
		input, err := h.put(ctx, raw)
		if err != nil {
			return r, err
		}
		op, err := h.operation(ctx)
		if err != nil {
			return r, err
		}
		modelBudget := uint32(3)
		modelTokens := spec.ModelTokens
		if modelTokens == 0 {
			modelTokens = 32768
		}
		task, err = h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: goal, InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: steps, ModelRequests: modelBudget, ModelTokens: modelTokens, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
		if err != nil {
			return r, err
		}
		if spec.PersistTask {
			if err := bindRunConfiguration(ctx, h.root, "research-task.json", true, runtimeTaskManifest{Version: 1, Ref: task.Ref}); err != nil {
				return r, err
			}
		}
	}

	r.task = task
	run, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		return r, err
	}
	if spec.Resume.TaskID == "" || run.Task.State == "QUEUED" {
		for _, kind := range []string{"claim", "start"} {
			run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
			if err != nil {
				return r, err
			}
		}
		if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
			return r, err
		}
	} else if run.Actions == nil {
		if run.Task.State != "RUNNING" || len(run.Work) != 1 {
			return r, brain.Error("RESULT_UNRESOLVED")
		}
		if !run.Work[0].InFlight {
			changeID, err := randomid.New()
			if err != nil {
				return r, err
			}
			run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: changeID, Kind: "start", Qualification: tasks.QualificationOf(run)})
			if err != nil {
				return r, err
			}
		}
		if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
			return r, err
		}
	} else if run.Actions.Limits != (tasks.ActionLimits{MaxOperations: steps, MaxQueries: queries, InputTokens: 8192, OutputTokens: 1024}) {
		return r, fetch.Invalid
	}
	model := &researchActionModel{searchResults: searchConfig.MaxResults, searchBytes: searchConfig.MaxBytes, searchTimeout: searchConfig.TimeoutMS, pageBytes: h.pageMaxBytes}
	base := &fetchActionHost{h: h, directory: directory, queries: port}
	searchHost.directory = directory
	searchHost.queries = port
	env := &researchActionHost{fetchActionHost: base, search: searchHost, task: task.Ref, outcomes: map[string]fetch.Outcome{}, discoveryEmpty: map[string]bool{}}
	runner, err := brain.NewActions(model, env, port)
	if err != nil {
		return r, err
	}
	ready, err := runner.Run(ctx, task.Ref)
	if err != nil {
		return r, fmt.Errorf("research actions: %w", err)
	}
	if ready.Actions == nil {
		return r, fmt.Errorf("research actions unavailable: state=%s", ready.Task.State)
	}
	if ready.Task.State != "RUNNING" || ready.Actions.AnswerQualification == nil {

		decisionError := ""
		if n := len(ready.Actions.Decisions); n > 0 && ready.Actions.Decisions[n-1].Record != nil {
			decisionError = ready.Actions.Decisions[n-1].Record.Error
		}
		return r, fmt.Errorf("search-fetch loop: state=%s reason=%s actions=%d models=%d queries=%d decision_error=%s execute_error=%v search_execute_error=%v err=%v", ready.Task.State, ready.Task.StopReason, len(ready.Actions.Actions), model.calls, len(ready.Actions.Queries), decisionError, env.lastError, env.search.lastError, err)
	}
	budget, err := h.attempts.Budget(ctx, task.Ref)
	if err != nil || budget.Charged > networkLimit {
		return r, fmt.Errorf("search/fetch did not share the original budget")
	}
	r.port, r.model, r.runner, r.env, r.ready = port, model, runner, env, ready
	r.prior = env
	r.networkCharged = budget.Charged
	return r, nil
}

func (r *researchRun) failureRecord() (record ResearchRecord) {
	if r.task.Ref.TaskID == "" {
		return record
	}
	h := r.h
	record.UsageStatus = "unavailable"
	record.SearchConfig = r.searchConfig
	if h == nil {
		return record
	}
	reportCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, e := h.core.Get(reportCtx, h.token, r.task.Ref); e != nil {
		return record
	}
	snapshot, e := h.core.Load(reportCtx, r.task.Ref)
	if e != nil {
		return record
	}
	b, e := h.attempts.Budget(reportCtx, r.task.Ref)
	if e != nil {
		return record
	}
	record.Usage = ResearchUsage{ModelRequests: uint64(snapshot.Task.ModelUsedRequests), ModelTokens: snapshot.Task.ModelUsedTokens, ReservedModelRequests: uint64(snapshot.Task.ModelReservedRequests), ReservedModelTokens: snapshot.Task.ModelReservedTokens, NetworkCharged: uint64(b.Charged), ExecutionWaitMillis: config().DriverTimeout.Milliseconds()}
	if snapshot.Actions != nil {
		record.Usage.ActionQueries = uint64(len(snapshot.Actions.Queries))
		record.Usage.QueryLimit = snapshot.Actions.Limits.MaxQueries
		for _, q := range snapshot.Actions.Queries {
			if strings.HasPrefix(q, "content/") {
				record.Usage.ContentQueries++
			}
			if strings.HasPrefix(q, "outcome/") {
				record.Usage.OutcomeQueries++
			}
		}
	}
	record.UsageStatus = "snapshot"
	return record
}

func (r *researchRun) finish(ctx context.Context, model brain.Model) (ResearchRecord, error) {
	h, task, ready, env, searchConfig := r.h, r.task, r.ready, r.env, r.searchConfig
	recorder := &recordingResearchModel{model: model}
	answer, err := processResearchAnswer(ctx, h, r.port, ready, recorder, r.prior, false)
	if err != nil {
		return ResearchRecord{}, err
	}
	complete, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		return ResearchRecord{}, err
	}
	if complete.Actions == nil {
		return ResearchRecord{}, fmt.Errorf("completed action history missing")
	}
	r.complete = complete
	usage := ResearchUsage{ExecutionWaitMillis: config().DriverTimeout.Milliseconds(), ReservedModelRequests: uint64(complete.Task.ModelReservedRequests), ReservedModelTokens: complete.Task.ModelReservedTokens, ModelRequests: uint64(complete.Task.ModelUsedRequests), ModelTokens: complete.Task.ModelUsedTokens, ActionQueries: uint64(len(complete.Actions.Queries)), NetworkCharged: uint64(r.networkCharged)}

	// Outcome counts come from the original observations already held by the
	// action host, not extra unmetered report reads or fixture-server counters.
	for _, action := range ready.Actions.Actions {
		outcome, known := env.outcomes[action.OperationID]
		if !known {
			return ResearchRecord{}, fmt.Errorf("missing original acquisition observation")
		}
		if outcome.Mode == "fixed-replay" {
			continue
		}
		if action.Descriptor == env.search.h.cap.Digest() {
			usage.SearchRequests += uint64(outcome.Requests)
		} else if action.Descriptor == h.cap.Digest() {
			usage.PageRequests += uint64(outcome.Requests)
		}
	}
	usage.QueryLimit = complete.Actions.Limits.MaxQueries
	for _, query := range complete.Actions.Queries {
		if strings.HasPrefix(query, "content/") {
			usage.ContentQueries++
		}
		if strings.HasPrefix(query, "outcome/") {
			usage.OutcomeQueries++
		}
	}
	return ResearchRecord{SearchConfig: searchConfig, Input: recorder.input, ModelOutput: recorder.output, Answer: answer, Usage: usage, AnswerModel: recorder.capabilities}, nil
}
