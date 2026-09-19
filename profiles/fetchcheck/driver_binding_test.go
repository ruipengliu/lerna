package fetchcheck

import (
	"context"
	"encoding/json"
	acquisitionexecution "lerna/adapters/research/execution"
	fetchtask "lerna/adapters/research/taskguard"
	"lerna/execution"
	"lerna/fetch"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDriverLimitsAndIdentityWithActualCoreQualification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	url := server.URL + "/start"
	h, err := fresh(ctx, []string{url, server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		t.Fatal(err)
	}
	config := acquisitionexecution.Config{Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 64, MaxRequests: 2, TaskLimit: 2, Timeout: time.Second}
	adapter, ledger, evidence := h.http, h.attempts, h.evidence
	driver, err := acquisitionexecution.NewPage(adapter, ledger, evidence, h.auth, config)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"url": url, "max_bytes": 64, "max_requests": 2, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	call := execution.Call{Request: request, Input: body}
	for _, raw := range [][]byte{
		[]byte(`{"url":"ignored","max_bytes":65,"max_requests":2,"timeout_ms":1000}`),
		[]byte(`{"url":"ignored","max_bytes":64,"max_requests":3,"timeout_ms":1000}`),
		[]byte(`{"url":"ignored","max_bytes":64,"max_requests":2,"timeout_ms":1001}`),
		[]byte(`{"url":"ignored","max_bytes":64,"max_requests":2,"timeout_ms":1000,"grant":"all"}`),
		[]byte(`{"url":"ignored","url":"replacement","max_bytes":64,"max_requests":2,"timeout_ms":1000}`),
	} {
		invalid := call
		invalid.Input = raw
		if e := driver.Start(ctx, invalid); e != fetch.Invalid {
			t.Fatalf("invalid input crossed driver boundary: %v", e)
		}
		if _, e := ledger.Inspect(ctx, "local", request.OperationID); e != fetch.Missing {
			t.Fatal("invalid input reserved acquisition")
		}
	}
	if err = driver.Start(ctx, call); err != nil {
		t.Fatal(err)
	}
	saved, err := ledger.Inspect(ctx, "local", request.OperationID)
	if err != nil || saved.EvidenceOperation == "" || saved.Fingerprint != call.Request.Fingerprint() {
		t.Fatal("driver failed to bind original operation")
	}
	// Inspect is reconstructed without input, as Execution does after restart.
	call.Input = nil
	driver, err = acquisitionexecution.NewPage(adapter, ledger, evidence, h.auth, config)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := driver.Inspect(ctx, call)
	if err != nil || observation.Phase != "FINISHED" || observation.Result != "SUCCESS" || observation.Effect != "CONFIRMED" {
		t.Fatalf("driver reconciliation: %+v %v", observation, err)
	}
	var output struct {
		Status    string `json:"status"`
		Reference string `json:"reference"`
	}
	if json.Unmarshal(observation.Output, &output) != nil || output.Status != "acquired" || output.Reference == "" {
		t.Fatal("driver missing governed reference")
	}
	actual, err := evidence.Read(ctx, output.Reference)
	if err != nil || string(actual.Body) != "hello" || actual.Requests != 2 {
		t.Fatal("driver output is not actual evidence")
	}
	if err = driver.Start(ctx, call); err != nil {
		t.Fatalf("original repeat: %v", err)
	}
	call.Request.InputRef = "replacement-input"
	if _, err = driver.Inspect(ctx, call); err != fetch.IdentityConflict {
		t.Fatalf("driver accepted replacement input: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatal("driver replay dispatched a replacement acquisition")
	}
}
