package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

func TestKnownCloudRevocationPreventsDeviceStartWithoutCloudTaskRead(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	window := f.control(t, "active", "running", 1)
	f.invoke(t, window)
	in := Revocation{AuthorityID: f.b.AuthorityID, EndpointID: f.b.EndpointID, ObjectRef: f.b.Lease.GrantRefs[0], Kind: "grant", IssuedAt: api.Time(time.Now()), StartBefore: api.Time(time.Now().Add(time.Minute))}
	digest, _ := api.Digest(in)
	var err error
	in.Proof, err = f.keys.Sign("development-es256", revocationClaims(in, digest))
	if err != nil {
		t.Fatal(err)
	}
	_, receipt := f.command(t, "executor.revocation.install", in.ObjectRef.ObjectID, in)
	if receipt.Stage != "applied" {
		t.Fatalf("revocation: %+v", receipt)
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if view.Operation.Effect != "not_started" || !view.NewAttemptsClosed || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].StartedAt != "" {
		t.Fatalf("revoked entry: %+v", view)
	}
	if _, err = os.Stat(filepath.Join(f.root, "files", "reports", "result.txt")); !os.IsNotExist(err) {
		t.Fatal("known revoke reached physical file")
	}
}
func TestDeviceCachesOriginalControlWindowAndExpiresWithoutRefresh(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	window := f.control(t, "active", "running", 1)
	original := f.invoke(t, window)
	// 真实墙钟等待原五秒窗口，不能只改夹具或续签同一 WindowID。
	deadline, _ := api.ParseTime(window.StartBefore)
	time.Sleep(time.Until(deadline) + 25*time.Millisecond)
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if view.Operation.Effect != "not_started" || !view.NewAttemptsClosed || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].StartedAt != "" {
		t.Fatalf("expired original window: %+v", view)
	}
	if _, err := os.Stat(filepath.Join(f.root, "files", "reports", "result.txt")); !os.IsNotExist(err) {
		t.Fatal("expired permit reached physical file")
	}
	_, _, err := f.h.Call(f.ctx, f.peer, "command", api.Raw(original))
	if err != nil {
		t.Fatal(err)
	}
	again := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if again.Operation.Revision != view.Operation.Revision {
		t.Fatal("replay refreshed original expiry")
	}
}

func TestUnknownDeviceWriteQueriesOriginalJournalAfterReopen(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	window := f.control(t, "active", "running", 1)
	original := f.invoke(t, window)
	f.h.Files.Fault = func(stage string) error {
		if stage == "renamed" {
			return os.ErrClosed
		}
		return nil
	}
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	beforeView := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if beforeView.Operation.Effect != "unknown" || len(beforeView.Attempts.Items) != 1 || beforeView.Attempts.Items[0].StartedAt == "" {
		t.Fatalf("unknown before restart: %+v", beforeView)
	}
	native, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
	if err != nil || string(native) != "independent cloud-admitted file bytes\n" {
		t.Fatalf("actual rename bytes: %q %v", native, err)
	}
	config := f.h.Config
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := Open(f.ctx, config, false)
	if err != nil {
		t.Fatal(err)
	}
	*f.h = *h
	_, _, err = f.h.Call(f.ctx, f.peer, "command", api.Raw(original))
	if err != nil {
		t.Fatal(err)
	}
	_, receipt := f.command(t, "execution.reconcile", f.b.Intent.OperationID, execution.ReconcileInput{OperationID: f.b.Intent.OperationID})
	if receipt.Stage != "applied" {
		t.Fatalf("reconcile: %+v", receipt)
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if view.Operation.Effect != "applied" || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != beforeView.Attempts.Items[0].AttemptID || !view.NewAttemptsClosed || !view.ActuallyStopped {
		t.Fatalf("late original fact: %+v", view)
	}
	entries, err := os.ReadDir(filepath.Join(f.root, "files", ".harness", "journals"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("original physical journal count: %d %v", len(entries), err)
	}
}
func TestDeviceCancelFirstAndCloudStopGatePersist(t *testing.T) {
	f := newDeviceFixture(t)
	_, r := f.command(t, "execution.cancel", f.b.Intent.OperationID, execution.CancelInput{OperationID: f.b.Intent.OperationID, Reason: "cloud original stop", TaskRef: f.b.Intent.TaskRef, OrchestratorID: f.b.AuthorityID})
	if r.Stage != "applied" {
		t.Fatalf("cancel first: %+v", r)
	}
	f.prepare(t)
	window := f.control(t, "active", "running", 1)
	in := execution.InvokeInput{OperationID: f.b.Intent.OperationID, TaskRef: f.b.Intent.TaskRef, GoalRevision: 1, ControlRevision: 1, CapabilityRef: f.b.Intent.CapabilityRef, BindingRef: f.b.Intent.BindingRef, IntentRef: f.b.IntentRef, IntentHash: f.b.ExecutionHash, UseRefs: f.b.UseRefs, Deadline: f.b.Intent.Deadline, ControlSnapshot: window, ReservationRef: f.b.ReservationRef}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.b.EndpointID, CommandID: f.b.OriginalCommandID, Method: "execution.invoke", TargetID: f.b.Intent.OperationID, ExpiresAt: f.b.Intent.Deadline, Payload: api.Raw(in)}
	_, raw, err := f.h.Call(f.ctx, f.peer, "command", api.Raw(c))
	if err != nil {
		t.Fatal(err)
	}
	var receipt api.Receipt
	if err = api.Decode(raw, &receipt); err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Reason != "operation_permanently_cancelled" {
		t.Fatalf("tombstone: %+v %v", receipt, err)
	}
	stopped := f.control(t, "active", "paused", 2)
	_, receipt = f.command(t, "execution.control", f.b.Intent.TaskRef.ObjectID, execution.ControlInput{TaskRef: f.b.Intent.TaskRef, Snapshot: stopped})
	if receipt.Stage != "applied" {
		t.Fatalf("new control: %+v", receipt)
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(f.root, "files", "reports", "result.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled device produced bytes")
	}
}
func TestOriginalDeviceAttemptSurvivesDatabaseAndTargetReopen(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	window := f.control(t, "active", "running", 1)
	original := f.invoke(t, window)
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	beforeView := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if len(beforeView.Attempts.Items) != 1 || beforeView.Operation.Effect != "applied" {
		t.Fatalf("initial: %+v", beforeView)
	}
	config := f.h.Config
	if err := f.h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := Open(f.ctx, config, false)
	if err != nil {
		t.Fatal(err)
	}
	*f.h = *h
	_, _, err = f.h.Call(f.ctx, f.peer, "command", api.Raw(original))
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != beforeView.Attempts.Items[0].AttemptID || view.Operation.Effect != "applied" {
		t.Fatalf("original reopen: %+v", view)
	}
	usage := deviceQuery[api.UsageSnapshot](t, f, "execution.usage.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if !usage.SpendingClosed || !usage.UsageFinal || usage.SourceRef.OwnerID != f.b.EndpointID || len(usage.ProofRefs) != 1 || usage.Cumulative[0].Value != "0" {
		t.Fatalf("device original usage: %+v", usage)
	}
	proof := deviceQuery[ContentChunk](t, f, "executor.content.get", usage.ProofRefs[0].ContentID, ContentGet{ContentRef: usage.ProofRefs[0], ChunkIndex: 0})
	if proof.Permission.ContentRef.OwnerID != f.b.EndpointID {
		t.Fatal("output ownership rewritten")
	}
}
