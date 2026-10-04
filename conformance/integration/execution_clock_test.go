package integration_test

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type executionClockStore struct {
	rt.Store
	rt.QueryBindingStore
	now   time.Time
	fault error
}
type executionClockTx struct {
	rt.Tx
	clock *executionClockStore
}

func (s *executionClockStore) Within(ctx context.Context, scope rt.Scope, parts []string, fn func(rt.Tx) error) (rt.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx rt.Tx) error { return fn(executionClockTx{Tx: tx, clock: s}) })
}
func (tx executionClockTx) Now(context.Context) (time.Time, error) {
	return tx.clock.now, tx.clock.fault
}
func (tx executionClockTx) Savepoint(ctx context.Context, fn func(rt.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner rt.Tx) error { return fn(executionClockTx{Tx: inner, clock: tx.clock}) })
}

func TestOriginalAttemptSelectsWindowUsingTrustedStoreClock(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(map[bool]string{false: "trusted_clock", true: "clock_failure_retains_original"}[fault], func(t *testing.T) {
			f := newExecutionFixture(t)
			ctx := context.Background()
			clock := &executionClockStore{Store: f.st, QueryBindingStore: f.st.(rt.QueryBindingStore), now: time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)}
			f.st = clock
			f.dispatcher.Store = clock
			original := f.invokeInput(t, false)
			original.ControlSnapshot.IssuedAt = api.Time(clock.now)
			original.ControlSnapshot.StartBefore = api.Time(clock.now.Add(5 * time.Second))
			accepted := f.command(t, "execution.invoke", original.OperationID, original, nil)
			if accepted.Stage != "applied" {
				t.Fatal(accepted)
			}
			clock.now = clock.now.Add(4 * time.Second)
			current := original.ControlSnapshot
			current.WindowID = api.NewID("window")
			current.IssuedAt = api.Time(clock.now)
			current.StartBefore = api.Time(clock.now.Add(5 * time.Second))
			installed := f.command(t, "execution.control", original.TaskRef.ObjectID, domain.ControlInput{TaskRef: original.TaskRef, Snapshot: current}, nil)
			if installed.Stage != "applied" {
				t.Fatal(installed)
			}
			clock.now = clock.now.Add(2 * time.Second)
			works, status, e := f.st.Claim(ctx, f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, time.Minute)
			if e != nil || status != rt.Committed || len(works) != 1 {
				t.Fatalf("original claim %+v %s %v", works, status, e)
			}
			handler, _ := f.registry.Job(domain.RunJob)
			if fault {
				failed := errors.New("trusted adjudication clock unavailable")
				clock.fault = failed
				if e = handler(ctx, f.st, f.sc, works[0]); !errors.Is(e, failed) {
					t.Fatalf("clock error replaced: %v", e)
				}
				if _, e = os.Stat(filepath.Join(f.root, "report.md")); !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("clock failure entered target: %v", e)
				}
				clock.fault = nil
				var before domain.OperationView
				f.query(t, "execution.get", original.OperationID, domain.OperationIDInput{OperationID: original.OperationID}, &before)
				if before.Operation.ExecutionState == "closed" || len(before.Attempts.Items) != 0 {
					t.Fatalf("clock failure closed original responsibility: %+v", before)
				}
			}
			if e = handler(ctx, f.st, f.sc, works[0]); e != nil {
				t.Fatal(e)
			}
			actual, e := os.ReadFile(filepath.Join(f.root, "report.md"))
			if e != nil || string(actual) != "report target truth\n" {
				t.Fatalf("trusted current window did not admit exact original target: %q %v", actual, e)
			}
			var view domain.OperationView
			f.query(t, "execution.get", original.OperationID, domain.OperationIDInput{OperationID: original.OperationID}, &view)
			if len(view.Attempts.Items) != 1 || view.Attempts.Items[0].ControlWindowID != current.WindowID {
				t.Fatalf("attempt selected wrong authority clock/window: %+v", view)
			}
		})
	}
}
