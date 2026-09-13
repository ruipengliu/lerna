package fetchcheck

import (
	"context"
	"lerna/adapters/executionlocal"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSDKExecutionContentReadsConsumeOriginalTaskQueries(t *testing.T) {
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
	var qualified execution.RequestContent
	var legacy execution.Content
	run, _ := prepareActionAnswerProcess(t, ctx, h, func(port *tasks.ActionPort) {
		content, err := h.access.WithQueries(port)
		if err != nil {
			t.Fatal(err)
		}
		qualified, legacy = content, content
		h.exec, err = execution.New(h.grants, h.work, content, h.target, h.binding, h.cap, config(), h.operation)
		if err != nil {
			t.Fatal(err)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	})
	reads := 0
	for _, query := range run.Actions.Queries {
		if strings.HasPrefix(query, "content/") {
			reads++
		}
	}
	// The task write supplied the input length. Invoke and Run each perform
	// a fresh READ; saving reads input and acquired-evidence metadata.
	if reads != 4 {
		t.Fatalf("execution observations missing from original quota: %d", reads)
	}
	record, err := h.exec.GetInvocation(ctx, run.Actions.Actions[0].OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Read(ctx, h.token, record.Request.InputRef, h.cap); err == nil {
		t.Fatal("execution query adapter accepted an unqualified read")
	}
	// The action has settled and Core advanced to the answer phase. Its old
	// request must not be silently rebound to the current worker qualification.
	if _, err = qualified.ReadFor(ctx, h.token, record.Request, h.cap); err == nil {
		t.Fatal("stale invocation qualification was silently upgraded")
	}
	after, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries) != len(run.Actions.Queries) {
		t.Fatal("unqualified or stale read consumed current worker queries")
	}
}
