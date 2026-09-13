package executioncheck

import (
	"context"
	"lerna/contextassembly"
	"lerna/execution"
	"testing"
	"time"
)

type contextGuard struct{ revoked bool }

func (g *contextGuard) ValidateStart(context.Context, execution.Request) error {
	if g.revoked {
		return contextassembly.Invalidated
	}
	return nil
}
func TestContextInvalidationPreventsNewStartButPreservesStartedOperation(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	req, material, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, req, material); e != nil {
		t.Fatal(e)
	}
	guard := &contextGuard{revoked: true}
	bound, e := h.exec.BindStartGuard(guard)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = bound.Run(ctx, req.OperationID); e != contextassembly.Invalidated {
		t.Fatalf("invalid context start: %v", e)
	}
	invocation, e := bound.GetInvocation(ctx, req.OperationID)
	if e != nil || invocation.Started {
		t.Fatalf("invalid context marked started: %+v %v", invocation, e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil || target.Changes != 0 {
		t.Fatalf("invalid context caused effects: %+v %v", target, e)
	}
	guard.revoked = false
	if _, e = bound.Run(ctx, req.OperationID); e != nil {
		t.Fatal(e)
	}
	guard.revoked = true
	if _, e = bound.Run(ctx, req.OperationID); e != nil {
		t.Fatalf("historical start blocked by new context state: %v", e)
	}
	target, e = h.target.Snapshot(ctx)
	if e != nil || target.Changes != 1 {
		t.Fatalf("historical operation repeated: %+v %v", target, e)
	}
}

type boundedContextGuard struct{}

func (boundedContextGuard) ValidateStart(ctx context.Context, _ execution.Request) error {
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestContextStartCheckHasItsOwnBoundedBudget(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	req, material, e := h.request(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.client.Invoke(ctx, req, material); e != nil {
		t.Fatal(e)
	}
	bound, e := h.exec.BindStartGuard(boundedContextGuard{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = bound.Run(ctx, req.OperationID); e != nil {
		t.Fatalf("context check consumed subsequent transaction IO budget: %v", e)
	}
}
