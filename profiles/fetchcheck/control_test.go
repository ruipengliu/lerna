package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskControlStopsNextAcquisitionHop(t *testing.T) {
	for _, intent := range []string{"PAUSE", "CANCEL"} {
		t.Run(intent, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var h *harness
			var ref tasks.Ref
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path == "/start" {
					current, err := h.core.Get(ctx, h.token, ref)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					control, err := h.core.Controls(controlLimits())
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					op, err := h.operation(ctx)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					if _, err = control.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: ref, ExpectedVersion: current.Version, Intent: intent}); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					http.Redirect(w, r, "/final", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("must not fetch"))
			}))
			defer server.Close()
			var err error
			h, err = fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
			request, grant, err := h.request(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			ref = request.Qualification.Ref
			if _, err = h.client.Invoke(ctx, request, grant); err != nil {
				t.Fatal(err)
			}
			result, err := h.exec.Run(ctx, request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			outcome, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
			if err != nil || !known || outcome.Status != "denied" || outcome.Requests != 1 || outcome.Reference != "" || hits.Load() != 1 || result.Result == "SUCCESS" {
				t.Fatalf("task control allowed another hop: %+v known=%v hits=%d result=%s err=%v", outcome, known, hits.Load(), result.Result, err)
			}
		})
	}
}
