package takeover

import (
	"context"
	"fmt"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"slices"
	"time"
)

var boundaryNames = []string{"resume-permission", "permission-before-dispatch", "read-permission", "task-pause", "task-cancel", "conflicting-evidence", "fair-recovery", "scope-capacity", "mapping-conflict"}

func (h *harness) deny(ctx context.Context, action string) error {
	snap, e := h.db.Load(ctx)
	if e != nil {
		return e
	}
	rules := snap.State.Rules
	for _, r := range rules {
		r.Scope.Actions = slices.DeleteFunc(r.Scope.Actions, func(a string) bool { return a == action })
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: rules}}}})
	return e
}
func boundary(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	switch name {
	case "resume-permission", "permission-before-dispatch", "read-permission":
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if name == "permission-before-dispatch" {
			if e = h.deny(ctx, "resource.takeover"); e != nil {
				return e
			}
			_, e = h.exec.AdvanceResourceControl(ctx, scope().Ref)
			o, err := h.target.Snapshot(ctx)
			return require(authorization.Is(e, authorization.Denied) && err == nil && o.ControlVersion == 1, "revoked control dispatched")
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		if name == "read-permission" {
			if e = h.deny(ctx, "resource.control.read"); e != nil {
				return e
			}
			_, e = h.client.GetResourceControl(ctx, scope().Ref)
			return require(authorization.Is(e, authorization.Denied), "revoked control read returned state")
		}
		if e = h.deny(ctx, "resource.resume"); e != nil {
			return e
		}
		_, e = h.control(ctx, "RESUME")
		return require(authorization.Is(e, authorization.Denied), "resume lacked independent authorization")
	case "task-pause", "task-cancel":
		r, e := h.invocation(ctx, 1, 1, true)
		if e != nil {
			return e
		}
		before, e := h.exec.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
		if e != nil {
			return e
		}
		c, e := h.core.Controls(controlLimits())
		if e != nil {
			return e
		}
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		intent := "PAUSE"
		if name == "task-cancel" {
			intent = "CANCEL"
		}
		if _, e = c.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: task.Version, Intent: intent}); e != nil {
			return e
		}
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		v, e := h.settle(ctx)
		if e != nil || v.Progress != "APPLIED" {
			return fmt.Errorf("settle %+v %v", v, e)
		}
		if _, e = h.control(ctx, "RESUME"); e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		task, e = h.core.Get(ctx, h.token, r.Qualification.Ref)
		if e != nil {
			return e
		}
		after, e := h.exec.GetInvocation(ctx, r.OperationID)
		if e != nil {
			return e
		}
		if after.Permit != before.Permit || after.Checks < before.Checks {
			return fmt.Errorf("permit or budget reset")
		}
		want := "WAITING"
		if name == "task-cancel" {
			want = "CANCELLED"
		}
		return require(task.State == want && task.Control.Intent == intent, "resource resume cleared task control")
	case "conflicting-evidence":
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
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
			return e
		}
		fact, e := h.target.InspectAsync(ctx, execution.Call{Request: r}, execution.Association{})
		if e != nil {
			return e
		}
		fact.Version++
		fact.Observation = execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Evidence: []byte(`{"proof":"conflicting-source"}`)}
		port, e := h.exec.BindProgress("job-driver")
		if e != nil {
			return e
		}
		if e = port.Accept(ctx, r.OperationID, fact); e != nil {
			return e
		}
		v, e := h.client.GetResourceControl(ctx, scope().Ref)
		if e != nil {
			return e
		}
		_, e = h.control(ctx, "RESUME")
		return require(v.Progress == "ACCEPTED" && v.Blocking == 1 && authorization.Is(e, authorization.Conflict), "late conflict released resource")
	case "fair-recovery":
		if e = h.replace(h.target, controlWrap{ResourceDriver: h.target, apply: func(context.Context, execution.ResourceCommand) (execution.ResourceObservation, error) {
			return execution.ResourceObservation{}, fmt.Errorf("unsupported stop")
		}}); e != nil {
			return e
		}
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		other, e := openTarget(ctx, h.root, h.token, "add", "independent")
		if e != nil {
			return e
		}
		defer other.close()
		op, e := other.operation(ctx)
		if e != nil {
			return e
		}
		if _, e = other.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scopeAt("independent").Ref, Intent: "TAKEOVER", ExpectedVersion: 1}); e != nil {
			return e
		}
		results, e := execution.RecoverResources(ctx, []execution.ResourceParticipant{{Resource: scope().Ref, Recovery: h.exec}, {Resource: scopeAt("independent").Ref, Recovery: other.exec}}, 1)
		return require(e == nil && len(results) == 2 && results[0].Err != nil && results[1].Err == nil && results[1].Control.Progress == "APPLIED", "one unavailable range blocked another")
	case "scope-capacity":
		for range 32 {
			if _, e = h.control(ctx, "TAKEOVER"); e != nil {
				return e
			}
		}
		_, e = h.control(ctx, "TAKEOVER")
		if !authorization.Is(e, authorization.Unavailable) {
			return fmt.Errorf("capacity unbounded: %v", e)
		}
		v, e := h.client.GetResourceControl(ctx, scope().Ref)
		return require(e == nil && v.Version == 33, "capacity failure partially changed version")
	case "mapping-conflict":
		bad := scope()
		bad.Authority = "other-authority"
		s, e := h.exec.WithResourceControl(bad, h.target)
		if e != nil {
			return e
		}
		_, e = s.GetResourceControl(ctx, scope().Ref)
		if !authorization.Is(e, authorization.IdentityConflict) {
			return fmt.Errorf("authority remapped %v", e)
		}
		bad = scope()
		bad.Aliases = append(bad.Aliases, execution.ResourceRef{Namespace: "other", Kind: "counter", Key: "main"})
		_, e = h.exec.WithResourceControl(bad, h.target)
		return require(authorization.Is(e, authorization.Invalid), "mapping expanded across namespace")
	}
	return fmt.Errorf("unknown boundary %s", name)
}
