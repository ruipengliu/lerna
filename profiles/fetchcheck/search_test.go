package fetchcheck

import (
	"context"
	"encoding/json"
	researchcontext "lerna/adapters/research/context"
	"lerna/adapters/research/jsonsearch"
	"lerna/fetch"
	"lerna/websearch"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestJSONSearchDiscoversCandidatesThroughAuthorizedHTTP(t *testing.T) {
	var pageReads atomic.Int32
	var searchReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/page" {
			pageReads.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("The bridge opened in 1998."))
			return
		}
		if r.URL.Path != "/search" {
			pageReads.Add(1)
			w.WriteHeader(404)
			return
		}
		searchReads.Add(1)
		results := []map[string]string{}
		if r.URL.Query().Get("q") == "bridge history" {
			results = append(results, map[string]string{"url": "https://example.com/bridge", "title": "Bridge history", "snippet": "Index hint; read the actual page."})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()
	target := server.URL + "/search?q=" + url.QueryEscape("bridge history")
	h, err := fresh(context.Background(), []string{target, server.URL + "/page"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	search, err := jsonsearch.New(h.http, server.URL+"/search")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	result, err := search.Search(context.Background(), websearch.Request{Query: "bridge history", MaxResults: 2, MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].URL != "https://example.com/bridge" || result.Acquisition.RequestedURL != target || result.Acquisition.Requests != 1 || result.Acquisition.FetchedAt.Before(before) || len(result.Acquisition.Body) == 0 {
		t.Fatal("search lost actual discovery evidence")
	}
	if pageReads.Load() != 0 {
		t.Fatal("search silently fetched a page")
	}

	ctx := context.Background()
	queryInput, _ := json.Marshal(websearch.Request{Query: "bridge history", MaxResults: 2, MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	call, _, err := h.request(ctx, queryInput)
	if err != nil {
		t.Fatal(err)
	}
	evidenceOp, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := websearch.NewAcquirer(search, h.attempts, h.evidence)
	if err != nil {
		t.Fatal(err)
	}
	intent := fetch.AttemptIntent{Task: call.Qualification.Ref, Subject: "operator", OperationID: call.OperationID, Fingerprint: call.Fingerprint(), MaxRequests: 1, TaskLimit: 2, EvidenceOperation: evidenceOp}
	original, known, err := coordinator.Acquire(ctx, intent, websearch.Request{Query: "bridge history", MaxResults: 2, MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	if err != nil || !known || original.Status != "acquired" || original.Reference == "" {
		t.Fatalf("durable discovery: %v", err)
	}
	retained, err := h.evidence.Read(ctx, original.Reference)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate orchestration while retaining real SQLite and Content identities.
	coordinator, err = websearch.NewAcquirer(search, h.attempts, h.evidence)
	if err != nil {
		t.Fatal(err)
	}
	recovered, known, err := coordinator.Recover(ctx, intent)
	if err != nil || !known || recovered != original {
		t.Fatalf("lost original discovery: %v", err)
	}
	replay, known, err := coordinator.Acquire(ctx, intent, websearch.Request{Query: "bridge history", MaxResults: 2, MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	if err != nil || !known || replay != original || searchReads.Load() != 2 {
		t.Fatal("replayed search dispatched again")
	}
	again, err := h.evidence.Read(ctx, replay.Reference)
	if err != nil || again.FetchedAt != retained.FetchedAt || string(again.Body) != string(retained.Body) {
		t.Fatal("search replaced original evidence")
	}
	budget, err := h.attempts.Budget(ctx, intent.Task)
	if err != nil || budget.Charged != 1 {
		t.Fatal("search reset original budget")
	}
	task, err := h.core.Get(ctx, h.token, intent.Task)
	if err != nil {
		t.Fatal(err)
	}
	discoveryReader, err := jsonsearch.NewEvidenceReader(h.evidence)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := researchcontext.NewSearch(discoveryReader, task, "local", []string{original.Reference})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projection.Assemble(ctx, task, "local", 32768)
	if err != nil || len(projected.Blocks) != 1 || projected.Blocks[0].Role != "search-candidates" {
		t.Fatalf("search projected as page evidence: %v", err)
	}
	var discovery struct {
		Status     string
		Candidates []websearch.Candidate
	}
	if json.Unmarshal([]byte(projected.Blocks[0].Text), &discovery) != nil || discovery.Status != "discovered" || len(discovery.Candidates) != 1 || discovery.Candidates[0].URL != "https://example.com/bridge" {
		t.Fatal("candidate projection lost discovery")
	}
	if _, err = projection.Assemble(ctx, task, "local", 8); err == nil {
		t.Fatal("unbounded search context")
	}
	page, err := h.http.Fetch(ctx, fetch.Request{URL: server.URL + "/page", MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	pageOp, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pageRef, err := h.evidence.Save(ctx, pageOp, page)
	if err != nil {
		t.Fatal(err)
	}
	combined, err := researchcontext.New(h.evidence, discoveryReader, nil, task, "local", researchcontext.References{Search: []string{original.Reference}, Pages: []string{pageRef}})
	if err != nil {
		t.Fatal(err)
	}
	all, err := combined.Assemble(ctx, task, "local", 32768)
	if err != nil || len(all.Blocks) != 2 || all.Blocks[0].Role != "search-candidates" || all.Blocks[1].Role != "external-evidence" {
		t.Fatalf("lost evidence roles: %v", err)
	}
	if _, err = combined.Assemble(ctx, task, "local", 100); err == nil {
		t.Fatal("combined context exceeded common budget")
	}
	if _, err = researchcontext.New(h.evidence, discoveryReader, nil, task, "local", researchcontext.References{Search: []string{original.Reference}, Pages: []string{original.Reference}}); err == nil {
		t.Fatal("same response treated as search and page")
	}
	if err = h.policy.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if err = projection.Validate(ctx, task, "local"); err == nil {
		t.Fatal("revoked search response remained usable")
	}
	if err = combined.Validate(ctx, task, "local"); err == nil {
		t.Fatal("combined context ignored revocation")
	}
	// Query changes cannot expand the host's exact authorized request set.
	denied, err := search.Search(context.Background(), websearch.Request{Query: "private query", MaxResults: 2, MaxBytes: 1024, MaxRequests: 1, Timeout: time.Second})
	if err == nil || len(denied.Candidates) != 0 || len(denied.Acquisition.Body) != 0 {
		t.Fatal("unauthorized query disclosed")
	}
}
