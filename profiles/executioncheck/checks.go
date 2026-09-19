package executioncheck

import (
	"context"
	"fmt"
	executionlocal "lerna/adapters/execution/local"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"sync"
	"time"
)

var names = []string{"sdk-sync-and-replay", "schema-rejected", "version-rejected", "changed-operation", "wrong-receiver", "concurrent-start", "one-action-per-generation", "resource-precondition", "pause-before-start", "cancel-before-start", "update-before-start", "expired-worker", "grant-revoked", "source-revoked", "return-lost", "effect-unconfirmed", "invalid-output", "unknown-keeps-resource", "late-effect-after-update", "outbox-replay", "query-privacy"}

type wrappedDriver struct {
	execution.Driver
	start   func(context.Context, execution.Call) error
	inspect func(context.Context, execution.Call) (execution.Observation, error)
}

func (d wrappedDriver) Start(ctx context.Context, c execution.Call) error {
	if d.start != nil {
		return d.start(ctx, c)
	}
	return d.Driver.Start(ctx, c)
}
func (d wrappedDriver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	if d.inspect != nil {
		return d.inspect(ctx, c)
	}
	return d.Driver.Inspect(ctx, c)
}
func (h *harness) replace(d execution.Driver, core execution.Core) error {
	s, e := execution.New(h.grants, core, h.access, d, h.binding, h.cap, config(), h.operation)
	if e == nil {
		h.exec = s
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(s, "local"), "local")
	}
	return e
}
func require(ok bool, why string) error {
	if !ok {
		return fmt.Errorf("%s", why)
	}
	return nil
}
func (h *harness) control(ctx context.Context, r execution.Request, intent string) error {
	c, e := h.core.Controls(controlLimits())
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	current, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil {
		return e
	}
	_, e = c.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: current.Ref, ExpectedVersion: current.Version, Intent: intent})
	return e
}

type inputCheck struct{}

func (inputCheck) ValidateInput(context.Context, string, tasks.Task, []string, *tasks.AnswerConstraint) error {
	return nil
}
func (h *harness) update(ctx context.Context, r execution.Request) error {
	u, e := h.core.Updates(inputCheck{}, tasks.UpdateLimits{MaxOperations: 32, MaxInteractions: 16, MaxRefs: 16, IOTimeout: time.Second, Control: controlLimits()})
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	current, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil {
		return e
	}
	_, e = u.SubmitUpdate(ctx, h.token, tasks.UpdateRequest{OperationID: op, Ref: current.Ref, ExpectedVersion: current.Version, Intent: "revise", GoalRef: r.InputRef})
	return e
}
func check(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	r, material, e := h.request(ctx)
	if e != nil {
		return e
	}
	if name == "schema-rejected" {
		r.InputRef, e = h.put(ctx, []byte(`{"delta":99}`))
		if e != nil {
			return e
		}
		_, e = h.client.Invoke(ctx, r, material)
		t, read := h.target.Snapshot(ctx)
		return require(e != nil && read == nil && t.Changes == 0, "schema rejection")
	}
	if name == "version-rejected" {
		r.Version = "unknown"
		_, e = h.client.Invoke(ctx, r, material)
		return require(authorization.Is(e, authorization.Invalid), "version rejection")
	}
	if name == "wrong-receiver" {
		h.binding.Audience = "another-receiver"
		if e = h.replace(h.target, h.work); e != nil {
			return e
		}
		_, e = h.client.Invoke(ctx, r, material)
		return require(e != nil, "receiver accepted")
	}
	if name == "resource-precondition" {
		r.ResourceVersion = 2
		material, e = h.issue(ctx, r)
		if e != nil {
			return e
		}
	}
	if name == "return-lost" {
		e = h.replace(wrappedDriver{Driver: h.target, start: func(c context.Context, call execution.Call) error {
			if e := h.target.Start(c, call); e != nil {
				return e
			}
			return fmt.Errorf("lost driver reply")
		}}, h.work)
		if e != nil {
			return e
		}
	}
	if name == "effect-unconfirmed" || name == "unknown-keeps-resource" {
		e = h.replace(wrappedDriver{Driver: h.target, inspect: func(context.Context, execution.Call) (execution.Observation, error) {
			return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
		}}, h.work)
		if e != nil {
			return e
		}
	}
	if name == "invalid-output" {
		e = h.replace(wrappedDriver{Driver: h.target, inspect: func(c context.Context, call execution.Call) (execution.Observation, error) {
			v, e := h.target.Inspect(c, call)
			v.Output = []byte(`{"unexpected":true}`)
			return v, e
		}}, h.work)
		if e != nil {
			return e
		}
	}
	if name == "late-effect-after-update" {
		e = h.replace(wrappedDriver{Driver: h.target, start: func(c context.Context, call execution.Call) error {
			if e := h.update(c, r); e != nil {
				return e
			}
			return h.target.Start(c, call)
		}}, h.work)
		if e != nil {
			return e
		}
	}
	receipt, e := h.client.Invoke(ctx, r, material)
	if e != nil {
		return e
	}
	switch name {
	case "changed-operation":
		r.ResourceVersion++
		_, e = h.client.Invoke(ctx, r, material)
		return require(authorization.Is(e, authorization.IdentityConflict), "changed operation accepted")
	case "pause-before-start", "cancel-before-start", "update-before-start", "expired-worker", "grant-revoked", "source-revoked":
		switch name {
		case "pause-before-start":
			e = h.control(ctx, r, "PAUSE")
		case "cancel-before-start":
			e = h.control(ctx, r, "CANCEL")
		case "update-before-start":
			e = h.update(ctx, r)
		case "expired-worker":
			h.clock.advance(11 * time.Second)
		case "source-revoked":
			h.policy.Replace(nil)
		case "grant-revoked":
			record, err := h.exec.GetInvocation(ctx, r.OperationID)
			if err != nil {
				return err
			}
			snap, err := h.db.Load(ctx)
			if err != nil {
				return err
			}
			op, err := h.operation(ctx)
			if err != nil {
				return err
			}
			_, e = h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", ExpectedRevision: snap.State.Revision, GrantId: record.Permit.GrantID, ExpectedGrantRevision: snap.State.Signed.Grants[record.Permit.GrantID].Record.Revision})
		}
		if e != nil {
			return e
		}
		_, e = h.exec.Run(ctx, r.OperationID)
		target, read := h.target.Snapshot(ctx)
		return require(e != nil && read == nil && target.Changes == 0, "inadmissible start")
	case "concurrent-start":
		var wg sync.WaitGroup
		errors := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := h.exec.Run(ctx, r.OperationID); errors <- err }()
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			if err != nil && !authorization.Is(err, authorization.Unavailable) && !authorization.Is(err, authorization.Conflict) {
				return err
			}
		}
		target, e := h.target.Snapshot(ctx)
		return require(e == nil && target.Changes == 1, "concurrent effect")
	case "one-action-per-generation":
		other := r
		other.OperationID, e = h.operation(ctx)
		if e != nil {
			return e
		}
		otherMaterial, err := h.issue(ctx, other)
		if err != nil {
			return err
		}
		if _, e = h.client.Invoke(ctx, other, otherMaterial); e != nil {
			return e
		}
		if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
			return e
		}
		_, e = h.exec.Run(ctx, other.OperationID)
		target, read := h.target.Snapshot(ctx)
		return require(e != nil && read == nil && target.Changes == 1, "generation spent twice")
	case "query-privacy":
		h.binding.Token = "invalid"
		if e = h.replace(h.target, h.work); e != nil {
			return e
		}
		_, e = h.client.GetInvocation(ctx, r.OperationID)
		_, missing := h.client.GetInvocation(ctx, "absent")
		return require(authorization.Is(e, authorization.Unauthenticated) && authorization.Is(missing, authorization.Unauthenticated), "query leaks")
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if e != nil {
		return e
	}
	if name == "unknown-keeps-resource" {
		next, m, err := h.request(ctx)
		if err != nil {
			return err
		}
		if _, e = h.client.Invoke(ctx, next, m); e != nil {
			return e
		}
		_, e = h.exec.Run(ctx, next.OperationID)
		target, read := h.target.Snapshot(ctx)
		return require(e != nil && read == nil && target.Changes == 1 && out.Effect == "UNKNOWN", "unknown resource reused")
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	current, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil {
		return e
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil {
		return e
	}
	switch name {
	case "resource-precondition":
		return require(out.Effect == "NOT_OCCURRED" && target.Changes == 0 && current.State != "COMPLETED", "precondition effect")
	case "effect-unconfirmed":
		return require(out.Effect == "UNKNOWN" && target.Changes == 1 && current.State == "WAITING", "return equated effect")
	case "invalid-output":
		return require(out.Effect == "CONFIRMED" && out.Result == "FAILURE" && target.Changes == 1 && current.State != "COMPLETED", "invalid result completed task")
	case "late-effect-after-update":
		return require(out.Effect == "CONFIRMED" && target.Changes == 1 && current.State != "COMPLETED", "old effect completed new goal")
	case "outbox-replay":
		saved, e := h.exec.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		v := current.Version
		for range 2 {
			if e = h.work.ConsumeExecution(ctx, saved.Reports[0]); e != nil {
				return e
			}
			if e = h.exec.Drain(ctx, 16); e != nil {
				return e
			}
		}
		current, e = h.core.Get(ctx, h.token, r.Qualification.Ref)
		return require(e == nil && current.Version == v, "report applied twice")
	default:
		replay, e := h.client.Invoke(ctx, r, "")
		if e != nil {
			return e
		}
		if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
			return e
		}
		target, e = h.target.Snapshot(ctx)
		return require(e == nil && replay == receipt && out.Effect == "CONFIRMED" && current.State == "COMPLETED" && target.Value == 3 && target.Changes == 1, "sync/replay")
	}
}
