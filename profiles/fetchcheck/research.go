package fetchcheck

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/adapters/catalogauth"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/executionlocal"
	"lerna/adapters/fetchauth"
	"lerna/adapters/fetchexecution"
	"lerna/adapters/fetchqueries"
	"lerna/adapters/fetchtask"
	"lerna/adapters/jsonsearch"
	"lerna/adapters/replayfetch"
	"lerna/adapters/searchcontext"
	"lerna/adapters/searchexecution"
	"lerna/adapters/searchprivacy"
	"lerna/adapters/sqlitecatalog"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/catalog"
	"lerna/execution"
	"lerna/fetch"
	"lerna/internal/randomid"
	"lerna/profiles/searchcheck"
	"lerna/schema"
	"lerna/sdk"
	"lerna/tasks"
	"lerna/websearch"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// CheckFrozenResearch runs one preregistered case through actual loopback HTTP,
// Core, SDK and governed publication with local protocol models. It does not
// contact public services, load credentials, or claim semantic quality.
func CheckFrozenResearch(ctx context.Context, caseID string) (ResearchRecord, error) {
	return checkFrozenResearch(ctx, caseID, false, false, nil)
}

// CheckFrozenResearchReplay uses the same task path with immutable local
// materials. A listener detects accidental HTTP; the replay adapter has none.
func CheckFrozenResearchReplay(ctx context.Context, caseID string) (ResearchRecord, error) {
	return checkFrozenResearch(ctx, caseID, true, false, nil)
}

// CheckFrozenResearchReopen closes and reopens the actual stores between the
// settled action phase and answer generation, then expires the old worker lease.
// This verifies persistence and lease recovery, not an actual process crash.
func CheckFrozenResearchReopen(ctx context.Context, caseID string) (ResearchRecord, error) {
	return checkFrozenResearch(ctx, caseID, false, true, nil)
}

// CheckFrozenResearchWithAnswerModel substitutes the answer model only. The
// action planner remains the bounded local protocol fixture. This reference
// host currently permits local model processing; remote disclosure requires a
// separately configured authority and evidence reader, not a relabelled model.
func CheckFrozenResearchWithAnswerModel(ctx context.Context, caseID string, model brain.Model) (ResearchRecord, error) {
	if err := validateResearchModel(model); err != nil {
		return ResearchRecord{}, err
	}
	return checkFrozenResearch(ctx, caseID, false, false, model)
}

func validateResearchModel(model brain.Model) error {
	if model == nil {
		return fmt.Errorf("answer model required")
	}
	cap := model.Capabilities()
	if cap.Location != "local" || cap.Model == "" || cap.Version == "" || !cap.Text || !cap.Structured || !cap.HardBounds || cap.InputUpper == 0 || cap.InputUpper > 2048 || cap.ContextTokens < 2560 {
		return fmt.Errorf("answer model incompatible with local reference limits")
	}
	return nil
}

func checkFrozenResearch(ctx context.Context, caseID string, fixed, reopen bool, answerModel brain.Model) (ResearchRecord, error) {
	return checkResearchBudget(ctx, caseID, fixed, reopen, answerModel, 64, "")
}

// ResearchConfig fixes reference observation and model budgets before admission.
// Zero model configuration preserves legacy local limits; explicit disclosure
// and token configuration allow a separately budgeted model at its real location.
type ResearchConfig struct {
	SearchMaxBytes uint32   // Zero retains the legacy 1024-byte response bound.
	DiscloseTo     []string // Trusted host locations for frozen public materials; never supplied by the model.
	ModelTokens    uint64   // Zero preserves the legacy reference budget and local model restrictions.
	Replay, Reopen bool
	MaxQueries     uint32
	AnswerModel    brain.Model
	SearchFormat   string // Empty/json or duckduckgo-html; reference material transport only.
}

// CheckReferenceResearch uses the public frozen materials with an explicitly
// configured runtime budget. It is not a result under the frozen-v1 plan.
func CheckReferenceResearch(ctx context.Context, caseID string, config ResearchConfig) (ResearchRecord, error) {
	if config.SearchMaxBytes > 1048576 {
		return ResearchRecord{}, fetch.Invalid
	}
	if config.ModelTokens == 0 && len(config.DiscloseTo) == 0 {
		if config.AnswerModel != nil {
			if err := validateResearchModel(config.AnswerModel); err != nil {
				return ResearchRecord{}, err
			}
		}
	} else {
		if config.AnswerModel == nil || config.ModelTokens == 0 || config.ModelTokens > 1048576 || len(config.DiscloseTo) > 16 {
			return ResearchRecord{}, fetch.Invalid
		}
		cap := config.AnswerModel.Capabilities()
		if err := validateResearchDisclosure(cap, config.ModelTokens, config.DiscloseTo, 512); err != nil {
			return ResearchRecord{}, err
		}
		if config.ModelTokens < 18432+max(uint64(2048), cap.InputUpper)+512 {
			return ResearchRecord{}, fetch.Invalid
		}
	}
	config.DiscloseTo = slices.Clone(config.DiscloseTo)
	return checkResearchConfigured(ctx, caseID, config)
}

func checkResearchBudget(ctx context.Context, caseID string, fixed, reopen bool, answerModel brain.Model, queries uint32, format string) (ResearchRecord, error) {
	return checkResearchConfigured(ctx, caseID, ResearchConfig{Replay: fixed, Reopen: reopen, AnswerModel: answerModel, MaxQueries: queries, SearchFormat: format})
}

func checkResearchConfigured(ctx context.Context, caseID string, config ResearchConfig) (ResearchRecord, error) {
	format, queries := config.SearchFormat, config.MaxQueries
	if format != "" && format != "json" && format != "duckduckgo-html" {
		return ResearchRecord{}, fmt.Errorf("unsupported search format")
	}
	if queries < 1 || queries > 128 {
		return ResearchRecord{}, fmt.Errorf("query allowance must be between 1 and 128")
	}
	if err := ctx.Err(); err != nil {
		return ResearchRecord{}, err
	}
	materials, err := searchcheck.LoadSources()
	if err != nil {
		return ResearchRecord{}, err
	}
	for _, material := range materials.Cases {
		if material.ID != caseID {
			continue
		}
		mode := caseID
		if mode == "answerable" {
			mode = "acquired"
		}
		if mode == "fetch_failed" {
			mode = "page_denied"
		}
		return runConfiguredResearchTask(ctx, mode, &material, config)
	}
	return ResearchRecord{}, fmt.Errorf("unknown frozen research case")
}
func runResearchTask(ctx context.Context, mode string, material *searchcheck.Case) (ResearchRecord, error) {
	return runResearchTaskBudget(ctx, mode, material, false, false, nil, 128)
}
func runResearchTaskMode(ctx context.Context, mode string, material *searchcheck.Case, fixed, reopen bool, answerModel brain.Model) (ResearchRecord, error) {
	return runResearchTaskBudget(ctx, mode, material, fixed, reopen, answerModel, 64)
}

func runResearchTaskBudget(ctx context.Context, mode string, material *searchcheck.Case, fixed, reopen bool, answerModel brain.Model, queries uint32) (ResearchRecord, error) {
	return runResearchTaskFormat(ctx, mode, material, fixed, reopen, answerModel, queries, "")
}
func runResearchTaskFormat(ctx context.Context, mode string, material *searchcheck.Case, fixed, reopen bool, answerModel brain.Model, queries uint32, format string) (ResearchRecord, error) {
	return runConfiguredResearchTask(ctx, mode, material, ResearchConfig{Replay: fixed, Reopen: reopen, AnswerModel: answerModel, MaxQueries: queries, SearchFormat: format})
}

func runConfiguredResearchTask(ctx context.Context, mode string, material *searchcheck.Case, config ResearchConfig) (ResearchRecord, error) {
	fixed, reopen, answerModel, queries, format := config.Replay, config.Reopen, config.AnswerModel, config.MaxQueries, config.SearchFormat
	searchPath := "/start"
	if format == "duckduckgo-html" {
		searchPath = "/html/"
	}

	multiplePages := mode == "conflicting" || mode == "first_page_denied" || mode == "second_page_denied" || mode == "both_pages_denied"
	query, goal := "history", "Acquire bounded evidence"
	if material != nil {
		query, goal = material.Query, material.Question
	}
	steps := uint32(3)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var hits atomic.Int32
	var searchHits atomic.Int32
	var pageURL, secondURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == searchPath {
			searchHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			candidates := []map[string]string{}
			if mode != "empty_search" {
				candidates = append(candidates, map[string]string{"url": pageURL, "title": "Actual page", "snippet": "Read the page to verify the fact"})
			}
			if multiplePages {
				candidates = append(candidates, map[string]string{"url": secondURL, "title": "Second record", "snippet": "A separate account"})
			}
			if material != nil {
				if r.URL.Query().Get("q") != material.Query {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				for i, page := range material.Pages {
					candidates[i]["title"] = page.Title
					candidates[i]["snippet"] = page.Snippet
				}
			}
			body, media, err := encodeReferenceSearch(format, candidates)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", media)
			w.Write(body)
			return
		}
		hits.Add(1)
		if material != nil {
			index := 0
			if r.URL.Path == "/second" {
				index = 1
			} else if r.URL.Path != "/final" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			page := material.Pages[index]
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(page.Status)
			w.Write([]byte(page.Body))
			return
		}
		if mode == "page_denied" || mode == "both_pages_denied" || mode == "first_page_denied" && r.URL.Path == "/final" || mode == "second_page_denied" && r.URL.Path == "/second" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if mode == "conflicting" {
			if r.URL.Path == "/final" {
				w.Write([]byte("The register says the bridge opened in 2001."))
			} else {
				w.Write([]byte("The archive says the bridge opened in 2003."))
			}
			return
		}
		w.Write([]byte("actual action response"))
	}))
	defer server.Close()
	pageURL = server.URL + "/final"
	secondURL = server.URL + "/second"
	expectedSearchRequests := int32(1)
	expectedPageRequests := int32(1)
	expectedActions := 2
	expectedDecisions := 2
	if multiplePages {
		expectedActions = 3
		expectedPageRequests = 2
	}
	if mode == "empty_search" {
		expectedActions = 1
		expectedDecisions = 1
		expectedPageRequests = 0
	}
	if mode == "candidate_denied" {
		pageURL = server.URL + "/forbidden"
		expectedPageRequests = 0
	}
	urls := []string{server.URL + searchPath + "?q=" + url.QueryEscape(query), server.URL + "/final"}
	networkLimit := uint32(2)
	if multiplePages {
		urls = append(urls, secondURL)
		networkLimit = 3
	}
	if fixed {
		expectedSearchRequests, expectedPageRequests = 0, 0
	}
	h, err := fresh(ctx, urls)
	if err != nil {
		return ResearchRecord{}, err
	}
	defer h.destroy()
	h.searchFormat = format
	if err := authorizeResearchRecipients(ctx, h, config.DiscloseTo); err != nil {
		return ResearchRecord{}, err
	}
	if fixed {
		resources := map[string]string{}
		for _, target := range urls {
			resources[target] = "root"
		}
		authority, err := fetchauth.New(h.auth, fetchauth.Scope{Token: h.token, Namespace: "local", Subject: "operator", Purpose: "task", Location: "local", Recipient: "local", Resources: resources})
		if err != nil {
			return ResearchRecord{}, err
		}
		entries := []replayfetch.Entry{}
		candidates := []map[string]string{}
		for i, page := range material.Pages {
			target := urls[i+1]
			candidates = append(candidates, map[string]string{"url": target, "title": page.Title, "snippet": page.Snippet})
			if page.Status == http.StatusForbidden {
				entries = append(entries, replayfetch.Entry{URL: target, Denied: true})
			} else {
				entries = append(entries, replayfetch.Entry{URL: target, Body: []byte(page.Body), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(page.Body)))})
			}
		}
		response, media, err := encodeReferenceSearch(format, candidates)
		if err != nil {
			return ResearchRecord{}, err
		}
		entries = append(entries, replayfetch.Entry{URL: urls[0], Body: response, SHA256: fmt.Sprintf("%x", sha256.Sum256(response)), MediaType: media})
		h.http, err = replayfetch.New(authority, h.clock, entries)
		if err != nil {
			return ResearchRecord{}, err
		}
		h.cap.Implementation = "fixed-replay"
	}

	return runPreparedResearch(ctx, h, researchRunSpec{
		SearchConfig: searchProviderConfig{MaxBytes: config.SearchMaxBytes}, Goal: goal, Query: query, SearchEndpoint: server.URL + searchPath, Queries: queries, NetworkLimit: networkLimit, Steps: steps,
		AnswerModel: answerModel, ModelTokens: config.ModelTokens, Reopen: reopen, Mode: mode, Material: material,
		verification: &researchVerification{actions: expectedActions, decisions: expectedDecisions, searchRequests: expectedSearchRequests, pageRequests: expectedPageRequests, searchHits: &searchHits, pageHits: &hits},
	})
}

// researchRunSpec supplies an already configured host with task inputs. Fixture
// material assertions are optional and never create sources for the execution.
type researchRunSpec struct {
	Resume                       tasks.Ref            // Existing public runtime identity; never submit again.
	PersistTask                  bool                 // Public runtime records identity before any external action.
	SearchConfig                 searchProviderConfig // Zero MaxBytes retains the reference default.
	Goal, Query, SearchEndpoint  string
	Queries, NetworkLimit, Steps uint32
	AnswerModel                  brain.Model
	ModelTokens                  uint64 // Zero retains the reference task budget.
	Reopen                       bool
	Mode                         string
	Material                     *searchcheck.Case
	verification                 *researchVerification
}
type researchVerification struct {
	actions, decisions           int
	searchRequests, pageRequests int32
	searchHits, pageHits         *atomic.Int32
}

// runPreparedResearch uses the same Core/ActionBrain/SDK/publication path for
// caller-supplied services and reference fixtures. Host policy remains authority.
func runPreparedResearch(ctx context.Context, h *harness, spec researchRunSpec) (record ResearchRecord, failure error) {
	var reopened *harness
	defer func() {
		if reopened != nil {
			reopened.close()
		}
	}()
	if h == nil || spec.Goal == "" || spec.Query == "" || spec.Queries < 1 || spec.Queries > 128 || spec.NetworkLimit < 1 || spec.NetworkLimit > 128 || spec.Steps < 1 || spec.Steps > 12 {
		return ResearchRecord{}, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	goal, query, queries, networkLimit, steps := spec.Goal, spec.Query, spec.Queries, spec.NetworkLimit, spec.Steps
	mode, material, reopen, answerModel := spec.Mode, spec.Material, spec.Reopen, spec.AnswerModel
	verification := spec.verification
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
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: steps, MaxQueries: queries, InputTokens: 8192, OutputTokens: 1024})
	if err != nil {
		return ResearchRecord{}, err
	}
	content, err := h.access.WithQueries(port)
	if err != nil {
		return ResearchRecord{}, err
	}
	scope, err := acquisitionQueries(h, port, h.cap)
	if err != nil {
		return ResearchRecord{}, err
	}
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		return ResearchRecord{}, err
	}
	h.target, err = fetchexecution.New(h.http, h.attempts, h.evidence, h.auth, fetchexecution.Config{Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: int64(h.pageMaxBytes), MaxRequests: min(uint32(2), networkLimit), TaskLimit: networkLimit, Timeout: time.Second})
	if err != nil {
		return ResearchRecord{}, err
	}
	h.target, err = h.target.WithObservations(scope)
	if err != nil {
		return ResearchRecord{}, err
	}
	h.exec, err = execution.New(h.grants, h.work, content, h.target, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return ResearchRecord{}, err
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	searchHost, err := bindConfiguredSearchHost(h, spec.SearchEndpoint, networkLimit, port, searchConfig)
	if err != nil {
		return ResearchRecord{}, err
	}
	for _, service := range []*execution.Service{h.exec, searchHost.h.exec} {
		if err := h.lifetime.track(service); err != nil {
			return ResearchRecord{}, err
		}
	}
	source := catalog.Source{Kind: "capability", Key: "fetch", Revision: 1}
	rules := []contentpolicy.Rule{}
	for _, s := range []catalog.Source{source, {Kind: "query", Key: "fetch-goal", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: s.Kind, Key: s.Key, Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(5 * time.Minute).Unix()})
	}
	policy, err := contentpolicy.New(rules)
	if err != nil {
		return ResearchRecord{}, err
	}
	store, err := sqlitecatalog.Open(filepath.Join(h.root, "fetch-catalog.db"), func(e catalog.Entry) error {
		if e.Ref.Digest != h.cap.Digest() && e.Ref.Digest != searchHost.h.cap.Digest() {
			return fetch.Denied
		}
		return nil
	})
	if err != nil {
		return ResearchRecord{}, err
	}
	defer store.Close()
	entry := catalog.Entry{Source: source, Ref: catalog.Ref{Namespace: "local", Name: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, Digest: h.cap.Digest()}, Title: "Acquire bounded evidence", Category: "fetch", ResourceType: "web", Purpose: "task", Location: "local", Resource: "root", Preconditions: "Authorized source and bounded input", Effects: "Retain actual HTTP evidence", Unsupported: "Arbitrary sources", Guarantees: "Original operation recovery", Available: true, Capability: h.cap}
	searchEntry := entry
	searchEntry.Capability = searchHost.h.cap
	searchEntry.Ref.Name = searchHost.h.cap.Name
	searchEntry.Ref.Digest = searchHost.h.cap.Digest()
	searchEntry.Ref.Implementation = searchHost.h.cap.Implementation
	if spec.Resume.TaskID == "" {
		if _, err = store.Replace(ctx, 0, []catalog.Entry{entry, searchEntry}); err != nil {
			return ResearchRecord{}, err
		}
	}
	directory, err := catalog.New(store, catalogauth.Adapter{Authority: h.auth, Policy: policy, Clock: h.clock, QuerySource: catalog.Source{Kind: "query", Key: "fetch-goal", Revision: 1}}, catalog.Config{MaxScan: 2, MaxPage: 2, MaxCandidates: 2, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: h.token, Purpose: "task", Location: "local"})
	if err != nil {
		return ResearchRecord{}, err
	}
	var task tasks.Task
	if spec.Resume.TaskID != "" {
		task, err = h.core.Get(ctx, h.token, spec.Resume)
		if err != nil {
			return ResearchRecord{}, err
		}
		if task.Goal != goal || task.Constraints.MaxSteps != steps || task.Constraints.ModelRequests != 3 || task.Constraints.ModelTokens != spec.ModelTokens {
			return ResearchRecord{}, fetch.Invalid
		}
	} else {
		raw, _ := json.Marshal(map[string]any{"query": query})
		input, err := h.put(ctx, raw)
		if err != nil {
			return ResearchRecord{}, err
		}
		op, err := h.operation(ctx)
		if err != nil {
			return ResearchRecord{}, err
		}
		modelBudget := uint32(3)
		modelTokens := spec.ModelTokens
		if modelTokens == 0 {
			modelTokens = 32768
		}
		task, err = h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: goal, InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: steps, ModelRequests: modelBudget, ModelTokens: modelTokens, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
		if err != nil {
			return ResearchRecord{}, err
		}
		if spec.PersistTask {
			if err := bindRunConfiguration(ctx, h.root, "research-task.json", true, runtimeTaskManifest{Version: 1, Ref: task.Ref}); err != nil {
				return ResearchRecord{}, err
			}
		}
	}

	defer func() {
		if failure == nil {
			return
		}
		record.UsageStatus = "unavailable"
		record.SearchConfig = searchConfig
		if h == nil {
			return
		}
		reportCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if _, e := h.core.Get(reportCtx, h.token, task.Ref); e != nil {
			return
		}
		snapshot, e := h.core.Load(reportCtx, task.Ref)
		if e != nil {
			return
		}
		b, e := h.attempts.Budget(reportCtx, task.Ref)
		if e != nil {
			return
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
		if verification != nil {
			record.Usage.SearchRequests = uint64(verification.searchHits.Load())
			record.Usage.PageRequests = uint64(verification.pageHits.Load())
		}
		record.UsageStatus = "snapshot"
	}()
	run, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		return ResearchRecord{}, err
	}
	if spec.Resume.TaskID == "" || run.Task.State == "QUEUED" {
		for _, kind := range []string{"claim", "start"} {
			run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
			if err != nil {
				return ResearchRecord{}, err
			}
		}
		if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
			return ResearchRecord{}, err
		}
	} else if run.Actions == nil {
		if run.Task.State != "RUNNING" || len(run.Work) != 1 {
			return ResearchRecord{}, brain.Error("RESULT_UNRESOLVED")
		}
		if !run.Work[0].InFlight {
			changeID, err := randomid.New()
			if err != nil {
				return ResearchRecord{}, err
			}
			run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: changeID, Kind: "start", Qualification: tasks.QualificationOf(run)})
			if err != nil {
				return ResearchRecord{}, err
			}
		}
		if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
			return ResearchRecord{}, err
		}
	} else if run.Actions.Limits != (tasks.ActionLimits{MaxOperations: steps, MaxQueries: queries, InputTokens: 8192, OutputTokens: 1024}) {
		return ResearchRecord{}, fetch.Invalid
	}
	model := &researchActionModel{searchResults: searchConfig.MaxResults, searchBytes: searchConfig.MaxBytes, searchTimeout: searchConfig.TimeoutMS, pageBytes: h.pageMaxBytes}
	base := &fetchActionHost{h: h, directory: directory, queries: port}
	searchHost.directory = directory
	searchHost.queries = port
	env := &researchActionHost{fetchActionHost: base, search: searchHost, task: task.Ref, outcomes: map[string]fetch.Outcome{}, discoveryEmpty: map[string]bool{}}
	runner, err := brain.NewActions(model, env, port)
	if err != nil {
		return ResearchRecord{}, err
	}
	ready, err := runner.Run(ctx, task.Ref)
	if err != nil {
		return ResearchRecord{}, fmt.Errorf("research actions: %w", err)
	}
	if ready.Actions == nil {
		return ResearchRecord{}, fmt.Errorf("research actions unavailable: state=%s", ready.Task.State)
	}
	if ready.Task.State != "RUNNING" || ready.Actions.AnswerQualification == nil || verification != nil && (len(ready.Actions.Actions) != verification.actions || model.calls != verification.decisions || verification.searchHits.Load() != verification.searchRequests || verification.pageHits.Load() != verification.pageRequests) {

		decisionError := ""
		if n := len(ready.Actions.Decisions); n > 0 && ready.Actions.Decisions[n-1].Record != nil {
			decisionError = ready.Actions.Decisions[n-1].Record.Error
		}
		return ResearchRecord{}, fmt.Errorf("search-fetch loop: state=%s reason=%s actions=%d models=%d queries=%d decision_error=%s execute_error=%v search_execute_error=%v err=%v", ready.Task.State, ready.Task.StopReason, len(ready.Actions.Actions), model.calls, len(ready.Actions.Queries), decisionError, env.lastError, env.search.lastError, err)
	}
	budget, err := h.attempts.Budget(ctx, task.Ref)
	if err != nil || budget.Charged > networkLimit || verification != nil && budget.Charged != uint32(verification.actions) {
		return ResearchRecord{}, fmt.Errorf("search/fetch did not share the original budget")
	}
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
		restoredHost := &fetchActionHost{h: h, directory: directory}
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
	verifyFixture := answerModel == nil
	if answerModel == nil {
		answerModel = decisionFixtureModel{evidence: true, missingCost: mode == "insufficient"}
	}
	recorder := &recordingResearchModel{model: answerModel}
	var prior researchOutcomeReader
	if !reopen {
		prior = env
	}
	answer, err := publishResearchAnswer(ctx, h, port, ready, recorder, mode == "insufficient", verifyFixture, prior)
	if err != nil {
		return ResearchRecord{}, err
	}
	callsBeforeReplay := model.calls
	if _, err = runner.Run(ctx, task.Ref); err != nil || model.calls != callsBeforeReplay || verification != nil && (verification.searchHits.Load() != verification.searchRequests || verification.pageHits.Load() != verification.pageRequests) {
		return ResearchRecord{}, fmt.Errorf("completed research task replayed work")
	}
	complete, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		return ResearchRecord{}, err
	}
	if complete.Actions == nil {
		return ResearchRecord{}, fmt.Errorf("completed action history missing")
	}
	var recovery *ResearchRecovery
	if reopen {
		if len(ready.Work) != 1 || len(complete.Work) != 1 || complete.Work[0].Generation <= ready.Work[0].Generation {
			return ResearchRecord{}, fmt.Errorf("answer recovery did not fence the expired worker")
		}
		recovery = &ResearchRecovery{OriginalWorkerGeneration: uint64(ready.Work[0].Generation), ResumedWorkerGeneration: uint64(complete.Work[0].Generation), ActionModelCallsAfterReopen: uint64(model.calls)}
	}
	usage := ResearchUsage{ExecutionWaitMillis: config().DriverTimeout.Milliseconds(), ReservedModelRequests: uint64(complete.Task.ModelReservedRequests), ReservedModelTokens: complete.Task.ModelReservedTokens, ModelRequests: uint64(complete.Task.ModelUsedRequests), ModelTokens: complete.Task.ModelUsedTokens, ActionQueries: uint64(len(complete.Actions.Queries)), NetworkCharged: uint64(budget.Charged)}

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
		if action.Descriptor == searchHost.h.cap.Digest() {
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
	return ResearchRecord{SearchConfig: searchConfig, Input: recorder.input, ModelOutput: recorder.output, Answer: answer, Usage: usage, Recovery: recovery, AnswerModel: recorder.capabilities}, nil
}

func bindSearchActionHost(h *harness, endpoint string, networkLimit uint32, queries *tasks.ActionPort) (*fetchActionHost, error) {
	search, err := jsonsearch.New(h.http, endpoint)
	if err != nil {
		return nil, err
	}
	return bindSearchProvider(h, search, networkLimit, queries)
}

// searchProviderConfig is chosen by the host before descriptor admission. The frozen
// reference keeps its original 1024-byte ceiling; external hosts select theirs.
type searchProviderConfig struct {
	MaxResults uint32 `json:",omitempty"`
	TimeoutMS  uint32 `json:",omitempty"`
	MaxBytes   uint32
	// Empty retains the local reference route. An external host supplies the
	// actual query processing/disclosure location; this does not grant access.
	Recipient string
}

// bindSearchProvider keeps provider selection outside the governed SDK execution
// assembly. The supplied Searcher must use the host's authorized transport.
func bindSearchProvider(h *harness, search websearch.Searcher, networkLimit uint32, queries *tasks.ActionPort) (*fetchActionHost, error) {
	return bindSearchProviderConfig(h, search, networkLimit, queries, searchProviderConfig{MaxBytes: 1024})
}

func bindSearchProviderConfig(h *harness, search websearch.Searcher, networkLimit uint32, queries *tasks.ActionPort, bounds searchProviderConfig) (*fetchActionHost, error) {
	if bounds.MaxResults == 0 {
		bounds.MaxResults = 4
	}
	if bounds.MaxResults > 4 {
		return nil, fetch.Invalid
	}
	if bounds.TimeoutMS == 0 {
		bounds.TimeoutMS = 1000
	}
	if bounds.TimeoutMS > 3000 {
		return nil, fetch.Invalid
	}
	if bounds.MaxBytes < 1 || bounds.MaxBytes > 1<<20 {
		return nil, fetch.Invalid
	}
	if bounds.Recipient == "" {
		bounds.Recipient = "local"
	}
	if !utf8.ValidString(bounds.Recipient) || len(bounds.Recipient) > 256 || strings.TrimSpace(bounds.Recipient) != bounds.Recipient {
		return nil, fetch.Invalid
	}
	copy := *h
	copy.cap = h.cap
	copy.cap.Name = "web.search"
	copy.cap.Input = schema.Resource{Type: "search.input", ID: "urn:search:input", Version: "1", Document: []byte(fmt.Sprintf(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:search:input","type":"object","additionalProperties":false,"required":["query","max_results","max_bytes","max_requests","timeout_ms"],"properties":{"query":{"type":"string","maxLength":2048},"max_results":{"type":"integer","minimum":1,"maximum":%d},"max_bytes":{"type":"integer","minimum":1,"maximum":%d},"max_requests":{"type":"integer","minimum":1,"maximum":1},"timeout_ms":{"type":"integer","minimum":1,"maximum":%d}}}`, bounds.MaxResults, bounds.MaxBytes, bounds.TimeoutMS))}
	// Bind routing to the signed descriptor without changing request arguments.
	// This annotation is not permission: the guard below checks current sources.
	// Preserve the existing local descriptor bytes for reference compatibility.
	if bounds.Recipient != "local" || h.answerFromSearch {
		annotationText := "Query processing/disclosure recipient: " + bounds.Recipient
		if h.answerFromSearch {
			annotationText += "; response mode: provider-summary"
		}
		annotation, _ := json.Marshal(annotationText)
		doc := copy.cap.Input.Document
		copy.cap.Input.Document = []byte(fmt.Sprintf(`%s,"$comment":%s}`, doc[:len(doc)-1], annotation))
	}
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		return nil, err
	}
	privacy, err := searchprivacy.New(h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock, copy.cap, bounds.Recipient)
	if err != nil {
		return nil, err
	}
	if queries != nil {
		privacy, err = privacy.WithQueries(queries)
		if err != nil {
			return nil, err
		}
	}
	var observations searchexecution.ObservationScope
	if queries != nil {
		observations, err = acquisitionQueries(h, queries, copy.cap)
		if err != nil {
			return nil, err
		}
	}
	driverConfig := searchexecution.Config{Observations: observations, Guard: guard, QueryGuard: privacy, Token: h.token, Namespace: "local", Subject: "operator", Capability: copy.cap, MaxResults: int(bounds.MaxResults), MaxBytes: int64(bounds.MaxBytes), MaxRequests: 1, TaskLimit: networkLimit, Timeout: time.Duration(bounds.TimeoutMS) * time.Millisecond}
	driver, err := searchexecution.New(search, h.attempts, h.evidence, h.auth, driverConfig)
	if err != nil {
		return nil, err
	}
	var content execution.Content = h.access
	if queries != nil {
		content, err = h.access.WithQueries(queries)
		if err != nil {
			return nil, err
		}
	}
	copy.exec, err = execution.New(h.grants, h.work, content, driver, h.binding, copy.cap, config(), h.operation)
	if err != nil {
		return nil, err
	}
	copy.client = sdk.NewCapabilityClient(executionlocal.Bind(copy.exec, "local"), "local")
	return &fetchActionHost{h: &copy, recoveryDriver: func(scope *fetchqueries.Scope, guard *fetchtask.Guard, queries *tasks.ActionPort) (execution.Driver, error) {
		cfg := driverConfig
		cfg.Observations = scope
		cfg.Guard = guard
		currentPrivacy, err := privacy.WithQueries(queries)
		if err != nil {
			return nil, err
		}
		cfg.QueryGuard = currentPrivacy
		return searchexecution.New(search, h.attempts, h.evidence, h.auth, cfg)
	}}, nil
}

type researchActionHost struct {
	*fetchActionHost
	search         *fetchActionHost
	task           tasks.Ref
	outcomes       map[string]fetch.Outcome
	discoveryEmpty map[string]bool
}

func (a *researchActionHost) Search(ctx context.Context, t tasks.Task, location string) (catalog.Page, error) {
	if err := a.validateIdentity(ctx, t, location); err != nil {
		return catalog.Page{}, err
	}
	// This profile has a fixed research capability intent. The user question
	// remains the task goal and search input; it is not a capability name.
	return a.directory.Search(ctx, catalog.Query{Text: "Acquire bounded evidence", Purpose: "task", Location: location, Limit: 2, Budget: 2})
}
func (a *researchActionHost) Prepare(ctx context.Context, t tasks.Task, entry catalog.Entry, p brain.ProposedAction) (tasks.Action, error) {
	if entry.Ref.Digest == a.search.h.cap.Digest() {
		return a.search.Prepare(ctx, t, entry, p)
	}
	return a.fetchActionHost.Prepare(ctx, t, entry, p)
}
func (a *researchActionHost) Execute(ctx context.Context, t tasks.Task, x tasks.Action) error {
	if x.Descriptor == a.search.h.cap.Digest() {
		return a.search.Execute(ctx, t, x)
	}
	if x.Descriptor != a.h.cap.Digest() {
		return fetch.Denied
	}
	return a.fetchActionHost.Execute(ctx, t, x)
}
func (a *researchActionHost) Recover(ctx context.Context, t tasks.Task, x tasks.Action) error {
	if x.Descriptor == a.search.h.cap.Digest() {
		return a.search.Recover(ctx, t, x)
	}
	if x.Descriptor != a.h.cap.Digest() {
		return fetch.Denied
	}
	return a.fetchActionHost.Recover(ctx, t, x)
}
func (a *researchActionHost) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	input, err := a.fetchActionHost.Assemble(ctx, t, location, limit)
	if err != nil {
		return brain.Input{}, err
	}
	current, err := a.h.core.Load(ctx, t.Ref)
	if err != nil {
		return brain.Input{}, err
	}
	refs := []string{}
	for _, action := range current.Actions.Actions {
		if action.Descriptor != a.search.h.cap.Digest() {
			continue
		}
		outcome, known, err := a.actionOutcome(ctx, tasks.QualificationOf(current), action.OperationID)
		if err != nil {
			return brain.Input{}, err
		}
		if known && outcome.Status == "acquired" {
			refs = append(refs, outcome.Reference)
		}
	}
	if len(refs) > 0 {
		evidence, err := meteredResearchEvidence(a.h, a.queries, current)
		if err != nil {
			return brain.Input{}, err
		}
		reader, err := a.h.searchEvidence(evidence)
		if err != nil {
			return brain.Input{}, err
		}
		projection, err := searchcontext.New(reader, t, location, refs)
		if err != nil {
			return brain.Input{}, err
		}
		discovered, err := projection.Assemble(ctx, t, location, limit)
		if err != nil {
			return brain.Input{}, err
		}
		input.Blocks = append(input.Blocks, discovered.Blocks...)
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > limit {
		return brain.Input{}, fetch.TooLarge
	}
	return input, nil
}

// AssessSnapshot performs no I/O and does not grant content access. It only
// selects whether to continue action planning or ask Core for answer assembly.
func (a *researchActionHost) AssessSnapshot(r tasks.RunSnapshot) (brain.Assessment, bool) {
	assessment, _, complete := a.snapshotAssessment(r)
	return assessment, complete
}

func (a *researchActionHost) snapshotAssessment(r tasks.RunSnapshot) (brain.Assessment, string, bool) {
	if r.Task.Ref != a.task || r.Actions == nil {
		return brain.Assessment{}, "", false
	}
	for _, action := range r.Actions.Actions {
		if action.Status == "READY" || action.Status == "DISPATCHED" {
			return brain.Assessment{}, "", true
		}
	}
	searched, page, searchRef := false, false, ""
	for _, action := range r.Actions.Actions {
		report := r.ExecutionReports[action.OperationID]
		if report.Effect != "CONFIRMED" && report.Effect != "NOT_OCCURRED" {
			continue
		}
		out, known := a.outcomes[action.OperationID]
		if !known {
			return brain.Assessment{}, "", false
		}
		if action.Descriptor == a.search.h.cap.Digest() && out.Status == "acquired" {
			searched, searchRef = true, out.Reference
		}
		if action.Descriptor == a.search.h.cap.Digest() && fetch.IsFailureStatus(out.Status) && report.Result == "FAILURE" {
			// A confirmed discovery failure is evidence for an answer, not a reason
			// to create a new search operation or consume a correction allowance.
			return brain.Assessment{ReadyForAnswer: true}, "", true
		}
		if action.Descriptor == a.h.cap.Digest() && (out.Status == "acquired" || fetch.IsFailureStatus(out.Status) && report.Result == "FAILURE") {
			page = true
		}
	}
	if searched && (page || a.h.answerFromSearch) {
		return brain.Assessment{ReadyForAnswer: true}, "", true
	}
	if !searched {
		return brain.Assessment{}, "", true
	}
	if empty, known := a.discoveryEmpty[searchRef]; known {
		return brain.Assessment{ReadyForAnswer: empty}, "", true
	}
	return brain.Assessment{}, searchRef, false
}

func (a *researchActionHost) Assess(ctx context.Context, r tasks.RunSnapshot) (brain.Assessment, error) {
	return a.AssessMetered(ctx, r)
}

func (a *researchActionHost) AssessMetered(ctx context.Context, r tasks.RunSnapshot) (brain.Assessment, error) {
	if assessment, ok := a.AssessSnapshot(r); ok {
		return assessment, nil
	}
	if r.Task.Ref != a.task || r.Actions == nil {
		return brain.Assessment{}, fetch.Denied
	}
	for _, action := range r.Actions.Actions {
		report := r.ExecutionReports[action.OperationID]
		if report.Effect != "CONFIRMED" && report.Effect != "NOT_OCCURRED" {
			continue
		}
		if _, _, err := a.actionOutcome(ctx, tasks.QualificationOf(r), action.OperationID); err != nil {
			return brain.Assessment{}, err
		}
	}
	assessment, searchRef, complete := a.snapshotAssessment(r)
	if complete || searchRef == "" {
		return assessment, nil
	}
	// A new observation is necessary to distinguish empty discovery. Both this
	// actual Content reads remain in the original quota, without a second
	// charge for the enclosing phase calculation.
	evidence, err := meteredResearchEvidence(a.h, a.queries, r)
	if err != nil {
		return brain.Assessment{}, err
	}
	reader, err := a.h.searchEvidence(evidence)
	if err != nil {
		return brain.Assessment{}, err
	}
	discovered, err := reader.Read(ctx, searchRef)
	if err != nil {
		return brain.Assessment{}, err
	}
	empty := len(discovered.Candidates) == 0
	// Cache only this immutable discovery fact for phase selection. The next
	// model context still acquires the source through its current Content gate.
	a.discoveryEmpty[searchRef] = empty
	return brain.Assessment{ReadyForAnswer: empty}, nil
}

// The outcome ledger is data observed to choose subsequent work, not Core's
// own qualification/accounting lookup. Charge each host read before storage I/O.
func readResearchOutcome(ctx context.Context, h *harness, queries *tasks.ActionPort, q tasks.Qualification, operation string) (fetch.Outcome, bool, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fetch.Outcome{}, false, err
	}
	if err := queries.ChargeQuery(ctx, q, fmt.Sprintf("outcome/%x", id)); err != nil {
		return fetch.Outcome{}, false, err
	}
	return h.attempts.Outcome(ctx, q.Ref.Namespace, operation)
}

// This host instance serves one serial task, including its answer phase. Known acquisition
// outcomes are immutable operation facts; never cache unknown reads, Content
// bytes, authorization decisions, or data across a process restart.
func (a *researchActionHost) actionOutcome(ctx context.Context, q tasks.Qualification, operation string) (fetch.Outcome, bool, error) {
	if q.Ref != a.task {
		return fetch.Outcome{}, false, fetch.Denied
	}
	if out, ok := a.outcomes[operation]; ok {
		return out, true, nil
	}
	out, known, err := readResearchOutcome(ctx, a.h, a.queries, q, operation)
	if err == nil && known {
		a.outcomes[operation] = out
	}
	return out, known, err
}
