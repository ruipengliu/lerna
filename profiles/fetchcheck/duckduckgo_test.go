package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/duckduckgo"
	"lerna/adapters/researchcontext"
	"lerna/fetch"
	"lerna/websearch"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDuckDuckGoRetainsActualHTMLAndRecoversWithoutSearch(t *testing.T) {
	ctx := context.Background()
	const body = `<html><body><div class="result results_links"><h2><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Go <b>documentation</b></a></h2><a class="result__snippet">Official &amp; current.</a></div><div class="result results_links"><a class="result__a" href="https://go.dev/learn/">Learn Go</a><a class="result__snippet">Tutorials.</a></div></body></html>`
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/html/" || r.URL.Query().Get("q") != "Go docs" || r.Method != "GET" {
			t.Error("incorrect search request")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(body))
	}))
	defer server.Close()
	target := server.URL + "/html/?q=Go+docs"
	h, err := fresh(ctx, []string{target, server.URL + "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	search, err := duckduckgo.New(h.http, server.URL+"/html/")
	if err != nil {
		t.Fatal(err)
	}
	request := websearch.Request{Query: "Go docs", MaxResults: 1, MaxBytes: 8192, MaxRequests: 1, Timeout: time.Second}
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	call, _, err := h.request(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	acquirer, err := websearch.NewAcquirer(search, h.attempts, h.evidence)
	if err != nil {
		t.Fatal(err)
	}
	intent := fetch.AttemptIntent{Task: call.Qualification.Ref, Subject: "operator", OperationID: call.OperationID, Fingerprint: call.Fingerprint(), MaxRequests: 1, TaskLimit: 1, EvidenceOperation: op}
	out, known, err := acquirer.Acquire(ctx, intent, request)
	if err != nil || !known || out.Status != "acquired" {
		t.Fatalf("search acquisition: %+v %v", out, err)
	}
	reader, err := duckduckgo.NewEvidenceReader(h.evidence, request.MaxResults)
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := reader.Read(ctx, out.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered.Candidates) != 1 || discovered.Candidates[0].URL != "https://go.dev/doc/" || discovered.Candidates[0].Title != "Go documentation" || discovered.Candidates[0].Snippet != "Official & current." || string(discovered.Acquisition.Body) != body || discovered.Acquisition.FetchedAt.IsZero() || discovered.Acquisition.Requests != 1 {
		t.Fatal("search rewrote acquisition facts or lost bounded candidates")
	}
	recovered, known, err := acquirer.Recover(ctx, intent)
	if err != nil || !known || recovered != out || hits.Load() != 1 {
		t.Fatal("recovery issued a new search")
	}
	if err = h.policy.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Read(ctx, out.Reference); err == nil {
		t.Fatal("revoked search response remained readable")
	}
}

func TestDuckDuckGoDistinguishesNoResultsFromBlockedPages(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		empty      bool
	}{
		{"empty", `<div class="result no-results__container"><div class="no-results__message">No results found</div></div>`, 200, true},
		{"challenge", `<form id="challenge-form"><div class="anomaly-modal">Verify</div></form>`, 200, false},
		{"http_challenge", `<form id="challenge-form">Verify</form>`, 202, false},
		{"unrecognized", `<html><body>Service maintenance</body></html>`, 200, false},
		{"unsafe_link", `<div class="result"><a class="result__a" href="javascript:alert(1)">Unsafe</a></div>`, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(test.status)
				w.Write([]byte(test.body))
			}))
			defer server.Close()
			h, err := fresh(context.Background(), []string{server.URL + "/html/?q=test", server.URL + "/unused"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			search, err := duckduckgo.New(h.http, server.URL+"/html/")
			if err != nil {
				t.Fatal(err)
			}
			out, err := search.Search(context.Background(), websearch.Request{Query: "test", MaxResults: 2, MaxBytes: 8192, MaxRequests: 1, Timeout: time.Second})
			if test.empty {
				if err != nil || len(out.Candidates) != 0 || len(out.Acquisition.Body) == 0 {
					t.Fatalf("explicit empty search: %v", err)
				}
			} else if err == nil || len(out.Candidates) != 0 || len(out.Acquisition.Body) != 0 {
				t.Fatal("blocked or invalid page became a search answer")
			}
			if hits.Load() != 1 || out.Acquisition.Requests != 1 {
				t.Fatal("failure hid dispatch or retried")
			}
		})
	}
}

func TestSDKDuckDuckGoPublishesDiscoveryWithoutRepeatedDispatch(t *testing.T) {
	checkSDKDuckDuckGoBudget(t, 1024, "")
}

func TestSDKDuckDuckGoUsesExplicitResponseBudget(t *testing.T) {
	checkSDKDuckDuckGoBudget(t, 4096, strings.Repeat(" ", 2048))
}

func checkSDKDuckDuckGoBudget(t *testing.T, maxBytes uint32, padding string) {
	ctx := context.Background()
	var hits atomic.Int32
	body := `<div class="result"><a class="result__a" href="https://go.dev/doc/">Go documentation</a><a class="result__snippet">Discovery only</a></div>` + padding
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(body))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/html/?q=docs", server.URL + "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	provider, err := duckduckgo.New(h.http, server.URL+"/html/")
	if err != nil {
		t.Fatal(err)
	}
	host, err := bindSearchProviderConfig(h, provider, 1, nil, searchProviderConfig{MaxBytes: maxBytes})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"query": "docs", "max_results": 1, "max_bytes": maxBytes, "max_requests": 1, "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	call, grant, err := host.h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = host.h.client.Invoke(ctx, call, grant); err != nil {
			t.Fatal(err)
		}
		if _, err = host.h.exec.Run(ctx, call.OperationID); err != nil {
			t.Fatal(err)
		}
		if err = host.h.exec.Drain(ctx, 16); err != nil {
			t.Fatal(err)
		}
	}
	result, err := host.h.client.Reconcile(ctx, call.OperationID)
	if err != nil || result.Result != "SUCCESS" || result.Effect != "CONFIRMED" {
		t.Fatalf("SDK result: %+v %v", result, err)
	}
	outcome, known, err := h.attempts.Outcome(ctx, "local", call.OperationID)
	if err != nil || !known || outcome.Status != "acquired" {
		t.Fatalf("outcome: %+v %v", outcome, err)
	}
	reader, err := duckduckgo.NewEvidenceReader(h.evidence, 1)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := reader.Read(ctx, outcome.Reference)
	if err != nil || len(discovery.Candidates) != 1 || discovery.Candidates[0].URL != "https://go.dev/doc/" || string(discovery.Acquisition.Body) != body {
		t.Fatalf("discovery: %+v %v", discovery, err)
	}
	run, err := h.core.Load(ctx, call.Qualification.Ref)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := researchcontext.NewSearch(reader, run.Task, "local", []string{outcome.Reference})
	if err != nil {
		t.Fatal(err)
	}
	input, err := projection.Assemble(ctx, run.Task, "local", 8192)
	if err != nil || len(input.Blocks) != 1 || input.Blocks[0].Role != "search-candidates" || !strings.Contains(input.Blocks[0].Text, "https://go.dev/doc/") || strings.Contains(input.Blocks[0].Text, "<div") {
		t.Fatalf("candidate context: %+v %v", input, err)
	}
	budget, err := h.attempts.Budget(ctx, call.Qualification.Ref)
	if err != nil || budget.Charged != 1 || hits.Load() != 1 {
		t.Fatalf("duplicate dispatch or budget: %+v %d %v", budget, hits.Load(), err)
	}
	// The configured descriptor must reject an oversized request before dispatch.
	raw, err = json.Marshal(map[string]any{"query": "docs", "max_results": 1, "max_bytes": maxBytes + 1, "max_requests": 1, "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	oversized, material, err := host.h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = host.h.client.Invoke(ctx, oversized, material); err == nil {
		t.Fatal("request exceeded configured search byte limit")
	}
	if hits.Load() != 1 {
		t.Fatal("oversized request reached HTTP")
	}
}

func TestDuckDuckGoEmptyDiscoveryDoesNotFabricatePageEvidence(t *testing.T) {
	record, err := runResearchTaskFormat(context.Background(), "empty_search", nil, false, false, nil, 128, "duckduckgo-html")
	if err != nil {
		t.Fatal(err)
	}
	if record.Answer.Status != "insufficient" || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != 0 || record.Usage.NetworkCharged != 1 {
		t.Fatalf("empty search became page acquisition: %+v", record)
	}
	for _, block := range record.Input.Blocks {
		if block.Role != "search-candidates" {
			t.Fatalf("unexpected page evidence: %s", block.Role)
		}
	}
}
