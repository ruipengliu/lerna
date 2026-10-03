package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type delayedOriginalPreparation struct {
	domain.Driver
	prepared  atomic.Uint64
	completed time.Time
}

func (d *delayedOriginalPreparation) Prepare(ctx context.Context, s rt.Scope, a rt.Auth, p domain.InvokeInput, i domain.ExecutionIntent, body []byte) (domain.PreparedRequest, error) {
	d.prepared.Add(1)
	timer := time.NewTimer(5200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return domain.PreparedRequest{}, ctx.Err()
	case <-timer.C:
	}
	out, err := d.Driver.Prepare(ctx, s, a, p, i, body)
	d.completed = time.Now().UTC()
	return out, err
}

type finiteControlPreparation struct {
	*executionAuthority
	f        *executionFixture
	calls    atomic.Uint64
	lost     bool
	current  func() error
	original api.Command
	after    func() error
}

func (a *finiteControlPreparation) PrepareControlWindow(ctx context.Context, s rt.Scope, r domain.StartRequest) (api.ControlSnapshot, error) {
	a.calls.Add(1)
	if a.current != nil {
		if err := a.current(); err != nil {
			return api.ControlSnapshot{}, err
		}
	}
	var frozen struct {
		Command api.Command         `json:"command"`
		Window  api.ControlSnapshot `json:"window"`
	}
	status, err := a.f.st.Within(ctx, s, []string{"control_source"}, func(tx rt.Tx) error {
		if _, err := tx.Get(ctx, "control_source.preparations", r.AttemptID, &frozen); err == nil {
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		w := r.Invoke.ControlSnapshot
		w.WindowID = api.NewID("window")
		w.IssuedAt = api.Time(now)
		w.StartBefore = api.Time(now.Add(5 * time.Second))
		c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.OwnerID, CommandID: api.NewID("command"), Method: "execution.control", TargetID: w.TaskID, ExpiresAt: w.StartBefore, Payload: api.Raw(domain.ControlInput{TaskRef: r.Invoke.TaskRef, Snapshot: w})}
		frozen.Command, frozen.Window = c, w
		return tx.Create(ctx, "control_source.preparations", r.AttemptID, r.Invoke.OperationID, frozen)
	})
	if status == rt.CommitUnknown {
		return api.ControlSnapshot{}, rt.ErrCommitUnknown
	}
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	a.original = frozen.Command
	receipt, err := a.f.dispatcher.Command(ctx, a.f.auth, api.Raw(frozen.Command))
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	if receipt.Error != nil {
		return api.ControlSnapshot{}, receipt.Error
	}
	if a.after != nil {
		fn := a.after
		a.after = nil
		if err := fn(); err != nil {
			return api.ControlSnapshot{}, err
		}
	}
	if a.lost {
		a.lost = false
		return api.ControlSnapshot{}, rt.ErrCommitUnknown
	}
	return frozen.Window, nil
}
func configureFinitePreparation(t *testing.T, f *executionFixture) (*delayedOriginalPreparation, *finiteControlPreparation) {
	t.Helper()
	driver := &delayedOriginalPreparation{Driver: &target.FileDriver{Files: f.files, Content: f.content, Location: "device"}}
	authority := &finiteControlPreparation{executionAuthority: f.authority, f: f}
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: authority, Location: "device", Drivers: []domain.Driver{driver}})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
	return driver, authority
}
func invokeFinitePreparation(t *testing.T, f *executionFixture) (domain.InvokeInput, api.Command) {
	t.Helper()
	in := f.invokeInput(t, false)
	now := time.Now().UTC()
	in.ControlSnapshot.IssuedAt = api.Time(now)
	in.ControlSnapshot.StartBefore = api.Time(now.Add(5 * time.Second))
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.sc.OwnerID, CommandID: api.NewID("command"), TargetID: in.OperationID, Method: "execution.invoke", ExpiresAt: in.Deadline, Payload: api.Raw(in)}
	receipt, err := f.dispatcher.Command(context.Background(), f.auth, api.Raw(c))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original invoke %+v %v", receipt, err)
	}
	return in, c
}
func TestExpensivePreparationFinishesBeforeOriginalAttemptGetsFiniteStartWindow(t *testing.T) {
	f := newExecutionFixture(t)
	driver, authority := configureFinitePreparation(t, f)
	in, original := invokeFinitePreparation(t, f)
	f.drain(t)
	body, err := os.ReadFile(filepath.Join(f.root, "report.md"))
	if err != nil || string(body) != "report target truth\n" {
		t.Fatalf("preparation exhausted window before actual original target: %q %v", body, err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &view)
	if len(view.Attempts.Items) != 1 || view.Attempts.Items[0].Effect != "applied" || driver.prepared.Load() != 1 || authority.calls.Load() != 1 {
		t.Fatalf("original attempt/preparation duplicated: %+v calls%d/%d", view, driver.prepared.Load(), authority.calls.Load())
	}
	var control domain.ControlInput
	if err = api.Decode(authority.original.Payload, &control); err != nil {
		t.Fatal(err)
	}
	issued, err := api.ParseTime(control.Snapshot.IssuedAt)
	if err != nil {
		t.Fatal(err)
	}
	before, err := api.ParseTime(control.Snapshot.StartBefore)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Before(driver.completed) || before.Sub(issued) != 5*time.Second || view.Attempts.Items[0].ControlWindowID != control.Snapshot.WindowID {
		t.Fatal("actual start did not bind finite post-preparation original window")
	}
	receipt, err := f.dispatcher.Lookup(context.Background(), f.auth, original.CommandID)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original invoke changed %+v %v", receipt, err)
	}
	stored, err := f.st.LookupCommand(context.Background(), f.sc, original.CommandID)
	if err != nil || !api.Equal(stored.Command, original) {
		t.Fatalf("new window rewrote original invoke: %+v %v", stored.Command, err)
	}
}
func TestLostPreparedWindowReplyKeepsOriginalAttemptAndNeverRefreshesExpiredWindow(t *testing.T) {
	f := newExecutionFixture(t)
	driver, authority := configureFinitePreparation(t, f)
	authority.lost = true
	in, _ := invokeFinitePreparation(t, f)
	works, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("original worker claim %+v %v", works, err)
	}
	handler, _ := f.registry.Job(domain.RunJob)
	if err = handler(context.Background(), f.st, f.sc, works[0]); !errors.Is(err, rt.ErrCommitUnknown) {
		t.Fatalf("lost original window reply not unknown: %v", err)
	}
	var before domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &before)
	if len(before.Attempts.Items) != 1 || before.Attempts.Items[0].Phase != "prepared" {
		t.Fatalf("unknown original window lost prepared responsibility: %+v", before)
	}
	var window domain.ControlInput
	if err = api.Decode(authority.original.Payload, &window); err != nil {
		t.Fatal(err)
	}
	until, _ := api.ParseTime(window.Snapshot.StartBefore)
	timer := time.NewTimer(time.Until(until) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	command := authority.original
	if err = handler(context.Background(), f.st, f.sc, works[0]); err != nil {
		t.Fatal(err)
	}
	var after domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &after)
	if len(after.Attempts.Items) != 1 || after.Attempts.Items[0].AttemptID != before.Attempts.Items[0].AttemptID || after.Operation.Effect != "not_started" || driver.prepared.Load() != 1 || !api.Equal(command, authority.original) {
		t.Fatalf("recovery refreshed old original window or action: %+v calls%d", after, driver.prepared.Load())
	}
	if _, err = os.ReadFile(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("expired original preparation performed target IO: %v", err)
	}
}

func TestCancellationAfterExpensivePreparationNeverGetsNewStartWindow(t *testing.T) {
	f := newExecutionFixture(t)
	driver, authority := configureFinitePreparation(t, f)
	in, _ := invokeFinitePreparation(t, f)
	authority.current = func() error {
		receipt := f.command(t, "execution.cancel", in.OperationID, domain.CancelInput{OperationID: in.OperationID, Reason: "cancel during original preparation", TaskRef: in.TaskRef, OrchestratorID: in.TaskRef.OwnerID}, nil)
		if receipt.Stage != "applied" {
			t.Fatalf("original cancel %+v", receipt)
		}
		return api.E("invalid_state", "current_original_task_cancelled")
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &view)
	if len(view.Attempts.Items) != 1 || view.Operation.Effect != "not_started" || driver.prepared.Load() != 1 || authority.original.CommandID != "" {
		t.Fatalf("cancelled original preparation got a start window: %+v", view)
	}
	if _, err := os.ReadFile(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled preparation reached target IO: %v", err)
	}
}

func TestPreparedAttemptCommitUnknownResumesItsOriginalEncodedRequest(t *testing.T) {
	f := newExecutionFixture(t)
	if f.postgresDSN != "" {
		t.Skip("real SQLite commit acknowledgement fault")
	}
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	var armed atomic.Bool
	st, err := sqlite.Open(f.databasePath, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && armed.CompareAndSwap(true, false) {
			return api.E("dependency_unavailable", "original_prepared_commit_reply_lost")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st, f.dispatcher.Store = st, st
	driver, authority := configureFinitePreparation(t, f)
	in, _ := invokeFinitePreparation(t, f)
	works, status, err := st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("claim %+v %v", works, err)
	}
	armed.Store(true)
	handler, _ := f.registry.Job(domain.RunJob)
	if err = handler(context.Background(), st, f.sc, works[0]); !errors.Is(err, rt.ErrCommitUnknown) {
		t.Fatalf("prepared actual commit not unknown: %v", err)
	}
	var before domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &before)
	if len(before.Attempts.Items) != 1 || before.Attempts.Items[0].Phase != "prepared" || authority.calls.Load() != 0 {
		t.Fatalf("unknown original preparation left no durable attempt: %+v", before)
	}
	if err = handler(context.Background(), st, f.sc, works[0]); err != nil {
		t.Fatal(err)
	}
	var after domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &after)
	if len(after.Attempts.Items) != 1 || after.Attempts.Items[0].AttemptID != before.Attempts.Items[0].AttemptID || after.Operation.Effect != "applied" || driver.prepared.Load() != 1 {
		t.Fatalf("commit recovery changed original attempt/request: %+v", after)
	}
}

func TestOldWorkerAfterPreparedWindowCannotEnterActualTarget(t *testing.T) {
	f := newExecutionFixture(t)
	driver, authority := configureFinitePreparation(t, f)
	in, _ := invokeFinitePreparation(t, f)
	old, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(old) != 1 {
		t.Fatalf("old claim %+v %v", old, err)
	}
	var current []rt.Work
	authority.after = func() error {
		if err := rt.Finish(context.Background(), f.st, f.sc, []string{"execution"}, old[0], rt.Ready(time.Now().Add(-time.Second)), nil); err != nil {
			return err
		}
		var err error
		var status rt.CommitStatus
		current, status, err = f.st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, 30*time.Second)
		if err != nil {
			return err
		}
		if status != rt.Committed || len(current) != 1 {
			t.Fatal("actual new worker did not take original job")
		}
		return nil
	}
	handler, _ := f.registry.Job(domain.RunJob)
	if err = handler(context.Background(), f.st, f.sc, old[0]); !errors.Is(err, rt.ErrClaimLost) {
		t.Fatalf("old worker entered after actual claim changed: %v", err)
	}
	if _, err = os.ReadFile(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("old worker performed target IO: %v", err)
	}
	command := authority.original
	if err = handler(context.Background(), f.st, f.sc, current[0]); err != nil {
		t.Fatal(err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &view)
	if len(view.Attempts.Items) != 1 || view.Operation.Effect != "applied" || driver.prepared.Load() != 1 || !api.Equal(command, authority.original) {
		t.Fatalf("new worker changed original preparation/window: %+v", view)
	}
}
