package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/executionlocal"
	"lerna/execution"
	"lerna/sdk"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Observe the request at the real Content boundary while delegating all
// content behavior to the existing governed adapter.
type observedRequestContent struct {
	execution.Content
	mu              sync.Mutex
	reads, saves    []execution.Request
	outputOperation string
}

func (o *observedRequestContent) ReadFor(ctx context.Context, token string, request execution.Request, capability execution.Capability) ([]byte, error) {
	o.mu.Lock()
	o.reads = append(o.reads, request)
	o.mu.Unlock()
	return o.Content.Read(ctx, token, request.InputRef, capability)
}
func (o *observedRequestContent) SaveFor(ctx context.Context, token string, request execution.Request, operation string, capability execution.Capability, output, evidence []byte) (string, error) {
	o.mu.Lock()
	o.saves = append(o.saves, request)
	o.outputOperation = operation
	o.mu.Unlock()
	return o.Content.Save(ctx, token, operation, request.InputRef, capability, output, evidence)
}
func TestSDKExecutionContentRetainsOriginalRequestQualification(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		name := "same_process"
		if reopen {
			name = "reopened_stores"
		}
		t.Run(name, func(t *testing.T) { checkExecutionContentRequest(t, reopen) })
	}
}

func checkExecutionContentRequest(t *testing.T, reopen bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Actual evidence."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	observed := &observedRequestContent{Content: h.access}
	h.exec, err = execution.New(h.grants, h.work, observed, h.target, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	input, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	if reopen {
		root, token, urls := h.root, h.token, h.urls
		h.close()
		h, err = open(ctx, root, token, urls)
		if err != nil {
			t.Fatal(err)
		}
		defer h.close()
		observed.Content = h.access
		h.exec, err = execution.New(h.grants, h.work, observed, h.target, h.binding, h.cap, config(), h.operation)
		if err != nil {
			t.Fatal(err)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	}
	for i := 0; i < 2; i++ {
		if _, err = h.client.Invoke(ctx, request, grant); err != nil {
			t.Fatal(err)
		}
		if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	record, err := h.exec.GetInvocation(ctx, request.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	observed.mu.Lock()
	defer observed.mu.Unlock()
	if len(observed.reads) != 2 || len(observed.saves) != 1 {
		t.Fatalf("request qualification did not reach content, or replay reread input: reads=%d saves=%d", len(observed.reads), len(observed.saves))
	}
	for _, actual := range append(observed.reads, observed.saves...) {
		if !reflect.DeepEqual(actual, request) {
			t.Fatal("content scope changed original invocation request")
		}
	}
	if observed.outputOperation != record.OutputOperation || record.Reference == "" || record.Result != "SUCCESS" {
		t.Fatal("request-aware save lost original output identity or governed result")
	}
}
