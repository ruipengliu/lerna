package fetchcheck

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/adapters/fetchauth"
	"lerna/adapters/jsonsearch"
	"lerna/adapters/replayfetch"
	"lerna/adapters/searchcontext"
	"lerna/adapters/searchprivacy"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/catalog"
	"lerna/fetch"
	"lerna/profiles/searchcheck"
	"lerna/schema"
	"lerna/tasks"
	"lerna/websearch"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	return checkPreparedResearchRun(ctx, h, researchRunSpec{
		SearchConfig: searchProviderConfig{MaxBytes: config.SearchMaxBytes}, Goal: goal, Query: query, SearchEndpoint: server.URL + searchPath, Queries: queries, NetworkLimit: networkLimit, Steps: steps,
		AnswerModel: answerModel, ModelTokens: config.ModelTokens,
	}, mode, material, reopen, &researchVerification{actions: expectedActions, decisions: expectedDecisions, searchRequests: expectedSearchRequests, pageRequests: expectedPageRequests, searchHits: &searchHits, pageHits: &hits})
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
	privacy, err := searchprivacy.New(h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock, copy.cap, bounds.Recipient)
	if err != nil {
		return nil, err
	}
	driver := &searchAcquisition{provider: search, privacy: privacy, bounds: bounds, networkLimit: networkLimit}
	if err := driver.bind(&copy, queries); err != nil {
		return nil, err
	}
	return &fetchActionHost{h: &copy, searchDriver: driver}, nil
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
