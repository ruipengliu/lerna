package fetchcheck

import (
	"context"
	executionlocal "lerna/adapters/execution/local"
	acquisitionexecution "lerna/adapters/research/execution"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This test delegate reconstructs the real observation scope after Start;
// it does not replace the driver, stored facts or returned observations.
type resetObservationDriver struct {
	driver *acquisitionexecution.PageDriver
	reset  func() (*acquisitionexecution.PageDriver, error)
}

func (d *resetObservationDriver) Start(ctx context.Context, c execution.Call) error {
	if err := d.driver.Start(ctx, c); err != nil {
		return err
	}
	next, err := d.reset()
	if err != nil {
		return err
	}
	d.driver = next
	return nil
}
func (d *resetObservationDriver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	return d.driver.Inspect(ctx, c)
}

func TestSDKAcquisitionObservationsConsumeOriginalTaskQueries(t *testing.T) {
	t.Run("same_scope", func(t *testing.T) { checkAcquisitionQueries(t, false, false) })
	t.Run("fresh_scope", func(t *testing.T) { checkAcquisitionQueries(t, true, false) })
	t.Run("later_scope", func(t *testing.T) { checkAcquisitionQueries(t, false, true) })
}
func checkAcquisitionQueries(t *testing.T, reset, later bool) {
	t.Helper()
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
	if later {
		// Construct the task's observation scope after the original source
		// retention was fixed, without waiting on wall-clock scheduling.
		h.clock.advance(2 * time.Second)
	}
	var qualified execution.RequestContent
	var legacy execution.Content
	run, _ := prepareActionAnswerProcess(t, ctx, h, func(port *tasks.ActionPort) {
		content, err := h.access.WithQueries(port)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := acquisitionQueries(h, port, h.cap)
		if err != nil {
			t.Fatal(err)
		}
		h.target, err = h.target.WithObservations(scope)
		if err != nil {
			t.Fatal(err)
		}
		var driver execution.Driver = h.target
		if reset {
			driver = &resetObservationDriver{driver: h.target, reset: func() (*acquisitionexecution.PageDriver, error) {
				next, err := acquisitionQueries(h, port, h.cap)
				if err != nil {
					return nil, err
				}
				h.target, err = h.target.WithObservations(next)
				return h.target, err
			}}
		}
		qualified, legacy = content, content
		h.exec, err = execution.New(h.grants, h.work, content, driver, h.binding, h.cap, config(), h.operation)
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
	// Four execution observations plus the driver recovery LOOKUP and
	// READ use the original quota. A fresh scope must recover metadata and
	// the durable outcome again, without receiving the previous local facts.
	expectedReads, expectedOutcomes := 6, 0
	if reset {
		expectedReads, expectedOutcomes = 7, 1
	}
	if reads != expectedReads {
		t.Fatalf("execution observations missing from original quota: %d", reads)
	}
	outcomes := 0
	for _, query := range run.Actions.Queries {
		if strings.HasPrefix(query, "outcome/") {
			outcomes++
		}
	}
	if outcomes != expectedOutcomes {
		t.Fatalf("driver Outcome observations: got %d, want %d", outcomes, expectedOutcomes)
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
	if _, err = h.target.Inspect(ctx, execution.Call{Request: record.Request}); err == nil {
		t.Fatal("settled invocation read acquisition facts under stale qualification")
	}
	after, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries) != len(run.Actions.Queries) {
		t.Fatal("unqualified or stale read consumed current worker queries")
	}
}
