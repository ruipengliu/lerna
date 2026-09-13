package asynccheck

import (
	"context"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"testing"
	"time"
)

func TestAsyncJobCompletesThroughDurableAssociation(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	if out.Phase != "IN_PROGRESS" {
		t.Fatalf("phase=%s", out.Phase)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		t.Fatal(e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil || target.Changes != 0 {
		t.Fatal(target, e)
	}
	if e = h.target.Complete(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	h.clock.advance(time.Second)
	out, e = h.client.Reconcile(ctx, r.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	if out.Effect != "CONFIRMED" {
		t.Fatalf("effect=%s", out.Effect)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		t.Fatal(e)
	}
	task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil || task.State != "COMPLETED" {
		t.Fatal(task, e)
	}
	target, e = h.target.Snapshot(ctx)
	if e != nil || target.Changes != 1 || target.Value != 3 {
		t.Fatal(target, e)
	}
}

func TestCancelAcceptedIsNotStopped(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	op, e := h.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: op, Invocation: r.OperationID})
	if e != nil {
		t.Fatal(e)
	}
	again, e := h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: op, Invocation: r.OperationID})
	if e != nil || again != receipt {
		t.Fatal(again, e)
	}
	cancel, e := h.client.GetCancel(ctx, op)
	if e != nil || cancel.Progress != "ACCEPTED" {
		t.Fatal(cancel, e)
	}
	if _, e = h.exec.RunCancel(ctx, op); e != nil {
		t.Fatal(e)
	}
	h.clock.advance(time.Second)
	if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	cancel, e = h.client.GetCancel(ctx, op)
	if e != nil || cancel.Progress != "STOPPED" {
		t.Fatal(cancel, e)
	}
	if e = h.target.Complete(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil || target.Changes != 0 {
		t.Fatal(target, e)
	}
}

func TestCompletionBeforeCancelUsesTargetTime(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	if e = h.target.Complete(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	h.clock.advance(time.Second)
	controls, e := h.core.Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil {
		t.Fatal(e)
	}
	op, e := h.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: r.Qualification.Ref, ExpectedVersion: task.Version, Intent: "CANCEL"}); e != nil {
		t.Fatal(e)
	}
	if e = h.exec.Recover(ctx, 16); e != nil {
		t.Fatal(e)
	}
	task, e = h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil || task.State != "COMPLETED" {
		t.Fatal("completion predates cancel", task, e)
	}
}

func TestAsyncPolicyIsFrozen(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	h.cap.Async.Source = "mutated"
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal("caller changed service policy", e)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
}

func TestLateProgressDoesNotBlockOutbox(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	if e = h.target.Complete(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	f, e := h.target.InspectAsync(ctx, execution.Call{Request: r}, execution.Association{})
	if e != nil {
		t.Fatal(e)
	}
	p, e := h.exec.BindProgress("job-driver")
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Accept(ctx, r.OperationID, f); e != nil {
		t.Fatal(e)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		t.Fatal(e)
	}
	f.Version++
	f.CompletedAt = 0
	f.Observation = execution.Observation{Phase: "IN_PROGRESS", Result: "UNKNOWN", Effect: "UNKNOWN", Evidence: []byte(`{"late":true}`)}
	if e = p.Accept(ctx, r.OperationID, f); e != nil {
		t.Fatal(e)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		t.Fatal("late progress blocked delivery", e)
	}
}

func TestTaskCancelBeforeStartSettlesOriginalWork(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	controls, e := h.core.Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil {
		t.Fatal(e)
	}
	op, e := h.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: r.Qualification.Ref, ExpectedVersion: task.Version, Intent: "CANCEL"}); e != nil {
		t.Fatal(e)
	}
	if e = h.exec.Recover(ctx, 16); e != nil {
		t.Fatal(e)
	}
	task, e = h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil || task.State != "CANCELLED" {
		t.Fatal("cancel left work in flight", task, e)
	}
}

func TestCancelCannotReuseExecutionPermitIdentity(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	reserved := r
	reserved.OperationID, e = h.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	material, e := h.issue(ctx, reserved)
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.grants.ReserveUse(ctx, material, h.binding.Presentation(reserved), &wire.AuthorizationAction{Resource: "root", Action: "resource.change", Purpose: "task", Location: "local"}, 1)
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: reserved.OperationID, Invocation: r.OperationID})
	if !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatal("permit identity reused for cancel", e)
	}
}

func TestRecoveryPassRotatesPastIdleAdmission(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	a, am, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b, bm, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, a, am); e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, b, bm); e != nil {
		t.Fatal(e)
	}
	active := a
	if a.OperationID < b.OperationID {
		active = b
	}
	if _, e = h.exec.Run(ctx, active.OperationID); e != nil {
		t.Fatal(e)
	}
	if e = h.target.Complete(ctx, active.OperationID); e != nil {
		t.Fatal(e)
	}
	h.clock.advance(time.Second)
	for range 2 {
		if e = h.exec.Recover(ctx, 1); e != nil {
			t.Fatal(e)
		}
	}
	result, e := h.client.GetInvocation(ctx, active.OperationID)
	if e != nil || result.Effect != "CONFIRMED" {
		t.Fatal("idle admission starved recovery", result, e)
	}
}

func TestProgressUsesBoundedDriverSlot(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		t.Fatal(e)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		t.Fatal(e)
	}
	fact, e := h.target.InspectAsync(ctx, execution.Call{Request: r}, execution.Association{})
	if e != nil {
		t.Fatal(e)
	}
	fact.Version++
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	if e = h.replace(driverWrap{AsyncDriver: h.target, cancel: h.target, inspect: func(c context.Context, in execution.Call, a execution.Association) (execution.Fact, error) {
		close(entered)
		<-release
		return h.target.InspectAsync(c, in, a)
	}}); e != nil {
		t.Fatal(e)
	}
	h.clock.advance(time.Second)
	go func() { defer close(done); _, _ = h.exec.Reconcile(ctx, r.OperationID) }()
	<-entered
	port, e := h.exec.BindProgress("job-driver")
	if e != nil {
		close(release)
		<-done
		t.Fatal(e)
	}
	e = port.Accept(ctx, r.OperationID, fact)
	close(release)
	<-done
	if !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("push bypassed bounded I/O slot", e)
	}
}

func TestInvokeRecoversPermitAllocatedBeforeInvocation(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	r, m, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	use, e := h.grants.ReserveUse(ctx, m, h.binding.Presentation(r), &wire.AuthorizationAction{Resource: "root", Action: "resource.change", Purpose: "task", Location: "local"}, 1)
	if e != nil {
		t.Fatal(e)
	}
	// The issuer can return a new grant after the caller loses its reply. The
	// consumer must recover the original allocation and validate it at admission.
	replacement, e := h.issue(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, r, replacement); e != nil {
		t.Fatal(e)
	}
	record, e := h.exec.GetInvocation(ctx, r.OperationID)
	if e != nil || record.Permit != use {
		t.Fatalf("original allocation was not recovered: %v", e)
	}
}
