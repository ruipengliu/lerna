package fetchcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoreDeadlineStopsAcquisitionBeforeLeaseAndHTTPTimeout(t *testing.T) {
	for _, mode := range []string{"redirect", "waiting"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var h *harness
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path == "/start" {
					// Expire the three-second task deadline while its ten-second lease and
					// one-second real HTTP timer remain live. No wall-clock sleep is used.
					h.clock.advance(4 * time.Second)
					if mode == "redirect" {
						http.Redirect(w, r, "/final", http.StatusFound)
						return
					}
					select {
					case <-r.Context().Done():
					case <-time.After(3 * time.Second):
						t.Error("deadline did not interrupt HTTP")
					}
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("must not acquire"))
			}))
			defer server.Close()
			var err error
			h, err = fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			h.taskDeadline = 3 * time.Second
			raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
			request, grant, err := h.request(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.client.Invoke(ctx, request, grant); err != nil {
				t.Fatal(err)
			}
			result, err := h.exec.Run(ctx, request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			actual, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
			if err != nil || !known || actual.Status != "timed_out" || actual.Requests != 1 || actual.Reference != "" || result.Result == "SUCCESS" || hits.Load() != 1 {
				t.Fatalf("deadline facts: %+v known=%v requests=%d result=%s err=%v", actual, known, hits.Load(), result.Result, err)
			}
			budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
			if err != nil || budget.Charged != 2 {
				t.Fatal("task deadline refunded original reservation")
			}
		})
	}
}
