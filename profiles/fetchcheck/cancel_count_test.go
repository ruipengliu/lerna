package fetchcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCallerCancellationRetainsObservedRequestCount(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	received := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(received); cancel(); <-r.Context().Done() }))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	body, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	// Run may return before its cancelled driver has recorded the failure.
	_, _ = h.exec.Run(call, request.OperationID)
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("server never received acquisition")
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		result, known, e := h.attempts.Outcome(ctx, "local", request.OperationID)
		if e != nil {
			t.Fatal(e)
		}
		if known {
			if result.Status != "cancelled" || result.Requests != 1 || result.Reference != "" {
				t.Fatalf("incorrect cancellation facts: %+v", result)
			}
			budget, e := h.attempts.Budget(ctx, request.Qualification.Ref)
			if e != nil || budget.Charged != 1 {
				t.Fatal("cancelled request refunded")
			}
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("known dispatched request lost after cancellation")
		}
	}
}
