package fetchcheck

import (
	"context"
	"lerna/adapters/executionlocal"
	"lerna/execution"
	"lerna/fetch"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A transport observation can fail after the actual acquisition committed.
// Withhold one response; all work and later observations delegate unchanged.
type lostFirstObservation struct {
	execution.Driver
	lost bool
}

func (d *lostFirstObservation) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	if !d.lost {
		d.lost = true
		return execution.Observation{}, fetch.Unavailable
	}
	return d.Driver.Inspect(ctx, c)
}
func TestUnknownAcquisitionRecoversUnderOriginalTaskBudget(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Actual evidence."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	run, _ := prepareActionAnswerProcess(t, ctx, h, func(port *tasks.ActionPort) {
		content, e := h.access.WithQueries(port)
		if e != nil {
			t.Fatal(e)
		}
		scope, e := acquisitionQueries(h, port, h.cap)
		if e != nil {
			t.Fatal(e)
		}
		h.target, e = h.target.WithObservations(scope)
		if e != nil {
			t.Fatal(e)
		}
		h.exec, e = execution.New(h.grants, h.work, content, &lostFirstObservation{Driver: h.target}, h.binding, h.cap, config(), h.operation)
		if e != nil {
			t.Fatal(e)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	})
	budget, err := h.attempts.Budget(ctx, run.Task.Ref)
	if err != nil || budget.Charged != 1 || hits.Load() != 1 {
		t.Fatalf("recovery duplicated acquisition: %+v hits=%d err=%v", budget, hits.Load(), err)
	}
}
