package fetchcheck

import (
	"context"
	"fmt"
	"lerna/adapters/acquisitionexecution"
	"lerna/adapters/executionlocal"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type exposeAcquisitionRequest struct {
	execution.Driver
	requests chan execution.Request
}

func (d exposeAcquisitionRequest) Start(ctx context.Context, c execution.Call) error {
	d.requests <- c.Request
	return d.Driver.Start(ctx, c)
}

type controlAfterAcquisition struct {
	execution.Driver
	control func(execution.Request) error
}

func (d controlAfterAcquisition) Start(ctx context.Context, c execution.Call) error {
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	return d.control(c.Request)
}

func TestControlAfterAcquisitionBeforeInspectionConverges(t *testing.T) {
	for _, intent := range []string{"CANCEL", "PAUSE"} {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reset=%v", intent, reset), func(t *testing.T) { checkControlledActionFact(t, intent, reset, true) })
		}
	}
}
func TestCancelledActionPublishesFiniteAcquisitionFact(t *testing.T) {
	for _, intent := range []string{"CANCEL", "PAUSE"} {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reset=%v", intent, reset), func(t *testing.T) { checkControlledActionFact(t, intent, reset) })
		}
	}
}
func checkControlledActionFact(t *testing.T, intent string, reset bool, completed ...bool) {

	ctx := context.Background()
	requests := make(chan execution.Request, 1)
	var h *harness
	var hits atomic.Int32
	var queryPort *tasks.ActionPort
	afterComplete := len(completed) != 0 && completed[0]
	requestControl := func(original execution.Request) error {
		current, err := h.core.Get(ctx, h.token, original.Qualification.Ref)
		if err != nil {
			return err
		}
		controls, err := h.core.Controls(controlLimits())
		if err != nil {
			return err
		}
		op, err := h.operation(ctx)
		if err != nil {
			return err
		}
		if _, err = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: current.Ref, ExpectedVersion: current.Version, Intent: intent}); err != nil {
			return err
		}
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		original := <-requests
		if !afterComplete {
			if err := requestControl(original); err != nil {
				t.Error(err)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("must not be disclosed"))
	}))
	defer server.Close()
	var err error
	h, err = fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	run, _ := runActionProcess(t, ctx, h, func(port *tasks.ActionPort) {
		queryPort = port
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
		var driver execution.Driver = h.target
		if reset {
			driver = &resetObservationDriver{driver: h.target, reset: func() (*acquisitionexecution.PageDriver, error) {
				next, err := acquisitionQueries(h, port, h.cap)
				if err != nil {
					return nil, err
				}
				return h.target.WithObservations(next)
			}}
		}
		driver = exposeAcquisitionRequest{driver, requests}
		if afterComplete {
			driver = controlAfterAcquisition{driver, requestControl}
		}
		h.exec, e = execution.New(h.grants, h.work, content, driver, h.binding, h.cap, config(), h.operation)
		if e != nil {
			t.Fatal(e)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	})
	if run.Actions == nil || len(run.Actions.Actions) != 1 {
		t.Fatal("missing original action")
	}
	op := run.Actions.Actions[0].OperationID
	fact, known, err := h.attempts.Outcome(ctx, "local", op)
	if err != nil || !known || (fact.Reference != "") != afterComplete || afterComplete && fact.Status != "acquired" || fact.Requests != 1 {
		t.Fatalf("missing finite acquisition fact: %+v %v", fact, err)
	}
	result, err := h.exec.GetInvocation(ctx, op)
	if err != nil || result.Result != "FAILURE" || result.Effect != "CONFIRMED" || result.Reference != "" {
		t.Fatalf("cancelled finite fact did not converge: result=%s effect=%s task=%s err=%v", result.Result, result.Effect, run.Task.State, err)
	}
	want := "CANCELLED"
	if intent == "PAUSE" {
		want = "WAITING"
	}
	if run.Task.State != want || run.Task.Control.Intent != intent || run.Task.Control.Progress != "APPLIED" {
		t.Fatalf("control did not settle: %+v", run.Task.Control)
	}

	budget, err := h.attempts.Budget(ctx, run.Task.Ref)
	if err != nil || budget.Charged != 1 {
		t.Fatal("cancellation reset acquisition budget")
	}
	facts := 0
	for _, key := range run.Actions.Queries {
		if strings.HasPrefix(key, "outcome-fact/") {
			facts++
		}
	}
	if facts != 1 {
		t.Fatalf("finite fact observation not charged: %d", facts)
	}
	if hits.Load() != 1 {
		t.Fatalf("control recovery repeated HTTP: %d", hits.Load())
	}
	if afterComplete {
		scope, err := acquisitionQueries(h, queryPort, h.cap)
		if err != nil {
			t.Fatal(err)
		}
		_, content, err := scope.Bind(result.Request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := content.Read(ctx, fact.Reference); err == nil {
			t.Fatal("controlled acquisition disclosed original content")
		}
	}

}
