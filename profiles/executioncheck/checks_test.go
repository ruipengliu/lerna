package executioncheck

import (
	"context"
	"testing"
)

func TestSynchronousExecutionChangesTargetOnce(t *testing.T) {
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
	receipt, e := h.client.Invoke(ctx, r, m)
	if e != nil {
		t.Fatal(e)
	}
	again, e := h.client.Invoke(ctx, r, m)
	if e != nil || again != receipt {
		t.Fatal("admission replay", e)
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	if out.Effect != "CONFIRMED" {
		t.Fatalf("effect %s", out.Effect)
	}
	if e = h.exec.Drain(ctx, 8); e != nil {
		t.Fatal(e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil || target.Value != 3 || target.Changes != 1 {
		t.Fatal("wrong effect", e)
	}
	task, e := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if e != nil || task.State != "COMPLETED" {
		t.Fatal("report not consumed", e)
	}
	_, e = h.exec.Run(ctx, r.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	target, _ = h.target.Snapshot(ctx)
	if target.Changes != 1 {
		t.Fatal("duplicate action")
	}
}

func TestExecutionContracts(t *testing.T) {
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if e := check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestExecutionFaults(t *testing.T) {
	for _, name := range faultNames {
		t.Run(name, func(t *testing.T) {
			if e := faultCheck(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestV09Upgrade(t *testing.T) {
	if e := upgrade(context.Background()); e != nil {
		t.Fatal(e)
	}
}
