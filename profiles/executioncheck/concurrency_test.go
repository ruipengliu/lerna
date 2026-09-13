package executioncheck

import (
	"context"
	"errors"
	"fmt"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"testing"
	"time"
)

func TestIndependentCoordinatorsStartOnce(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	r, material, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, material); err != nil {
		t.Fatal(err)
	}
	other, err := open(ctx, h.root, h.token)
	if err != nil {
		t.Fatal(err)
	}
	defer other.close()
	done := make(chan error, 2)
	for _, svc := range []*execution.Service{h.exec, other.exec} {
		go func() { _, e := svc.Run(ctx, r.OperationID); done <- e }()
	}
	for range 2 {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	target, err := h.target.Snapshot(ctx)
	if err != nil || target.Changes != 1 {
		t.Fatalf("target=%+v err=%v", target, err)
	}
}

func TestLateInspectorRetainsEvidence(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	r, material, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.replace(wrappedDriver{Driver: h.target, inspect: func(context.Context, execution.Call) (execution.Observation, error) {
		return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
	}}, h.work); err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, material); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	for range config().MaxChecks - 1 {
		if _, err = h.client.Reconcile(ctx, r.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	release := make(chan struct{})
	if err = h.replace(wrappedDriver{Driver: h.target, inspect: func(_ context.Context, c execution.Call) (execution.Observation, error) {
		<-release
		return h.target.Inspect(ctx, c)
	}}, h.work); err != nil {
		t.Fatal(err)
	}
	out, err := h.client.Reconcile(ctx, r.OperationID)
	close(release)
	if err != nil || out.Effect != "UNKNOWN" {
		t.Fatalf("timeout=%+v err=%v", out, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, err = h.client.GetInvocation(ctx, r.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if out.Effect == "CONFIRMED" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("late independent effect evidence discarded")
}

type stalledAuthority struct{ execution.Authority }

func (a stalledAuthority) UpdateExecution(ctx context.Context, _ func(authorization.ExecutionTransaction) error) error {
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("execution storage has no finite deadline")
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestExecutionStorageDeadline(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	c := config()
	c.IOTimeout = 20 * time.Millisecond
	svc, err := execution.New(stalledAuthority{h.grants}, h.work, h.access, h.target, h.binding, h.cap, c, h.operation)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetInvocation(ctx, "missing")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded storage: %v", err)
	}
}

func TestReconcileDoesNotBypassReadPermission(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	r, m, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, m); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	snap, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"capability.reconcile"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(time.Hour).Unix()}
	_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "reconcile-only", Scope: scope}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.GetInvocation(ctx, r.OperationID); err == nil {
		t.Fatal("read unexpectedly allowed")
	}
	if _, err = h.client.Reconcile(ctx, r.OperationID); err == nil {
		t.Fatal("reconcile leaked result without read permission")
	}
	if out, err := h.exec.Reconcile(ctx, r.OperationID); err != nil || out.Effect != "CONFIRMED" {
		t.Fatalf("trusted reconciliation disabled: %+v %v", out, err)
	}
}
