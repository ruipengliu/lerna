//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
)

func TestPGPoolRunPreservesRetryWindow(t *testing.T)     { poolRunRetryWindow(t, database(t)) }
func TestSQLitePoolRunPreservesRetryWindow(t *testing.T) { poolRunRetryWindow(t, sqliteDatabase(t)) }
func poolRunRetryWindow(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	other := contract.OwnerRef{TenantID: owner.TenantID, OwnerID: "non-anchor"}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	if err := h.InstallPool(ctx, demo.DefaultPool("retry-window", []contract.OwnerRef{owner, other}), 0); err != nil {
		t.Fatal(err)
	}
	target := rawHostFor(store, other, principal)
	target.Clock = clock
	policy := demo.DefaultPolicy()
	policy.ExecutionLimit = 500 * time.Millisecond
	policy.TransientFailures = 1
	policy.BaseBackoff = 100 * time.Millisecond
	target.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: other, ObjectID: "retry", Policy: policy}})
	var envelope contract.CommandEnvelope
	_ = json.Unmarshal(command("retry-source", "retry", "hello", nil, future()), &envelope)
	envelope.Target.OwnerID = other.OwnerID
	wire, _ := json.Marshal(envelope)
	original := assertReceivedResult(t, target, ctx, wire)
	assertReceivedResult(t, h, ctx, command("normal-source", "normal", "hello", nil, future()))
	first := conformanceWorker(t, owner, store, store, store, clock)
	second := conformanceWorker(t, other, store, store, store, clock)
	pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{first, second})
	if err != nil {
		t.Fatal(err)
	}
	timer := &poolTimer{clock: clock, registered: make(chan poolWait, 3)}
	pool.Timer = timer
	running, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { finished <- pool.Run(running, "worker-a", time.Minute, time.Second) }()
	defer func() {
		cancel()
		if err := <-finished; !errors.Is(err, context.Canceled) {
			t.Errorf("Run lifecycle: %v", err)
		}
	}()
	var retry poolWait
	for range 3 {
		select {
		case wait := <-timer.registered:
			if wait.until.Equal(clock.Time().Add(100 * time.Millisecond)) {
				retry = wait
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if retry.release == nil {
		t.Fatal("Run missed legal retry wake before 500ms deadline")
	}
	clock.Advance(100 * time.Millisecond)
	close(retry.release)
	select {
	case <-timer.registered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		got, err := target.ObserveSchedule(ctx, "retry", 1, &principal)
		if err != nil {
			t.Fatal(err)
		}
		if got.State.Outcome == "expired" {
			t.Fatalf("Run missed legal retry window: %+v", got.State)
		}
		if got.State.Outcome == "success" {
			if got.State.Attempts != 2 {
				t.Fatalf("retry starts: %+v", got.State)
			}
			projected, err := target.Observe(ctx, "retry", &principal)
			if err != nil || projected.Projection == nil || projected.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
				t.Fatalf("retry projection: %+v %v", projected, err)
			}
			normal, err := h.Observe(ctx, "normal", &principal)
			if err != nil || normal.Projection == nil {
				t.Fatalf("normal control: %+v %v", normal, err)
			}
			replay, err := target.Record(ctx, wire, &principal)
			assertReceiptSame(t, original, assertReceived(t, replay, err))
			return
		}
		t.Fatalf("Run parked before second Start: %+v", got.State)
	}
}

// The external timer exposes actual future waits; each parked lane has its own
// release, while all lanes/owners use the same trusted clock.
type poolWait struct {
	until   time.Time
	release chan struct{}
}
type poolTimer struct {
	clock      *workClock
	registered chan poolWait
}

func (timer *poolTimer) Wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 || delay > time.Second {
		return errors.New("nonfinite pool wait")
	}
	wait := poolWait{timer.clock.Time().Add(delay), make(chan struct{})}
	select {
	case timer.registered <- wait:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-wait.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPGPoolRunCurrentRevisionDeadlineAndZeroQuota(t *testing.T) { poolRunDeadline(t, database(t)) }
func TestSQLitePoolRunCurrentRevisionDeadlineAndZeroQuota(t *testing.T) {
	poolRunDeadline(t, sqliteDatabase(t))
}
func poolRunDeadline(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	other := contract.OwnerRef{TenantID: "tenant-two", OwnerID: "non-anchor"}
	subject := principal
	subject.TenantID = other.TenantID
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	target := rawHostFor(store, other, subject)
	target.Clock = clock
	cfg := demo.DefaultPool("deadline-wake", []contract.OwnerRef{owner, other})
	cfg.Limits[0].Concurrent = 1
	cfg.Quotas[0].Concurrent = 1
	cfg.Quotas[3].Concurrent = 1
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	record := func(id string, revision *contract.Revision) []byte {
		var envelope contract.CommandEnvelope
		_ = json.Unmarshal(command(id, "input", "hello", revision, future()), &envelope)
		envelope.Target.TenantID = other.TenantID
		envelope.Target.OwnerID = other.OwnerID
		data, _ := json.Marshal(envelope)
		out, err := target.Record(ctx, data, &subject)
		assertReceived(t, out, err)
		return data
	}
	policy := demo.DefaultPolicy()
	policy.ExecutionLimit = 2 * time.Second
	target.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: other, ObjectID: "input", Policy: policy}})
	record("old", nil)
	first := conformanceWorker(t, owner, store, store, store, clock)
	second := conformanceWorker(t, other, store, store, store, clock)
	batch, err := second.Claim(ctx, "worker-a", 1, 800*time.Millisecond)
	if err != nil || len(batch) != 1 {
		t.Fatalf("real old Claim: %+v %v", batch, err)
	}
	startWork(t, second, batch[0])
	pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{first, second})
	if err != nil {
		t.Fatal(err)
	}
	wake, err := pool.NextWake(ctx, "ordinary", time.Second)
	if err != nil || !wake.NextWake.Equal(clock.Time().Add(800*time.Millisecond)) {
		t.Fatalf("live lease boundary: %+v %v", wake, err)
	}
	clock.Advance(100 * time.Millisecond)
	policy.ExecutionLimit = 200 * time.Millisecond
	target.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: other, ObjectID: "input", Policy: policy}})
	revision := contract.Revision("1")
	record("new", &revision)
	deadline := clock.Time().Add(200 * time.Millisecond)
	wake, err = pool.NextWake(ctx, "ordinary", time.Second)
	if err != nil || !wake.NextWake.Equal(deadline) {
		t.Fatalf("new deadline masked by live old Claim: %+v %v", wake, err)
	}
	timer := &poolTimer{clock: clock, registered: make(chan poolWait, 3)}
	pool.Timer = timer
	running, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- pool.Run(running, "worker-b", time.Minute, time.Second) }()
	joined := false
	defer func() {
		cancel()
		if joined {
			return
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run lifecycle: %v", err)
		}
	}()
	var short poolWait
	for range 3 {
		select {
		case wait := <-timer.registered:
			if wait.until.Equal(deadline) {
				short = wait
			} else if !wait.until.Equal(clock.Time().Add(time.Second)) {
				t.Fatalf("unexpected lane wait: %v", wait.until)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if short.release == nil {
		t.Fatal("ordinary lane did not park at current revision deadline")
	}
	clock.Advance(200 * time.Millisecond)
	close(short.release)
	select {
	case <-timer.registered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	newer, err := target.ObserveSchedule(ctx, "input", 2, &subject)
	older, olderErr := target.ObserveSchedule(ctx, "input", 1, &subject)
	if err != nil || newer.State.Outcome != "expired" || newer.State.Attempts != 0 || olderErr != nil || older.State.Outcome != "running" || !older.ActiveClaim {
		t.Fatalf("current closure damaged live old revision: new %+v %v old %+v %v", newer, err, older, olderErr)
	}
	poolState, err := h.ObservePool(ctx)
	if err != nil || poolState.Active["ordinary"] != 1 || poolState.Queued["ordinary"] != 1 || newer.Job.State == "done" {
		t.Fatalf("new closure released live old Claim/queue: %+v %v", poolState, err)
	}
	sequence := poolState.State.Cursors["ordinary"].Sequence
	cancel()
	// Independent no-quota responsibility must still wake at its original
	// deadline; already-due work waits positively instead of consuming starts.
	// Stop the runner before changing its timer/configuration.
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	joined = true
	if err := second.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	cfg.Quotas[3].Concurrent = 0
	if err := h.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	revision = contract.Revision("2")
	record("zero", &revision)
	wake, err = pool.NextWake(ctx, "ordinary", time.Second)
	if err != nil || !wake.NextWake.Equal(clock.Time().Add(200*time.Millisecond)) {
		t.Fatalf("zero quota deadline: %+v %v", wake, err)
	}
	processed, err := pool.StepLane(ctx, "ordinary", "worker-b", time.Minute)
	if err != nil || processed {
		t.Fatalf("zero quota allocated: %v %v", processed, err)
	}
	// Before expiry the due job cannot make progress, but parks in the future.
	if wake.WaitFor <= 0 {
		t.Fatal("due zero quota responsibility busy loops")
	}
	timer = &poolTimer{clock: clock, registered: make(chan poolWait, 3)}
	pool.Timer = timer
	running, cancel = context.WithCancel(ctx)
	joined = false
	go func() { done <- pool.Run(running, "worker-b", time.Minute, time.Second) }()
	short = poolWait{}
	zeroDeadline := clock.Time().Add(200 * time.Millisecond)
	for range 3 {
		select {
		case wait := <-timer.registered:
			if wait.until.Equal(zeroDeadline) {
				short = wait
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if short.release == nil {
		t.Fatal("zero quota Run did not park at deadline")
	}
	clock.Advance(200 * time.Millisecond)
	close(short.release)
	select {
	case short = <-timer.registered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for opportunity := 0; opportunity < 2; opportunity++ {
		observed, err := target.ObserveSchedule(ctx, "input", 3, &subject)
		if err != nil {
			t.Fatal(err)
		}
		if observed.State.Outcome == "expired" {
			break
		}
		clock.Advance(time.Second)
		close(short.release)
		select {
		case short = <-timer.registered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	closed, err := target.ObserveSchedule(ctx, "input", 3, &subject)
	if err != nil || closed.State.Outcome != "expired" || closed.State.Attempts != 0 || closed.Job.State != "done" {
		t.Fatalf("no quota maintenance: %+v %v", closed, err)
	}
	poolState, err = h.ObservePool(ctx)
	if err != nil || poolState.State.Cursors["ordinary"].Sequence != sequence || poolState.Active["ordinary"] != 0 || poolState.Queued["ordinary"] != 0 {
		t.Fatalf("maintenance allocated execution or retained queue: %+v %v", poolState, err)
	}
	wake, err = pool.NextWake(ctx, "ordinary", time.Second)
	if err != nil || wake.WaitFor != time.Second {
		t.Fatalf("empty positive fallback: %+v %v", wake, err)
	}
}

func TestPGPoolWakeKeepsEarlierClaimedDeadline(t *testing.T) { poolClaimedDeadline(t, database(t)) }
func TestSQLitePoolWakeKeepsEarlierClaimedDeadline(t *testing.T) {
	poolClaimedDeadline(t, sqliteDatabase(t))
}
func poolClaimedDeadline(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	if err := h.InstallPool(ctx, demo.DefaultPool("claimed-deadline", []contract.OwnerRef{owner}), 0); err != nil {
		t.Fatal(err)
	}
	policy := demo.DefaultPolicy()
	policy.ExecutionLimit = 400 * time.Millisecond
	h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
	assertReceivedResult(t, h, ctx, command("old", "input", "hello", nil, future()))
	worker := conformanceWorker(t, owner, store, store, store, clock)
	batch, err := worker.Claim(ctx, "worker-a", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("real claimed revision: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	policy.ExecutionLimit = 3 * time.Second
	h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
	revision := contract.Revision("1")
	assertReceivedResult(t, h, ctx, command("new", "input", "hello", &revision, future()))
	pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{worker})
	if err != nil {
		t.Fatal(err)
	}
	wake, err := pool.NextWake(ctx, "ordinary", time.Second)
	if err != nil || !wake.NextWake.Equal(clock.Time().Add(400*time.Millisecond)) {
		t.Fatalf("claimed deadline masked by later current/lease/fallback: %+v %v", wake, err)
	}
	old, err := h.ObserveSchedule(ctx, "input", 1, &principal)
	current, currentErr := h.ObserveSchedule(ctx, "input", 2, &principal)
	if err != nil || !old.ActiveClaim || old.State.Outcome != "running" || currentErr != nil || !current.State.Deadline.Equal(clock.Time().Add(3*time.Second)) {
		t.Fatalf("actual revision boundaries: old %+v %v new %+v %v", old, err, current, currentErr)
	}
	if err := worker.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	if processed, err := pool.StepLane(ctx, "ordinary", "worker-b", time.Minute); err != nil || !processed {
		t.Fatalf("normal newer progress: %v %v", processed, err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Projection == nil || got.Projection.InputRevision != 2 || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("normal latest projection: %+v %v", got, err)
	}
}
