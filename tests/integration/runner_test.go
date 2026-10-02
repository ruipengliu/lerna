package integration_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
)

// Lose both local and PostgreSQL hints while retaining the real database.
type quietBackend struct{ durable.Backend }

func (quietBackend) Notify()               {}
func (quietBackend) Wake() <-chan struct{} { return nil }

func runnerOptions() durable.RunnerOptions {
	return durable.RunnerOptions{Scope: scope, Capacity: 1, Batch: 1, Steps: 2, Scan: 10 * time.Millisecond, Lease: 500 * time.Millisecond, Renew: 50 * time.Millisecond, WorkTimeout: 2 * time.Second, Drain: 60 * time.Millisecond}
}
func TestDurableWorkLoop(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			t.Run("FW06_ActualExitOwnsCapacity", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "blocked")
				s.submit(t, "second")
				started := make(chan struct{})
				cancelled := make(chan struct{})
				release := make(chan struct{})
				var calls atomic.Int64
				h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
					calls.Add(1)
					close(started)
					<-c.Done()
					close(cancelled)
					<-release
					return nil
				})
				r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, runnerOptions())
				if err != nil {
					t.Fatal(err)
				}
				life, stop := context.WithCancel(ctx())
				run := make(chan error, 1)
				go func() { run <- r.Run(life) }()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("handler did not start")
				}
				stop()
				<-cancelled
				select {
				case err := <-run:
					if !errors.Is(err, durable.ErrDrain) {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("drain did not bound stop")
				}
				if r.Active() != 1 || calls.Load() != 1 || s.job(t, "second").State != "ready" {
					t.Fatal("cancel released actual capacity")
				}
				close(release)
				wait, cancel := context.WithTimeout(ctx(), time.Second)
				defer cancel()
				if err := r.Wait(wait); err != nil {
					t.Fatal(err)
				}
				if r.Active() != 0 || s.job(t, "blocked").State == "done" {
					t.Fatal("nil handler return implied done")
				}
			})
			t.Run("FW06_ScanAndRequestIndependence", func(t *testing.T) {
				s := newSuite(t, driver)
				s.e, _ = durable.New(quietBackend{s.store}, durable.Options{Kinds: []string{kind}})
				s.p, _ = s.e.Register("probe")
				request, disconnect := context.WithCancel(ctx())
				i := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
				_, result := s.e.Admit(request, scope, "service", i, s.admission("scan", false, false))
				mustCommit(t, result)
				disconnect()
				done := make(chan struct{}, 1)
				life, stop := context.WithCancel(ctx())
				defer stop()
				h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
					return w.Step(c, func() error {
						result := s.finish(t, w.Claim(), durable.Done(), false)
						if result.Outcome != durable.Committed {
							return result.Err
						}
						done <- struct{}{}
						return nil
					})
				})
				r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, runnerOptions())
				if err != nil {
					t.Fatal(err)
				}
				run := make(chan error, 1)
				go func() { run <- r.Run(life) }()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("notification-free scan did not find work")
				}
				stop()
				if err := <-run; err != nil {
					t.Fatal(err)
				}
				if _, v := s.value(t, "scan"); v != 1 {
					t.Fatal("request cancellation lost background work")
				}
			})
			t.Run("FW06_ControlHasIndependentCapacity", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "ordinary")
				mustCommit(t, s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					if err := s.lock(tx, "control"); err != nil {
						return err
					}
					_, err := tx.Raise(durable.JobKey{Kind: "control", Responsibility: "control"}, "control", time.Now())
					return err
				}))
				started := make(chan struct{})
				release := make(chan struct{})
				ordinary := durable.HandlerFunc(func(context.Context, *durable.Work) error { close(started); <-release; return nil })
				completed := make(chan struct{}, 1)
				control := durable.HandlerFunc(func(_ context.Context, w *durable.Work) error {
					result := s.finish(t, w.Claim(), durable.Done(), false)
					if result.Outcome != durable.Committed {
						return result.Err
					}
					completed <- struct{}{}
					return nil
				})
				one, _ := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: ordinary}}, runnerOptions())
				two, _ := durable.NewRunner(s.e, []durable.Binding{{Kind: "control", Handler: control}}, runnerOptions())
				life, stop := context.WithCancel(ctx())
				runs := make(chan error, 2)
				go func() { runs <- one.Run(life) }()
				<-started
				go func() { runs <- two.Run(life) }()
				select {
				case <-completed:
				case <-time.After(time.Second):
					t.Fatal("control capacity starved by blocked ordinary work")
				}
				stop()
				close(release)
				for n := 0; n < 2; n++ {
					if err := <-runs; err != nil {
						t.Fatal(err)
					}
				}
			})
		})
	}
}
