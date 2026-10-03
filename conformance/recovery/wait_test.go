//go:build integration

package recovery_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type waitStore interface {
	workStore
	demo.ScheduleRepository
	runtime.ScheduleStore
}

func TestPGPersistentWaitBehaviors(t *testing.T) {
	runWaitBehaviors(t, func(t *testing.T) waitStore {
		setup := database(t)
		value, _ := configurations.Load(setup)
		cfg := value.(postgres.Config)
		cfg.MaxOpenConnections = 1
		store, err := postgres.Open(contextFor(t), cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		admissionReopeners.Store(store, func(t *testing.T) admissionStore {
			replacement, err := postgres.Open(contextFor(t), cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { replacement.Close() })
			return replacement
		})
		return store
	})
}
func TestSQLitePersistentWaitBehaviors(t *testing.T) {
	runWaitBehaviors(t, func(t *testing.T) waitStore { return sqliteDatabase(t) })
}
func runWaitBehaviors(t *testing.T, factory func(*testing.T) waitStore) {
	t.Run("ProcessCancellationReturnsClaimWithoutBusinessStop", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		worker.Runner = &startConfirmationFault{runner: store, cancel: cancel}
		if err = worker.Process(runCtx, batch[0]); !errors.Is(err, context.Canceled) {
			t.Fatalf("process shutdown: %v", err)
		}
		got, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || got.State.Attempts != 1 || got.State.Stopped || got.Job.State != "ready" || got.Job.CompletedRevision != 0 || got.ActiveClaim {
			t.Fatalf("shutdown retained responsibility: %+v %v", got, err)
		}
		if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "input", 1)
	})
	t.Run("OldPermanentClosureKeepsNewWorkAndPreviousSuccessfulProjection", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		worker := scheduledWorker(t, store, clock)
		out, err := h.Record(ctx, command("first", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		if _, err = worker.Step(ctx, "first", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "input", 1)
		policy := demo.DefaultPolicy()
		policy.PermanentReason = "precondition_failed"
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		revision := contract.Revision("1")
		out, err = h.Record(ctx, command("second", "input", "hello", &revision, future()), &principal)
		assertReceived(t, out, err)
		batch, err := worker.Claim(ctx, "second", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		old := batch[0]
		startWork(t, worker, old)
		h.Policies = nil
		revision = "2"
		out, err = h.Record(ctx, command("third", "input", "hello", &revision, future()), &principal)
		assertReceived(t, out, err)
		if err = worker.Finish(ctx, old, "permanent", "precondition_failed", nil); err != nil {
			t.Fatal(err)
		}
		got, err := h.Observe(ctx, "input", &principal)
		if err != nil || got.Job.CompletedRevision != 2 || got.Job.WorkRevision != 3 || got.Job.State != "ready" || got.Projection == nil || got.Projection.InputRevision != 1 {
			t.Fatalf("separate closed/success revisions: %+v %v", got, err)
		}
		if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "input", 3)
	})
	t.Run("StartedWorkCannotPersistSuccessAtExecutionDeadline", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.ExecutionLimit = time.Millisecond
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		work := batch[0]
		startWork(t, worker, work)
		step, err := worker.Step(ctx, "next", 1, time.Minute, time.Second)
		if err != nil || step.Processed != 0 || !step.NextWake.Equal(clock.Time().Add(time.Millisecond)) {
			t.Fatalf("deadline wake precedes live lease: %+v %v", step, err)
		}
		clock.Advance(time.Millisecond)
		if err = worker.Complete(ctx, work.Claim, durablework.Project(work)); err != nil {
			t.Fatal(err)
		}
		state, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || state.State.Outcome != "expired" || state.State.Attempts != 1 || state.Job.CompletedRevision != 1 {
			t.Fatalf("deadline closure: %+v %v", state, err)
		}
		got, err := h.Observe(ctx, "input", &principal)
		if err != nil || got.Projection != nil {
			t.Fatalf("late successful digest persisted: %+v %v", got, err)
		}
	})
	t.Run("MissingStartOrPermissionsCannotPersistSuccess", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		work := batch[0]
		if err = worker.Complete(ctx, work.Claim, durablework.Project(work)); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("missing start accepted: %v", err)
		}
		unconfigured := durablework.NewWorker(owner, store, store, store, clock)
		if err = unconfigured.Complete(ctx, work.Claim, durablework.Project(work)); err == nil {
			t.Fatal("missing permissions persisted success")
		}
		got, err := h.Observe(ctx, "input", &principal)
		if err != nil || got.Job.CompletedRevision != 0 || got.Projection != nil {
			t.Fatalf("rejected completion leaked facts: %+v %v", got, err)
		}
		if err = worker.Process(ctx, work); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "input", 1)
	})
	t.Run("ClaimedPolicySnapshotAndWaitingSurviveReopen", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		h.ScheduleControl = true
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.Identity = "old-v1"
		policy.Lane = "control"
		policy.Gate = &demo.GateCondition{ID: "gate", Revision: 2}
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		wire := command("source", "input", "hello", nil, future())
		out, err := h.Record(ctx, wire, &principal)
		original := assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		old := batch[0]
		replacement := demo.DefaultPolicy()
		replacement.Identity = "new-v2"
		replacement.PermanentReason = "precondition_failed"
		policies, err = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: replacement}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err = h.Record(ctx, wire, &principal)
		assertReceiptSame(t, original, assertReceived(t, out, err))
		if err = worker.Process(ctx, old); err != nil {
			t.Fatal(err)
		}
		before, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || before.State.Policy.Identity != "old-v1" || before.State.Outcome != "waiting" {
			t.Fatalf("bound snapshot: %+v %v", before, err)
		}
		next := reopenAdmissionStore(t, store).(waitStore)
		h = hostFor(next, owner, principal)
		h.ScheduleControl = true
		h.Clock = clock
		worker = scheduledWorker(t, next, clock)
		after, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || after.State.Policy.Gate.Revision != 2 || !after.State.Due.Equal(before.State.Due) || !after.State.Deadline.Equal(before.State.Deadline) || after.ActiveClaim {
			t.Fatalf("durable predicate: %+v %v", after, err)
		}
		if err = h.AdvanceGate(ctx, "unknown", 2); !errors.Is(err, demo.ErrGate) {
			t.Fatalf("unknown gate: %v", err)
		}
		if err = h.AdvanceGate(ctx, "gate", 2); err != nil {
			t.Fatal(err)
		}
		if err = h.AdvanceGate(ctx, "gate", 2); err != nil {
			t.Fatal(err)
		}
		if err = h.AdvanceGate(ctx, "gate", 1); !errors.Is(err, demo.ErrGate) {
			t.Fatalf("regress gate: %v", err)
		}
		clock.Advance(time.Second)
		if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "input", 1)
	})
	t.Run("CommittedStartPersistsAndRepeatedConfirmationIsIdempotent", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.MaxAttempts = 1
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Millisecond)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		old := batch[0]
		worker.Runner = &startConfirmationFault{runner: store}
		state, started, err := worker.Start(ctx, old)
		if !errors.Is(err, runtime.ErrCommitUnknown) {
			t.Fatalf("committed start confirmation not lost: %v", err)
		}
		observed, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || observed.State.Attempts != 1 {
			t.Fatalf("authoritative committed start: %+v %v", observed, err)
		}
		state, started, err = worker.Start(ctx, old)
		if err != nil || !started || state.Attempts != 1 {
			t.Fatalf("repeated confirmation: %+v %v %v", state, started, err)
		}
		next := reopenAdmissionStore(t, store).(waitStore)
		h = hostFor(next, owner, principal)
		h.Clock = clock
		worker = scheduledWorker(t, next, clock)
		clock.Advance(time.Millisecond)
		if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		got, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || got.State.Attempts != 1 || got.State.Reason != "attempts_exhausted" || got.Job.CompletedRevision != 1 || got.ActiveClaim {
			t.Fatalf("start survived replacement: %+v %v", got, err)
		}
		observation, err := h.Observe(ctx, "input", &principal)
		if err != nil || observation.Projection != nil {
			t.Fatalf("unknown computation invented success: %+v %v", observation, err)
		}
	})
	t.Run("CurrentWorkerEligibilityIsCheckedBeforeActualStart", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "first", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		worker.Permissions.Revoke("first")
		if err = worker.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		got, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || got.State.Attempts != 0 || got.State.Outcome != "permanent" || got.State.Reason != "forbidden" || got.Job.CompletedRevision != 1 {
			t.Fatalf("revocation: %+v %v", got, err)
		}
		out, err = h.Record(ctx, command("healthy", "healthy", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
			t.Fatal(err)
		}
		requireHello(t, h, "healthy", 1)
	})
	t.Run("FiniteRetryPersistsAcrossReopenAndClosedOutcomes", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			failures  int64
			permanent string
			attempts  int64
			limit     time.Duration
			want      string
			count     int64
		}{{"retry-success", 2, "", 4, time.Minute, "success", 3}, {"attempt-limit", 9, "", 2, time.Minute, "permanent", 2}, {"permanent", 0, "schema_invalid", 3, time.Minute, "permanent", 1}, {"deadline", 9, "", 4, time.Second, "expired", 1}} {
			t.Run(test.name, func(t *testing.T) {
				store := factory(t)
				ctx := contextFor(t)
				h := hostFor(store, owner, principal)
				h.ScheduleControl = true
				clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
				h.Clock = clock
				policy := demo.DefaultPolicy()
				policy.TransientFailures = test.failures
				policy.PermanentReason = test.permanent
				policy.MaxAttempts = test.attempts
				policy.ExecutionLimit = test.limit
				policy.BaseBackoff = time.Second
				policy.MaxBackoff = 2 * time.Second
				policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
				if err != nil {
					t.Fatal(err)
				}
				h.Policies = policies
				out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
				original := assertReceived(t, out, err)
				worker := scheduledWorker(t, store, clock)
				first, err := worker.Step(ctx, "first", 1, time.Minute, time.Second)
				if err != nil || first.Processed != 1 {
					t.Fatalf("first: %+v %v", first, err)
				}
				before, err := h.ObserveSchedule(ctx, "input", 1, &principal)
				if err != nil {
					t.Fatal(err)
				}
				if before.State.Outcome == "retry" && !before.State.Due.Equal(clock.Time().Add(time.Second)) {
					t.Fatalf("first backoff: %+v", before)
				}
				// Use a fresh real connection/Host (SQLite closes its exclusive writer).
				next := reopenAdmissionStore(t, store).(waitStore)
				h = hostFor(next, owner, principal)
				h.Clock = clock
				worker = scheduledWorker(t, next, clock)
				after, err := h.ObserveSchedule(ctx, "input", 1, &principal)
				if err != nil || after.State.Attempts != before.State.Attempts || !after.State.Deadline.Equal(before.State.Deadline) || !after.State.Due.Equal(before.State.Due) {
					t.Fatalf("reopen: %+v %v", after, err)
				}
				for n := 0; n < 8 && (after.State.Outcome == "retry" || after.State.Outcome == "waiting"); n++ {
					early, err := worker.Step(ctx, "early", 1, time.Minute, time.Second)
					if err != nil || early.Processed != 0 || !early.NextWake.After(clock.Time()) {
						t.Fatalf("early: %+v %v", early, err)
					}
					clock.Advance(after.State.Due.Sub(clock.Time()))
					if _, err = worker.Step(ctx, "next", 1, time.Minute, time.Second); err != nil {
						t.Fatal(err)
					}
					after, err = h.ObserveSchedule(ctx, "input", 1, &principal)
					if err != nil {
						t.Fatal(err)
					}
				}
				got, err := h.Observe(ctx, "input", &principal)
				if err != nil || got.Job.CompletedRevision != 1 || got.Job.State != "done" || after.State.Outcome != test.want || after.State.Attempts != test.count || after.ActiveClaim {
					t.Fatalf("closed: %+v %+v %v", got, after, err)
				}
				if test.want == "success" {
					requireHello(t, h, "input", 1)
				} else if got.Projection != nil {
					t.Fatal("failure fabricated successful digest")
				}
				query, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "source"}), &principal, h.Permissions, h, time.Now)
				if err != nil {
					t.Fatal(err)
				}
				found, ok := query.AsFound()
				if !ok {
					t.Fatal("missing original")
				}
				assertReceiptSame(t, original, found.Receipt)
			})
		}
	})
	t.Run("RunnerParksAtFutureTimerAndCancels", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		h.ScheduleControl = true
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.Gate = &demo.GateCondition{ID: "gate", Revision: 1}
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		timer := &waitTimer{clock: clock, registered: make(chan time.Time), tick: make(chan struct{})}
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- worker.Run(runCtx, "runner", 16, time.Minute, time.Second, timer) }()
		select {
		case until := <-timer.registered:
			if !until.Equal(clock.Time().Add(time.Second)) {
				t.Fatalf("finite timer: %v", until)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		out, err = h.Record(ctx, command("healthy", "healthy", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		if err = h.AdvanceGate(ctx, "gate", 1); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Second)
		timer.tick <- struct{}{}
		select {
		case <-timer.registered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		requireHello(t, h, "input", 1)
		requireHello(t, h, "healthy", 1)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
	t.Run("StopOldRevisionPreservesNewWorkAndSuccess", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		h.ScheduleControl = true
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.PermanentReason = "precondition_failed"
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err := h.Record(ctx, command("first", "input", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		batch, err := worker.Claim(ctx, "old", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		old := batch[0]
		h.Policies = nil
		revision := contract.Revision("1")
		out, err = h.Record(ctx, command("new", "input", "hello", &revision, future()), &principal)
		assertReceived(t, out, err)
		if err = h.Stop(ctx, "input", 1); err != nil {
			t.Fatal(err)
		}
		if err = h.Stop(ctx, "input", 1); err != nil {
			t.Fatal(err)
		}
		if err = worker.Process(ctx, old); err == nil {
			t.Fatal("stopped old claim processed")
		}
		got, err := h.Observe(ctx, "input", &principal)
		if err != nil || got.Job.CompletedRevision != 1 || got.Job.WorkRevision != 2 || got.Job.State != "ready" || got.Projection != nil {
			t.Fatalf("new responsibility: %+v %v", got, err)
		}
		result, err := worker.Step(ctx, "new", 1, time.Minute, time.Second)
		if err != nil || result.Processed != 1 {
			t.Fatalf("new step: %+v %v", result, err)
		}
		requireHello(t, h, "input", 2)
		if err = h.Stop(ctx, "input", 2); err != nil {
			t.Fatal(err)
		}
		state, err := h.ObserveSchedule(ctx, "input", 2, &principal)
		if err != nil || state.State.Outcome != "success" {
			t.Fatalf("success preserved: %+v %v", state, err)
		}
	})
	t.Run("GateWaitReleasesResourcesAndDroppedNotificationsRecover", func(t *testing.T) {
		store := factory(t)
		ctx := contextFor(t)
		h := hostFor(store, owner, principal)
		h.ScheduleControl = true
		clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
		h.Clock = clock
		policy := demo.DefaultPolicy()
		policy.Identity = "wait-v1"
		policy.Gate = &demo.GateCondition{ID: "gate", Revision: 2}
		policies, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "waiting", Policy: policy}})
		if err != nil {
			t.Fatal(err)
		}
		h.Policies = policies
		out, err := h.Record(ctx, command("original", "waiting", "hello", nil, future()), &principal)
		original := assertReceived(t, out, err)
		worker := scheduledWorker(t, store, clock)
		step, err := worker.Step(ctx, "worker", 1, time.Minute, time.Second)
		if err != nil || step.Processed != 1 || !step.NextWake.After(clock.Time()) {
			t.Fatalf("wait step: %+v %v", step, err)
		}
		got, err := h.ObserveSchedule(ctx, "waiting", 1, &principal)
		if err != nil || got.State.Outcome != "waiting" || got.ActiveClaim || got.State.Attempts != 0 || got.State.Policy.Gate.ID != "gate" {
			t.Fatalf("durable wait: %+v %v", got, err)
		}
		out, err = h.Record(ctx, command("healthy", "healthy", "hello", nil, future()), &principal)
		assertReceived(t, out, err)
		normal, err := worker.Step(ctx, "worker", 1, time.Minute, time.Second)
		if err != nil || normal.Processed != 1 {
			t.Fatalf("healthy: %+v %v", normal, err)
		}
		requireHello(t, h, "healthy", 1)
		if err = h.AdvanceGate(ctx, "gate", 2); err != nil {
			t.Fatal(err)
		}
		early, err := worker.Step(ctx, "worker", 1, time.Minute, time.Second)
		if err != nil || early.Processed != 0 || !early.NextWake.After(clock.Time()) {
			t.Fatalf("future due: %+v %v", early, err)
		}
		clock.Advance(policy.Recheck)
		done, err := worker.Step(ctx, "worker", 1, time.Minute, time.Second)
		if err != nil || done.Processed != 1 {
			t.Fatalf("lost notify: %+v %v", done, err)
		}
		requireHello(t, h, "waiting", 1)
		query, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "original"}), &principal, h.Permissions, h, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := query.AsFound()
		if !ok {
			t.Fatal("original absent")
		}
		assertReceiptSame(t, original, found.Receipt)
	})
}

func requireHello(t *testing.T, h *durablework.Host, id contract.ID, revision int64) {
	t.Helper()
	got, err := h.Observe(contextFor(t), id, &principal)
	if err != nil || got.Projection == nil || got.Projection.InputRevision != revision || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("actual hello projection: %+v %v", got, err)
	}
}

// Test time is one shared owner clock. The registered future wait is the
// observable timer seam, independent of SQL/function call counts.
type waitTimer struct {
	clock      *workClock
	registered chan time.Time
	tick       chan struct{}
}

func (c *waitTimer) Wait(ctx context.Context, delay time.Duration) error {
	until := c.clock.Time().Add(delay)
	select {
	case c.registered <- until:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-c.tick:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func scheduledWorker(t *testing.T, store waitStore, clock runtime.Clock) *durablework.Worker {
	t.Helper()
	permissions, err := demo.NewWorkerPermissions([]string{"worker", "new", "first", "next", "early", "old", "runner", "second"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := durablework.NewScheduledWorker(owner, store, store, store, clock, permissions)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func TestLegacyFirstAdoptionUsesCurrentTrustedTime(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			store := restoreWaitLegacy(t, backend)
			ctx := contextFor(t)
			scope := contract.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-owner"}
			subject := contract.SubjectBinding{TenantID: scope.TenantID, SubjectID: "fixture-writer", DelegationChain: []contract.DelegatedSubject{}}
			h := hostFor(store, scope, subject)
			clock := &workClock{now: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
			h.Clock = clock
			before, err := h.Observe(ctx, "v1-input", &subject)
			if err != nil || before.Job.State != "ready" {
				t.Fatalf("historical pending: %+v %v", before, err)
			}
			permissions, err := demo.NewWorkerPermissions([]string{"first", "next"})
			if err != nil {
				t.Fatal(err)
			}
			worker, err := durablework.NewScheduledWorker(scope, store, store, store, clock, permissions)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := worker.Claim(ctx, "first", 1, time.Millisecond)
			if err != nil || len(batch) != 1 || batch[0].Claim.JobID != before.Job.ID {
				t.Fatalf("original responsibility: %+v %v", batch, err)
			}
			state, err := h.ObserveSchedule(ctx, "v1-input", 1, &subject)
			if err != nil || state.State.Source != "legacy-adoption" || !state.State.AdoptedAt.Equal(clock.Time()) || !state.State.Deadline.Equal(clock.Time().Add(5*time.Minute)) || state.State.Attempts != 0 || state.State.Policy.TransientFailures != 0 || state.State.Policy.Gate != nil {
				t.Fatalf("first adoption: %+v %v", state, err)
			}
			// Normal source is old enough that its command acceptance deadline cannot
			// serve as this new execution budget. Its original bytes really project.
			if err = worker.Process(ctx, batch[0]); err != nil {
				t.Fatal(err)
			}
			after, err := h.Observe(ctx, "v1-input", &subject)
			if err != nil || after.Job.ID != before.Job.ID || after.Job.State != "done" || after.Projection == nil || after.Projection.TextDigest != "sha256:eeebf3ebdb81d9e669ca989199225a42df8bfe152a9e247c1b5981052f615bbc" {
				t.Fatalf("legacy actual projection: %+v %v", after, err)
			}
			query, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: scope, CommandID: "applied-original"}), &subject, h.Permissions, h, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := query.AsFound()
			if !ok {
				t.Fatal("original absent")
			}
			if _, ok = found.Receipt.AsApplied(); !ok {
				t.Fatal("original decision changed")
			}
		})
	}
}
func restoreWaitLegacy(t *testing.T, backend string) waitStore {
	t.Helper()
	if backend == "postgres" {
		cfg := postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
		var nonce [12]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			t.Fatal(err)
		}
		cfg.Schema = "lerna_test_" + hex.EncodeToString(nonce[:])
		store, err := postgres.Open(contextFor(t), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.CreateSchema(contextFor(t)); err != nil {
			store.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.DropTestSchema(ctx); err != nil {
				t.Error(err)
			}
			store.Close()
		})
		data, err := os.ReadFile(filepath.Join("..", "fixtures", "durable-work", "pg-v1", "database.sql"))
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.ReplaceAll(data, []byte("lerna_test_000000000000000000000001"), []byte(cfg.Schema))
		data = bytes.Replace(data, []byte("CREATE SCHEMA "+cfg.Schema+";"), nil, 1)
		if err = restoreWaitPGDump(contextFor(t), cfg.DSN, data); err != nil {
			t.Fatal("historical fixture restore failed", err)
		}
		if err = store.Migrate(contextFor(t)); err != nil {
			t.Fatal(err)
		}
		return store
	}
	data, err := os.ReadFile(filepath.Join("..", "fixtures", "durable-work", "sqlite-v1", "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := sqlite.Config{Path: filepath.Join(t.TempDir(), "legacy.sqlite"), TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond}
	if err = os.WriteFile(cfg.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestLegacyV2ClaimWaitsForOriginalLeaseBeforeAdopting(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			ctx := contextFor(t)
			var store waitStore
			var cfg postgres.Config
			var path string
			if backend == "postgres" {
				var nonce [12]byte
				if _, err := rand.Read(nonce[:]); err != nil {
					t.Fatal(err)
				}
				cfg = postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
				pg, err := postgres.Open(ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = pg.CreateSchema(ctx); err != nil {
					pg.Close()
					t.Fatal(err)
				}
				store = pg
				t.Cleanup(func() {
					clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := pg.DropTestSchema(clean); err != nil {
						t.Error(err)
					}
					pg.Close()
				})
			} else {
				path = filepath.Join(t.TempDir(), "v2.sqlite")
			}
			fixture := filepath.Join("..", "fixtures", "durable-work", map[string]string{"postgres": "pg-v2", "sqlite": "sqlite-v2"}[backend])
			reportData, err := os.ReadFile(filepath.Join(fixture, "writer-observation.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report struct{ Claim durablework.Work }
			if err = json.Unmarshal(reportData, &report); err != nil {
				t.Fatal(err)
			}
			old := report.Claim
			if backend == "postgres" {
				data, err := os.ReadFile(filepath.Join(fixture, "database.sql"))
				if err != nil {
					t.Fatal(err)
				}
				originalSchema, err := os.ReadFile(filepath.Join(fixture, "writer-schema.txt"))
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.ReplaceAll(data, bytes.TrimSpace(originalSchema), []byte(cfg.Schema))
				data = bytes.Replace(data, []byte("CREATE SCHEMA "+cfg.Schema+";"), nil, 1)
				if err = restoreWaitPGDump(ctx, cfg.DSN, data); err != nil {
					t.Fatal(err)
				}
			} else {
				data, err := os.ReadFile(filepath.Join(fixture, "database.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				sq, err := sqlite.Open(ctx, sqlite.Config{Path: path, TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond})
				if err != nil {
					t.Fatal(err)
				}
				store = sq
				t.Cleanup(func() { sq.Close() })
			}
			if migrator, ok := store.(interface{ Migrate(context.Context) error }); !ok {
				t.Fatal("missing migration")
			} else if err = migrator.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			clock := &workClock{now: time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)}
			h := hostFor(store, owner, principal)
			h.Clock = clock
			worker := scheduledWorker(t, store, clock)
			before, err := h.Observe(ctx, "input", &principal)
			if err != nil || before.Job.State != "leased" || before.Job.WorkRevision != 2 || before.Job.CompletedRevision != 0 || before.Job.ID != old.Claim.JobID {
				t.Fatalf("v2 relationship preserved: %+v %v", before, err)
			}
			live, err := worker.Claim(ctx, "next", 1, time.Minute)
			if err != nil || len(live) != 0 {
				t.Fatalf("v2 original lease bypassed: %+v %v", live, err)
			}
			if _, err = h.ObserveSchedule(ctx, "input", 2, &principal); !errors.Is(err, demo.ErrPolicy) {
				t.Fatalf("premature adoption: %v", err)
			}
			clock.Advance(time.Second)
			batch, err := worker.Claim(ctx, "next", 1, time.Minute)
			if err != nil || len(batch) != 1 || batch[0].Claim.JobID != old.Claim.JobID || batch[0].Claim.Epoch != old.Claim.Epoch+1 || batch[0].Claim.ClaimedRevision != 2 {
				t.Fatalf("v2 takeover: %+v %v", batch, err)
			}
			state, err := h.ObserveSchedule(ctx, "input", 2, &principal)
			if err != nil || !state.State.AdoptedAt.Equal(clock.Time()) || !state.State.Deadline.Equal(clock.Time().Add(5*time.Minute)) || state.State.Source != "legacy-adoption" {
				t.Fatalf("v2 adoption anchor: %+v %v", state, err)
			}
			if err = worker.Complete(ctx, old.Claim, durablework.Project(old)); !errors.Is(err, runtime.ErrClaim) {
				t.Fatalf("legacy late message accepted: %v", err)
			}
			if err = worker.Process(ctx, batch[0]); err != nil {
				t.Fatal(err)
			}
			requireHello(t, h, "input", 2)
		})
	}
}

// Convert the immutable psql COPY transport into the driver's native COPY.
// Facts and bytes are unchanged; client restrict directives have no SQL meaning.
func restoreWaitPGDump(ctx context.Context, dsn string, data []byte) error {
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return errors.New("legacy restore connection unavailable")
	}
	defer connection.Close(ctx)
	lines := strings.Split(string(data), "\n")
	var sqlText strings.Builder
	flush := func() error {
		if strings.TrimSpace(sqlText.String()) == "" {
			return nil
		}
		_, err := connection.Exec(ctx, sqlText.String())
		sqlText.Reset()
		return err
	}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if strings.HasPrefix(line, "\\restrict ") || strings.HasPrefix(line, "\\unrestrict ") {
			continue
		}
		if strings.HasPrefix(line, "COPY ") && strings.HasSuffix(line, " FROM stdin;") {
			if err = flush(); err != nil {
				return err
			}
			var input strings.Builder
			terminated := false
			for index++; index < len(lines); index++ {
				if lines[index] == "\\." {
					terminated = true
					break
				}
				input.WriteString(lines[index])
				input.WriteByte('\n')
			}
			if !terminated {
				return errors.New("historical COPY truncated")
			}
			if _, err = connection.PgConn().CopyFrom(ctx, strings.NewReader(input.String()), line); err != nil {
				return err
			}
		} else {
			sqlText.WriteString(line)
			sqlText.WriteByte('\n')
		}
	}
	return flush()
}

// Fault at the real Tx system boundary: the database commits Start before
// confirmation is lost or process context is cancelled. Later Txs are real.
type startConfirmationFault struct {
	runner runtime.TxRunner
	used   atomic.Bool
	cancel context.CancelFunc
}

func (f *startConfirmationFault) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	if err := f.runner.Within(ctx, owner, fn); err != nil {
		return err
	}
	if f.used.CompareAndSwap(false, true) {
		if f.cancel != nil {
			f.cancel()
			return context.Canceled
		}
		return runtime.ErrCommitUnknown
	}
	return nil
}
