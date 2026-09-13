package fetchcheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/fetchtask"
	"lerna/adapters/jsonsearch"
	"lerna/adapters/searchexecution"
	"lerna/adapters/searchprivacy"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchRecoversAcrossActualProcessExit(t *testing.T) {
	for _, mode := range []string{"reserved", "response", "content", "outcome"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"results":[{"url":"https://example.com/history","title":"History","snippet":"A discovery hint"}]}`))
			}))
			defer server.Close()
			urls := []string{server.URL + "/start?q=history", server.URL + "/final"}
			root := t.TempDir()
			h, err := open(ctx, root, "", urls)
			if err != nil {
				t.Fatal(err)
			}
			host, err := bindSearchActionHost(h, server.URL+"/start", 1, nil)
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			h = host.h
			body, _ := json.Marshal(map[string]any{"query": "history", "max_results": 2, "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
			request, grant, err := h.request(ctx, body)
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			if _, err = h.client.Invoke(ctx, request, grant); err != nil {
				h.close()
				t.Fatal(err)
			}
			saved := processConfig{Token: h.token, Mode: mode, URLs: urls, Request: request}
			raw, _ := json.Marshal(saved)
			if err = os.WriteFile(filepath.Join(root, "probe.json"), raw, 0600); err != nil {
				h.close()
				t.Fatal(err)
			}
			h.close()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSearchCrashProbe$")
			child.Env = append(os.Environ(), "LERNA_SEARCH_PROCESS_ROOT="+root)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 75 {
				t.Fatalf("child exit: %v %s", err, output)
			}
			h, err = open(ctx, root, saved.Token, urls)
			if err != nil {
				t.Fatal(err)
			}
			host, err = bindSearchActionHost(h, server.URL+"/start", 1, nil)
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			h = host.h
			defer h.close()
			intent, err := h.attempts.Inspect(ctx, "local", request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if intent.EvidenceOperation == "" || intent.Fingerprint != request.Fingerprint() {
				t.Fatal("lost original acquisition identity")
			}
			fact, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
			if err != nil || known != (mode == "outcome") {
				t.Fatal("unexpected committed result before recovery")
			}
			var original fetch.Result
			var originalRef string
			recoverable := mode == "content" || mode == "outcome"
			if recoverable {
				originalRef, err = h.evidence.Lookup(ctx, intent.EvidenceOperation)
				if err != nil {
					t.Fatal(err)
				}
				original, err = h.evidence.Read(ctx, originalRef)
				if err != nil || string(original.Body) != `{"results":[{"url":"https://example.com/history","title":"History","snippet":"A discovery hint"}]}` {
					t.Fatal("lost saved response")
				}
			}
			wantHits := int32(1)
			if mode == "reserved" {
				wantHits = 0
			}
			if hits.Load() != wantHits {
				t.Fatal("unexpected actual pre-recovery requests")
			}
			originalInvocation, err := h.exec.GetInvocation(ctx, request.OperationID)
			if err != nil || originalInvocation.OutputOperation == "" {
				t.Fatal("missing original Execution output identity")
			}
			result, err := h.client.Reconcile(ctx, request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			currentInvocation, e := h.exec.GetInvocation(ctx, request.OperationID)
			if e != nil || currentInvocation.OutputOperation != originalInvocation.OutputOperation {
				t.Fatal("recovery replaced Execution output identity")
			}
			if recoverable {
				if result.Result != "SUCCESS" || result.Effect != "CONFIRMED" {
					t.Fatalf("saved acquisition not recovered: %s %s", result.Result, result.Effect)
				}
				restored, err := h.evidence.Read(ctx, originalRef)
				if err != nil || !reflect.DeepEqual(restored, original) {
					t.Fatal("recovery changed original response or time")
				}
				reader, e := jsonsearch.NewEvidenceReader(h.evidence)
				if e != nil {
					t.Fatal(e)
				}
				discovery, e := reader.Read(ctx, originalRef)
				if e != nil || len(discovery.Candidates) != 1 || discovery.Candidates[0].URL != "https://example.com/history" || discovery.Candidates[0].Title != "History" || discovery.Candidates[0].Snippet != "A discovery hint" || !reflect.DeepEqual(discovery.Acquisition, original) {
					t.Fatal("recovered search candidates lost their original response provenance")
				}
				fact, known, err = h.attempts.Outcome(ctx, "local", request.OperationID)
				if err != nil || !known || fact.Status != "acquired" || fact.Requests != 1 || fact.Reference != originalRef {
					t.Fatal("original content not bound to recovered outcome")
				}
			} else if result.Result != "UNKNOWN" || result.Effect != "UNKNOWN" || result.Reference != "" {
				t.Fatal("unsaved response fabricated a known result")
			}
			if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
				t.Fatal(err)
			}
			if err = h.exec.Drain(ctx, 16); err != nil {
				t.Fatal(err)
			}
			task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "WAITING"
			if recoverable {
				wantState = "COMPLETED"
			}
			if task.State != wantState {
				t.Fatalf("recovered Core state=%s want=%s", task.State, wantState)
			}
			if hits.Load() != wantHits {
				t.Fatal("recovery dispatched replacement HTTP requests")
			}
			budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
			if err != nil || budget.Charged != 1 {
				t.Fatal("process exit reset request reservation")
			}
			t.Logf("window=%s child_exit=75 http=%d result=%s core_state=%s charged=%d source_recovered=%t", mode, hits.Load(), result.Result, task.State, budget.Charged, recoverable)
		})
	}
}

func TestSearchCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_SEARCH_PROCESS_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	raw, err := os.ReadFile(filepath.Join(root, "probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved processConfig
	if json.Unmarshal(raw, &saved) != nil || len(saved.URLs) != 2 {
		t.Fatal("invalid search probe configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := open(ctx, root, saved.Token, saved.URLs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	endpoint, _, found := strings.Cut(saved.URLs[0], "?")
	if !found {
		t.Fatal("missing configured search endpoint")
	}
	host, err := bindSearchActionHost(h, endpoint, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	h = host.h
	search, err := jsonsearch.New(crashFetcher{h.http, saved.Mode}, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		t.Fatal(err)
	}
	privacy, err := searchprivacy.New(h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock, h.cap, "local")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := searchexecution.New(search, crashOutcome{h.attempts, saved.Mode}, h.evidence, h.auth, searchexecution.Config{Guard: guard, QueryGuard: privacy, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxResults: 4, MaxBytes: 1024, MaxRequests: 1, TaskLimit: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.exec.Run(ctx, saved.Request.OperationID)
	t.Fatalf("search crash window not reached: result=%s error=%v", result.Result, err)
}
