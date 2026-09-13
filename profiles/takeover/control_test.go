package takeover

import (
	"context"
	"lerna/execution"
	"testing"
)

func TestTakeoverBlocksBothAPIEntrypoints(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	other, e := h.other(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer other.close()
	state, e := h.client.GetResourceControl(ctx, scope().Ref)
	if e != nil {
		t.Fatal(e)
	}
	op, e := h.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: "TAKEOVER", ExpectedVersion: state.Version})
	if e != nil {
		t.Fatal(e)
	}
	for _, service := range []*harness{h, other} {
		r, m, e := service.request(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = service.client.Invoke(ctx, r, m); e == nil {
			t.Fatal("taken-over resource admitted invocation")
		}
	}
	state, e = h.exec.AdvanceResourceControl(ctx, scope().Ref)
	if e != nil || state.Progress != "APPLIED" {
		t.Fatal(state, e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil || target.Changes != 0 {
		t.Fatal(target, e)
	}
}
