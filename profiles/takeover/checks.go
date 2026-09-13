package takeover

import (
	"context"
	"fmt"
	"lerna/adapters/executionlocal"
	"lerna/authorization"
	"lerna/execution"
	"lerna/sdk"
	"time"
)

var caseNames = []string{"two-api-mutations", "alias-replay", "cross-type-identity", "admitted-before-takeover", "inflight-fence", "late-start", "resume-observation", "resume-replay", "independent-resource", "unmapped-entry", "unknown-target", "lost-control-reply", "control-budget", "stale-coordinator", "resume-observation-race"}

func require(ok bool, msg string) error {
	if !ok {
		return fmt.Errorf("%s", msg)
	}
	return nil
}

type controlWrap struct {
	execution.ResourceDriver
	observe func(context.Context, execution.ResourceRef) (execution.ResourceObservation, error)
	apply   func(context.Context, execution.ResourceCommand) (execution.ResourceObservation, error)
}

func (w controlWrap) ObserveResource(c context.Context, r execution.ResourceRef) (execution.ResourceObservation, error) {
	if w.observe != nil {
		return w.observe(c, r)
	}
	return w.ResourceDriver.ObserveResource(c, r)
}
func (w controlWrap) ApplyResourceControl(c context.Context, r execution.ResourceCommand) (execution.ResourceObservation, error) {
	if w.apply != nil {
		return w.apply(c, r)
	}
	return w.ResourceDriver.ApplyResourceControl(c, r)
}

type driverWrap struct {
	execution.AsyncDriver
	execution.CancelDriver
	start func(context.Context, execution.Call) (execution.Fact, error)
}

func (w driverWrap) StartAsync(c context.Context, r execution.Call) (execution.Fact, error) {
	if w.start != nil {
		return w.start(c, r)
	}
	return w.AsyncDriver.StartAsync(c, r)
}
func (h *harness) replace(d execution.Driver, control execution.ResourceDriver) error {
	var e error
	h.exec, e = execution.New(h.grants, h.work, h.access, d, h.binding, h.cap, config(), h.operation)
	if e == nil {
		h.exec, e = h.exec.WithResourceControl(scope(), control)
	}
	if e == nil {
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	}
	return e
}
func (h *harness) control(ctx context.Context, intent string) (execution.ResourceControlReceipt, error) {
	v, e := h.client.GetResourceControl(ctx, scope().Ref)
	if e != nil {
		return execution.ResourceControlReceipt{}, e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return execution.ResourceControlReceipt{}, e
	}
	return h.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: intent, ExpectedVersion: v.Version})
}
func (h *harness) invocation(ctx context.Context, control, business uint64, run bool) (execution.Request, error) {
	r, _, e := h.request(ctx)
	if e != nil {
		return r, fmt.Errorf("create request: %w", e)
	}
	r.ControlVersion = control
	r.ResourceVersion = business
	m, e := h.issue(ctx, r)
	if e != nil {
		return r, e
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return r, fmt.Errorf("invoke: %w", e)
	}
	if run {
		_, e = h.exec.Run(ctx, r.OperationID)
		if e != nil {
			return r, fmt.Errorf("run: %w", e)
		}
	}
	return r, e
}
func (h *harness) settle(ctx context.Context) (execution.ResourceControl, error) {
	h.clock.advance(4 * time.Second)
	if e := h.exec.ReconcileResource(ctx, scope().Ref, 16); e != nil {
		return execution.ResourceControl{}, e
	}
	return h.exec.AdvanceResourceControl(ctx, scope().Ref)
}
func check(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	switch name {
	case "two-api-mutations":
		r, e := h.invocation(ctx, 1, 1, true)
		if e != nil {
			return e
		}
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
			return e
		}
		if e = h.exec.Drain(ctx, 16); e != nil {
			return e
		}
		other, e := h.other(ctx)
		if e != nil {
			return e
		}
		defer other.close()
		r, e = other.invocation(ctx, 1, 2, true)
		if e != nil {
			return e
		}
		if e = other.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		o, e := h.target.Snapshot(ctx)
		return require(e == nil && o.Value == 9 && o.Version == 3 && o.Changes == 2, "two entries did not mutate one target")
	case "alias-replay", "resume-replay":
		rc, e := h.control(ctx, "TAKEOVER")
		if e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		if name == "resume-replay" {
			rc, e = h.control(ctx, "RESUME")
			if e != nil {
				return e
			}
			if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
				return e
			}
			if _, e = h.control(ctx, "TAKEOVER"); e != nil {
				return e
			}
		}
		intent := "TAKEOVER"
		if name == "resume-replay" {
			intent = "RESUME"
		}
		in := execution.ResourceControlRequest{OperationID: rc.OperationID, Resource: scope().Aliases[0], Intent: intent, ExpectedVersion: rc.Version - 1}
		replay, e := h.client.RequestResourceControl(ctx, in)
		if e != nil || replay != rc {
			return fmt.Errorf("receipt replay: %v", e)
		}
		lookup, e := h.client.LookupResourceControl(ctx, rc.OperationID)
		if e != nil || lookup != rc {
			return fmt.Errorf("lookup: %v", e)
		}
		in.Intent = "INVALID"
		if _, e = h.client.RequestResourceControl(ctx, in); e == nil {
			return fmt.Errorf("invalid intent accepted")
		}
		in.Intent = intent
		in.ExpectedVersion++
		if _, e = h.client.RequestResourceControl(ctx, in); !authorization.Is(e, authorization.IdentityConflict) {
			return fmt.Errorf("changed identity: %v", e)
		}
		v, e := h.client.GetResourceControl(ctx, scope().Ref)
		return require(e == nil && v.Intent == "TAKEOVER", "old resume reopened control")
	case "cross-type-identity":
		r, e := h.invocation(ctx, 1, 1, false)
		if e != nil {
			return e
		}
		cancel, e := h.operation(ctx)
		if e != nil {
			return e
		}
		if _, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: cancel, Invocation: r.OperationID}); e != nil {
			return e
		}
		other, e := h.other(ctx)
		if e != nil {
			return e
		}
		defer other.close()
		for _, op := range []string{cancel, r.OperationID} {
			_, e = other.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: "TAKEOVER", ExpectedVersion: 1})
			if !authorization.Is(e, authorization.IdentityConflict) {
				return fmt.Errorf("cross-kind: %v", e)
			}
		}
		rc, e := h.control(ctx, "TAKEOVER")
		if e != nil {
			return e
		}
		_, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: rc.OperationID, Invocation: r.OperationID})
		return require(authorization.Is(e, authorization.IdentityConflict), "control identity reused as cancel")
	case "admitted-before-takeover", "inflight-fence", "late-start":
		var delayed execution.Call
		if name == "late-start" {
			if e = h.replace(driverWrap{AsyncDriver: h.target, CancelDriver: h.target, start: func(_ context.Context, c execution.Call) (execution.Fact, error) {
				delayed = c
				return execution.Fact{}, fmt.Errorf("lost before target")
			}}, h.target); e != nil {
				return e
			}
		}
		r, e := h.invocation(ctx, 1, 1, name != "admitted-before-takeover")
		if e != nil && name != "late-start" {
			return e
		}
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		v, e := h.exec.AdvanceResourceControl(ctx, scope().Ref)
		if e != nil {
			return e
		}
		if name != "admitted-before-takeover" && v.Progress != "ACCEPTED" {
			return fmt.Errorf("unknown fact settled prematurely")
		}
		if name == "late-start" {
			if _, e = h.target.StartAsync(ctx, delayed); e == nil {
				return fmt.Errorf("late target start passed fence")
			}
		}
		if _, e = h.exec.Run(ctx, r.OperationID); e != nil && !authorization.Is(e, authorization.Conflict) {
			return e
		}
		v, e = h.settle(ctx)
		if e != nil {
			return e
		}
		out, e := h.client.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		o, e := h.target.Snapshot(ctx)
		return require(e == nil && v.Progress == "APPLIED" && out.Effect == "NOT_OCCURRED" && o.Changes == 0, "takeover did not settle original work")
	case "resume-observation":
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		if e = h.target.UserChange(ctx, 40); e != nil {
			return e
		}
		if _, e = h.control(ctx, "RESUME"); e != nil {
			return e
		}
		v, e := h.exec.AdvanceResourceControl(ctx, scope().Ref)
		if e != nil || v.Progress != "APPLIED" || v.ResourceVersion != 2 {
			return fmt.Errorf("fresh observation: %+v %v", v, e)
		}
		r, e := h.invocation(ctx, 3, 1, true)
		if e != nil {
			return e
		}
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		out, e := h.client.Reconcile(ctx, r.OperationID)
		if e != nil || out.Effect != "NOT_OCCURRED" {
			return fmt.Errorf("stale business version: %v", e)
		}
		r, e = h.invocation(ctx, 3, 2, true)
		if e != nil {
			return e
		}
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		o, e := h.target.Snapshot(ctx)
		return require(e == nil && o.Value == 43 && o.Changes == 1, "user change overwritten")
	case "independent-resource":
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		other, e := openTarget(ctx, h.root, h.token, "add", "independent")
		if e != nil {
			return e
		}
		defer other.close()
		r, e := other.invocation(ctx, 1, 1, true)
		if e != nil {
			return e
		}
		if e = other.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		o, e := other.target.Snapshot(ctx)
		return require(e == nil && o.Changes == 1, "independent resource blocked")
	case "unmapped-entry":
		h.cap.Implementation = "unmapped"
		h.exec, e = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
		if e != nil {
			return e
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
		_, e = h.invocation(ctx, 0, 1, false)
		return require(authorization.Is(e, authorization.Unsupported), "unmapped entry bypassed resource")
	case "unknown-target", "control-budget", "lost-control-reply":
		if e = h.replace(h.target, controlWrap{ResourceDriver: h.target, apply: func(c context.Context, r execution.ResourceCommand) (execution.ResourceObservation, error) {
			if name == "lost-control-reply" {
				if _, e := h.target.ApplyResourceControl(c, r); e != nil {
					return execution.ResourceObservation{}, e
				}
			}
			return execution.ResourceObservation{}, fmt.Errorf("unknown control outcome")
		}}); e != nil {
			return e
		}
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		_, _ = h.exec.AdvanceResourceControl(ctx, scope().Ref)
		count := 1
		if name == "control-budget" {
			count = 10
		}
		for i := 0; i < count; i++ {
			h.clock.advance(4 * time.Second)
			_, e = h.exec.AdvanceResourceControl(ctx, scope().Ref)
		}
		v, err := h.client.GetResourceControl(ctx, scope().Ref)
		if err != nil {
			return err
		}
		if name == "lost-control-reply" {
			return require(v.Progress == "APPLIED", "lost result not recovered by observation")
		}
		if name == "control-budget" && (!authorization.Is(e, authorization.Unavailable) || v.Checks != 8) {
			return fmt.Errorf("budget reset: %+v %v", v, e)
		}
		_, err = h.control(ctx, "RESUME")
		return require(v.Progress == "ACCEPTED" && authorization.Is(err, authorization.Conflict), "unknown target released")
	case "stale-coordinator":
		other, e := h.other(ctx)
		if e != nil {
			return e
		}
		defer other.close()
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if _, e = other.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		op, e := other.operation(ctx)
		if e != nil {
			return e
		}
		_, e = other.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: "RESUME", ExpectedVersion: 1})
		return require(authorization.Is(e, authorization.Conflict), "stale coordinator rewrote control")
	case "resume-observation-race":
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		if _, e = h.control(ctx, "RESUME"); e != nil {
			return e
		}
		if e = h.replace(h.target, controlWrap{ResourceDriver: h.target, apply: func(c context.Context, r execution.ResourceCommand) (execution.ResourceObservation, error) {
			if e := h.target.UserChange(c, 55); e != nil {
				return execution.ResourceObservation{}, e
			}
			return h.target.ApplyResourceControl(c, r)
		}}); e != nil {
			return e
		}
		_, _ = h.exec.AdvanceResourceControl(ctx, scope().Ref)
		v, e := h.client.GetResourceControl(ctx, scope().Ref)
		o, err := h.target.Snapshot(ctx)
		return require(e == nil && err == nil && v.Progress == "ACCEPTED" && o.Intent == "TAKEOVER" && o.Value == 55, "stale resume observation passed")
	}
	return fmt.Errorf("unknown case %s", name)
}
