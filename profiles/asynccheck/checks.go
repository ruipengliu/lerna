package asynccheck

import (
	"context"
	"fmt"
	"lerna/adapters/executionlocal"
	"lerna/authorization"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"time"
)

var caseNames = []string{"task-cancel", "cancel-before-start", "uncorrelated-lost-handle", "correlated-lost-handle", "duplicate-progress", "conflicting-evidence", "conflict-after-task-completed", "forged-progress", "poll-budget", "cancel-completion-race", "lost-cancel-reply", "two-coordinators", "unsupported-cancel", "expired-key", "negative-coverage"}

type driverWrap struct {
	execution.AsyncDriver
	cancel  execution.CancelDriver
	start   func(context.Context, execution.Call) (execution.Fact, error)
	inspect func(context.Context, execution.Call, execution.Association) (execution.Fact, error)
	stop    func(context.Context, execution.Call, execution.Association, string) error
}

func (d driverWrap) StartAsync(c context.Context, in execution.Call) (execution.Fact, error) {
	if d.start != nil {
		return d.start(c, in)
	}
	return d.AsyncDriver.StartAsync(c, in)
}
func (d driverWrap) InspectAsync(c context.Context, in execution.Call, a execution.Association) (execution.Fact, error) {
	if d.inspect != nil {
		return d.inspect(c, in, a)
	}
	return d.AsyncDriver.InspectAsync(c, in, a)
}
func (d driverWrap) Cancel(c context.Context, in execution.Call, a execution.Association, op string) error {
	if d.stop != nil {
		return d.stop(c, in, a, op)
	}
	return d.cancel.Cancel(c, in, a, op)
}
func (h *harness) replace(d execution.Driver) error {
	var e error
	h.exec, e = execution.New(h.grants, h.work, h.access, d, h.binding, h.cap, config(), h.operation)
	if e == nil {
		h.client = newClient(h.exec)
	}
	return e
}
func require(ok bool, msg string) error {
	if !ok {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func check(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	if name == "unsupported-cancel" {
		h.cap.Async.Cancel = false
		if e = h.replace(h.target); e != nil {
			return e
		}
	}
	if name == "uncorrelated-lost-handle" || name == "correlated-lost-handle" || name == "expired-key" || name == "negative-coverage" {
		if name == "uncorrelated-lost-handle" {
			h.cap.Async.Correlation = false
		}
		d := driverWrap{AsyncDriver: h.target, cancel: h.target, start: func(c context.Context, in execution.Call) (execution.Fact, error) {
			if name == "negative-coverage" {
				return execution.Fact{}, &authorization.Error{Code: authorization.OutcomeUnknown}
			}
			_, e := h.target.StartAsync(c, in)
			if e != nil {
				return execution.Fact{}, e
			}
			return execution.Fact{}, &authorization.Error{Code: authorization.OutcomeUnknown}
		}}
		if name == "uncorrelated-lost-handle" {
			d.inspect = func(_ context.Context, c execution.Call, _ execution.Association) (execution.Fact, error) {
				return execution.Fact{Source: "job-driver", Version: 1, Association: execution.Association{Target: "counter-jobs", OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint()}, Observation: execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}}, nil
			}
		}
		if e = h.replace(d); e != nil {
			return e
		}
	}
	r, m, e := h.request(ctx)
	if e != nil {
		return e
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return e
	}
	if name == "cancel-before-start" {
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		if _, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: op, Invocation: r.OperationID}); e != nil {
			return e
		}
		if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
			return e
		}
		if _, e = h.exec.RunCancel(ctx, op); e != nil {
			return e
		}
		s, e := h.target.Snapshot(ctx)
		return require(e == nil && s.Jobs == 0, "cancelled invocation started")
	}
	_, e = h.exec.Run(ctx, r.OperationID)
	if e != nil && name != "correlated-lost-handle" && name != "uncorrelated-lost-handle" && name != "expired-key" && name != "negative-coverage" {
		return e
	}
	switch name {
	case "expired-key", "negative-coverage":
		if name == "expired-key" {
			if e = h.target.Complete(ctx, r.OperationID); e != nil {
				return e
			}
			if e = h.target.ExpireLookup(ctx, r.OperationID); e != nil {
				return e
			}
		} else {
			if e = h.target.Fence(ctx, execution.Call{Request: r}); e != nil {
				return e
			}
		}
		h.clock.advance(time.Second)
		out, e := h.client.Reconcile(ctx, r.OperationID)
		if e != nil {
			return e
		}
		if name == "expired-key" {
			return require(out.Effect == "UNKNOWN", "expired key treated as absence")
		}
		if out.Effect != "NOT_OCCURRED" {
			return fmt.Errorf("sufficient negative proof ignored")
		}
		if _, e = h.target.StartAsync(ctx, execution.Call{Request: r, Input: []byte(`{"delta":3}`)}); e != nil {
			return e
		}
		snapshot, e := h.target.Snapshot(ctx)
		return require(e == nil && snapshot.Changes == 0, "late original start escaped fence")
	case "correlated-lost-handle", "uncorrelated-lost-handle":
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		out, e := h.client.Reconcile(ctx, r.OperationID)
		if e != nil {
			return e
		}
		expected := "CONFIRMED"
		if name == "uncorrelated-lost-handle" {
			expected = "UNKNOWN"
		}
		if out.Effect != expected {
			return fmt.Errorf("effect %s", out.Effect)
		}
		_, _ = h.exec.Run(ctx, r.OperationID)
		target, e := h.target.Snapshot(ctx)
		return require(e == nil && target.Jobs == 1 && target.Changes == 1, "lost response caused another start")
	case "task-cancel":
		controls, e := h.core.Controls(controlLimits())
		if e != nil {
			return e
		}
		task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
		if e != nil {
			return e
		}
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		_, e = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: r.Qualification.Ref, ExpectedVersion: task.Version, Intent: "CANCEL"})
		if e != nil {
			return e
		}
		h.clock.advance(time.Second)
		if e = h.exec.Recover(ctx, 16); e != nil {
			return e
		}
		task, e = h.core.Get(ctx, h.token, r.Qualification.Ref)
		return require(e == nil && task.State == "CANCELLED", "task cancel not settled")
	case "duplicate-progress", "forged-progress", "conflicting-evidence", "conflict-after-task-completed":
		port, e := h.exec.BindProgress("job-driver")
		if e != nil {
			return e
		}
		fact, e := h.target.InspectAsync(ctx, execution.Call{Request: r}, execution.Association{})
		if e != nil {
			return e
		}
		before, e := h.client.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		if name == "forged-progress" {
			fact.Source = "untrusted"
			e = port.Accept(ctx, r.OperationID, fact)
			return require(authorization.Is(e, authorization.Denied), "forged source accepted")
		}
		if name == "duplicate-progress" {
			for range 20 {
				if e = port.Accept(ctx, r.OperationID, fact); e != nil {
					return e
				}
			}
			after, e := h.client.GetInvocation(ctx, r.OperationID)
			return require(e == nil && before.Revision == after.Revision, "duplicate progress consumed capacity")
		}
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		fact, e = h.target.InspectAsync(ctx, execution.Call{Request: r}, execution.Association{})
		if e != nil {
			return e
		}
		if e = port.Accept(ctx, r.OperationID, fact); e != nil {
			return e
		}
		if name == "conflict-after-task-completed" {
			if e = h.exec.Drain(ctx, 16); e != nil {
				return e
			}
		}
		fact.Version++
		fact.Observation.Phase = "NOT_STARTED"
		fact.Observation.Effect = "NOT_OCCURRED"
		fact.Observation.Result = "FAILURE"
		fact.Observation.Evidence = []byte(`{"source":"contradictory target evidence"}`)
		if e = port.Accept(ctx, r.OperationID, fact); e != nil {
			return e
		}
		if e = h.exec.Drain(ctx, 16); e != nil {
			return e
		}
		out, e := h.client.GetInvocation(ctx, r.OperationID)
		if e != nil || !out.ConflictingEvidence {
			return fmt.Errorf("conflict missing: %v", e)
		}
		task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
		if e != nil {
			return e
		}
		if name == "conflict-after-task-completed" {
			return require(task.State == "COMPLETED", "terminal task reopened")
		}
		return require(task.State != "COMPLETED", "conflicting evidence completed task")
	case "poll-budget":
		for range config().MaxChecks {
			h.clock.advance(time.Second)
			if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
				return e
			}
		}
		h.clock.advance(time.Second)
		_, e = h.client.Reconcile(ctx, r.OperationID)
		return require(authorization.Is(e, authorization.Unavailable), "poll budget reset")
	case "cancel-completion-race", "lost-cancel-reply", "unsupported-cancel":
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		if _, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: op, Invocation: r.OperationID}); e != nil {
			return e
		}
		if name == "cancel-completion-race" {
			if e = h.target.Complete(ctx, r.OperationID); e != nil {
				return e
			}
		}
		if name == "lost-cancel-reply" {
			if e = h.replace(driverWrap{AsyncDriver: h.target, cancel: h.target, stop: func(c context.Context, in execution.Call, a execution.Association, op string) error {
				if e := h.target.Cancel(c, in, a, op); e != nil {
					return e
				}
				return &authorization.Error{Code: authorization.OutcomeUnknown}
			}}); e != nil {
				return e
			}
		}
		if _, e = h.exec.RunCancel(ctx, op); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
			return e
		}
		cancel, e := h.client.GetCancel(ctx, op)
		if e != nil {
			return e
		}
		want := "STOPPED"
		if name == "cancel-completion-race" {
			want = "NOT_PREVENTED"
		}
		if name == "unsupported-cancel" {
			want = "UNSUPPORTED"
		}
		return require(cancel.Progress == want, "wrong cancellation fact")
	case "two-coordinators":
		other, e := open(ctx, h.root, h.token)
		if e != nil {
			return e
		}
		defer other.close()
		h.clock.advance(time.Second)
		other.clock.advance(time.Second)
		done := make(chan error, 2)
		for _, svc := range []*execution.Service{h.exec, other.exec} {
			go func() { _, e := svc.Reconcile(ctx, r.OperationID); done <- e }()
		}
		for range 2 {
			if e = <-done; e != nil && !authorization.Is(e, authorization.Conflict) {
				return e
			}
		}
		out, e := h.client.GetInvocation(ctx, r.OperationID)
		return require(e == nil && out.Checks == 1, "duplicate recovery claim")
	}
	return fmt.Errorf("unknown case")
}

func newClient(s *execution.Service) *sdk.CapabilityClient {
	return sdk.NewCapabilityClient(executionlocal.Bind(s, "local"), "local")
}
