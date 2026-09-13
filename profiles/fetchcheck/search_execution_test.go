package fetchcheck

import (
	"context"
	"lerna/adapters/acquisitionexecution"
	"lerna/adapters/executionlocal"
	"lerna/adapters/fetchtask"
	"lerna/adapters/jsonsearch"
	"lerna/adapters/searchprivacy"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/schema"
	"lerna/sdk"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSDKSearchPersistsOriginalDiscoveryAndChecksRecipient(t *testing.T) {
	for _, recipient := range []string{"local", "remote-search"} {
		t.Run(recipient, func(t *testing.T) {
			ctx := context.Background()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"results":[{"url":"https://example.com/history","title":"History","snippet":"Discovery hint"}]}`))
			}))
			defer server.Close()
			h, err := fresh(ctx, []string{server.URL + "/search?q=history", server.URL + "/other"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			h.cap.Name = "web.search"
			h.cap.Input = schema.Resource{Type: "search.input", ID: "urn:search:input", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:search:input","type":"object","additionalProperties":false,"required":["query","max_results","max_bytes","max_requests","timeout_ms"],"properties":{"query":{"type":"string"},"max_results":{"type":"integer"},"max_bytes":{"type":"integer"},"max_requests":{"type":"integer"},"timeout_ms":{"type":"integer"}}}`)}
			search, err := jsonsearch.New(h.http, server.URL+"/search")
			if err != nil {
				t.Fatal(err)
			}
			taskGuard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
			if err != nil {
				t.Fatal(err)
			}
			privacy, err := searchprivacy.New(h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock, h.cap, recipient)
			if err != nil {
				t.Fatal(err)
			}
			driver, err := acquisitionexecution.NewSearch(search, h.attempts, h.evidence, h.auth, acquisitionexecution.SearchConfig{Config: acquisitionexecution.Config{Guard: taskGuard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 1024, MaxRequests: 1, TaskLimit: 2, Timeout: time.Second}, QueryGuard: privacy, MaxResults: 4})
			if err != nil {
				t.Fatal(err)
			}
			h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
			if err != nil {
				t.Fatal(err)
			}
			client := sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
			request, grant, err := h.request(ctx, []byte(`{"query":"history","max_results":2,"max_bytes":1024,"max_requests":1,"timeout_ms":1000}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Invoke(ctx, request, grant); err != nil {
				t.Fatal(err)
			}
			if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
				t.Fatal(err)
			}
			if err = h.exec.Drain(ctx, 16); err != nil {
				t.Fatal(err)
			}
			result, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
			if err != nil || !known {
				t.Fatalf("missing known search result: %v", err)
			}
			if recipient == "local" {
				if result.Status != "acquired" || result.Reference == "" || requests.Load() != 1 {
					t.Fatal("search failed")
				}
				saved, err := h.evidence.Read(ctx, result.Reference)
				if err != nil || saved.RequestedURL != server.URL+"/search?q=history" {
					t.Fatal("missing search response")
				}
			} else if result.Status != "denied" || result.Requests != 0 || result.Reference != "" || requests.Load() != 0 {
				t.Fatal("private query reached forbidden location")
			}
			before := requests.Load()
			if _, err = client.Invoke(ctx, request, grant); err != nil {
				t.Fatal(err)
			}
			if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != before {
				t.Fatal("search replay dispatched")
			}
			budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
			if err != nil || budget.Charged != 1 {
				t.Fatal("search budget changed")
			}
		})
	}
}
