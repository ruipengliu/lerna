//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"math"
	"sync"
	"testing"
	"time"
)

type workStore interface {
	admissionStore
	runtime.ClaimStore
	demo.WorkRepository
}

func TestPGSharedWorkBehaviors(t *testing.T) {
	runWorkBehaviors(t, func(t *testing.T) workStore { return database(t) })
}
func TestSQLiteSharedWorkBehaviors(t *testing.T) {
	runWorkBehaviors(t, func(t *testing.T) workStore { return sqliteDatabase(t) })
}
func runWorkBehaviors(t *testing.T, newStore func(*testing.T) workStore) {
	t.Run("ScanClaimsSnapshotAndProjectsOutsideTransaction", func(t *testing.T) { behaviorScanClaimsSnapshotAndProjectsOutsideTransaction(t, newStore) })
	t.Run("OldCompletionPreservesNewRevisionAndExactSnapshot", func(t *testing.T) { behaviorOldCompletionPreservesNewRevisionAndExactSnapshot(t, newStore) })

	t.Run("RenewBindsClaimAndExpiresWithoutReplacement", func(t *testing.T) { behaviorRenewBindsClaimAndExpiresWithoutReplacement(t, newStore) })
	t.Run("TriggerRejectsRegressingAndOutOfRangeRevisions", func(t *testing.T) { behaviorTriggerRejectsRegressingAndOutOfRangeRevisions(t, newStore) })
	t.Run("ConcurrentNewWorkAndCompletionBothCommitOrders", func(t *testing.T) { behaviorConcurrentNewWorkAndCompletionBothCommitOrders(t, newStore) })
	t.Run("ClaimRequestBoundsAndFailedProjectionRollBackCompletion", func(t *testing.T) { behaviorClaimRequestBoundsAndFailedProjectionRollBackCompletion(t, newStore) })
	t.Run("MaximumRevisionClaimsAndCompletesWithoutOverflow", func(t *testing.T) { behaviorMaximumRevisionClaimsAndCompletesWithoutOverflow(t, newStore) })
	t.Run("LeaseExpiryIsCheckedAfterWaitingForObjectLock", func(t *testing.T) { behaviorLeaseExpiryIsCheckedAfterWaitingForObjectLock(t, newStore) })
	t.Run("ConcurrentWorkersClaimOneOriginalJobAndCompleteNormally", func(t *testing.T) { behaviorConcurrentWorkersClaimOneOriginalJobAndCompleteNormally(t, newStore) })
	t.Run("CloseReopenRetainsClaimAndOriginalReceipt", func(t *testing.T) { behaviorCloseReopenRetainsClaimAndOriginalReceipt(t, newStore) })
	t.Run("ExactOwnerTimeBoundaries", func(t *testing.T) { behaviorExactOwnerTimeBoundaries(t, newStore) })
	t.Run("ClaimStorageScopeAndBounds", func(t *testing.T) { behaviorClaimStorageScopeAndBounds(t, newStore) })
	t.Run("BoundedBatchRetainsEveryOriginalResponsibility", func(t *testing.T) { behaviorBoundedBatchRetainsEveryOriginalResponsibility(t, newStore) })
}
func behaviorScanClaimsSnapshotAndProjectsOutsideTransaction(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("claim-source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	worker := durablework.NewWorker(owner, store, store, store, store)
	batch, err := worker.Claim(ctx, "worker-a", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	work := batch[0]
	if work.Input.Text != "hello" || work.Input.Revision != 1 || work.Claim.ClaimedRevision != 1 || work.Claim.Epoch != 1 || work.Claim.Worker != "worker-a" {
		t.Fatalf("snapshot: %+v", work)
	}
	result := durablework.Project(work)
	if result.InputRevision != 1 || result.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("projection: %+v", result)
	}
	if err = worker.Complete(ctx, work.Claim, result); err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.CompletedRevision != 1 || got.Job.State != "done" || got.Projection == nil || *got.Projection != result {
		t.Fatalf("projected: %+v %v", got, err)
	}
	batch, err = worker.Claim(ctx, "worker-b", 1, time.Minute)
	if err != nil || len(batch) != 0 {
		t.Fatalf("done re-claimed: %+v %v", batch, err)
	}
}

func behaviorOldCompletionPreservesNewRevisionAndExactSnapshot(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	worker := durablework.NewWorker(owner, store, store, store, store)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("original", "input", "hello", nil, future()), &principal)
	original := assertReceived(t, out, err)
	batch, err := worker.Claim(ctx, "first", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	old := batch[0]
	revision := contract.Revision("1")
	out, err = h.Record(ctx, command("new-work", "input", "你好🌍\x00", &revision, future()), &principal)
	assertReceived(t, out, err)
	if old.Input.Text != "hello" || old.Input.Revision != 1 {
		t.Fatalf("snapshot changed: %+v", old)
	}
	if err = worker.Complete(ctx, old.Claim, durablework.Project(old)); err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 2 || got.Job.WorkRevision != 2 || got.Job.CompletedRevision != 1 || got.Job.State != "ready" || got.Projection.InputRevision != 1 {
		t.Fatalf("new revision lost: %+v %v", got, err)
	}
	next, err := worker.Claim(ctx, "second", 1, time.Minute)
	if err != nil || len(next) != 1 || next[0].Claim.JobID != old.Claim.JobID || next[0].Claim.Object != old.Claim.Object || next[0].Claim.ClaimedRevision != 2 || next[0].Input.Text != "你好🌍\x00" {
		t.Fatalf("new snapshot/identity: %+v %v", next, err)
	}
	if err = worker.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
		t.Fatal(err)
	}
	got, err = h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 2 || got.Job.CompletedRevision != 2 || got.Job.State != "done" || got.Projection.InputRevision != 2 || got.Projection.TextDigest != "sha256:3b60b92cff063c389fc62e0f02e99889ea22226852f21b211d522357538793ea" {
		t.Fatalf("latest projection: %+v %v", got, err)
	}
	queried, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("original missing")
	}
	assertReceiptSame(t, original, found.Receipt)
}

type workClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *workClock) Now(context.Context, runtime.Tx) (time.Time, error) { return c.Time(), nil }
func (c *workClock) Time() time.Time                                    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *workClock) Advance(d time.Duration)                            { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

// transactionGate pauses at the authorized Host Tx seam before commit.
// requested marks a request before the writer queue; started marks an actual
// entered transaction. PostgreSQL can enter a Tx while its object lock is held.
// Every actual write and eligibility decision remains in the real adapter.
type transactionGate struct {
	runner    runtime.TxRunner
	requested chan struct{}
	started   chan struct{}
	ready     chan struct{}
	release   chan struct{}
}

func (g *transactionGate) Within(ctx context.Context, o contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	if g.requested != nil {
		close(g.requested)
	}
	return g.runner.Within(ctx, o, func(ctx context.Context, tx runtime.Tx) error {
		if g.started != nil {
			close(g.started)
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if g.ready != nil {
			close(g.ready)
		}
		if g.release != nil {
			select {
			case <-g.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
}
func awaitStage(t *testing.T, ctx context.Context, stage <-chan struct{}) {
	t.Helper()
	select {
	case <-stage:
	case <-ctx.Done():
		t.Fatal("finite synchronization deadline", ctx.Err())
	}
}
func behaviorRenewBindsClaimAndExpiresWithoutReplacement(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	clock := &workClock{now: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
	worker := durablework.NewWorker(owner, store, store, store, clock)
	batch, err := worker.Claim(ctx, "first", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	old := batch[0]
	clock.Advance(30 * time.Second)
	renewed, err := worker.Renew(ctx, old.Claim, time.Minute)
	if err != nil || renewed.Epoch != old.Claim.Epoch || renewed.JobID != old.Claim.JobID || !renewed.LeaseUntil.Equal(clock.Time().Add(time.Minute)) {
		t.Fatalf("renew: %+v %v", renewed, err)
	}
	if err = worker.Complete(ctx, old.Claim, durablework.Project(old)); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("superseded lease token accepted: %v", err)
	}
	for _, field := range []string{"worker", "epoch", "revision", "lease", "job", "object", "phase"} {
		forged := renewed
		switch field {
		case "worker":
			forged.Worker = "other"
		case "epoch":
			forged.Epoch++
		case "revision":
			forged.ClaimedRevision++
		case "lease":
			forged.LeaseUntil = forged.LeaseUntil.Add(time.Second)
		case "job":
			forged.JobID = "other"
		case "object":
			forged.Object.ID = "other"
		case "phase":
			forged.Phase = "other"
		}
		result := durablework.Project(old)
		result.InputRevision = forged.ClaimedRevision
		if err = worker.Complete(ctx, forged, result); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("forged %s complete: %v", field, err)
		}
		if _, err = worker.Renew(ctx, forged, time.Minute); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("forged %s renew: %v", field, err)
		}
	}
	clock.Advance(time.Minute - time.Microsecond)
	shadowStore := store
	shadow := durablework.NewWorker(owner, shadowStore, shadowStore, shadowStore, clock)
	live, err := shadow.Claim(ctx, "too-early", 1, time.Minute)
	if err != nil || len(live) != 0 {
		t.Fatalf("reopen lost live lease: %+v %v", live, err)
	}
	clock.Advance(time.Microsecond)
	if err = worker.Complete(ctx, renewed, durablework.Project(old)); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("expired unreplaced claim completed: %v", err)
	}
	if _, err = worker.Renew(ctx, renewed, time.Minute); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("expired unreplaced claim renewed: %v", err)
	}
	replacementStore := store
	replacement := durablework.NewWorker(owner, replacementStore, replacementStore, replacementStore, clock)
	next, err := replacement.Claim(ctx, "second", 1, time.Minute)
	if err != nil || len(next) != 1 || next[0].Claim.Epoch != renewed.Epoch+1 || next[0].Claim.JobID != renewed.JobID || next[0].Claim.Object != renewed.Object {
		t.Fatalf("replacement: %+v %v", next, err)
	}
	if err = worker.Complete(ctx, renewed, durablework.Project(old)); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("replaced claim complete: %v", err)
	}
	if err = replacement.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.CompletedRevision != 1 || got.Projection.InputRevision != 1 {
		t.Fatalf("replacement result: %+v %v", got, err)
	}
}
func behaviorTriggerRejectsRegressingAndOutOfRangeRevisions(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("first", "input", "one", nil, future()), &principal)
	assertReceived(t, out, err)
	rev := contract.Revision("1")
	out, err = h.Record(ctx, command("second", "input", "two", &rev, future()), &principal)
	assertReceived(t, out, err)
	object := contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "input"}
	for _, revision := range []int64{-1, 0, 1, 2} {
		err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			_, err := store.Trigger(ctx, tx, object, "project", revision, time.Now())
			return err
		})
		if !errors.Is(err, runtime.ErrWorkBounds) {
			t.Fatalf("regressing revision %d permitted: %v", revision, err)
		}
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.WorkRevision != 2 || got.Input.Revision != 2 {
		t.Fatalf("revision regressed: %+v %v", got, err)
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	batch, err := worker.Claim(ctx, "normal", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.ClaimedRevision != 2 {
		t.Fatalf("normal claim: %+v %v", batch, err)
	}
	for _, value := range []int64{-1, 0, 9223372036854775807} {
		claim := batch[0].Claim
		claim.Epoch = value
		if err = worker.Complete(ctx, claim, durablework.Project(batch[0])); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("epoch %d permitted: %v", value, err)
		}
		claim = batch[0].Claim
		claim.ClaimedRevision = value
		projection := durablework.Project(batch[0])
		projection.InputRevision = value
		if err = worker.Complete(ctx, claim, projection); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("revision %d permitted: %v", value, err)
		}
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}
func behaviorConcurrentNewWorkAndCompletionBothCommitOrders(t *testing.T, newStore func(*testing.T) workStore) {
	for _, newFirst := range []bool{true, false} {
		name := "completion-first"
		if newFirst {
			name = "new-work-first"
		}
		t.Run(name, func(t *testing.T) {
			store := newStore(t)
			h := hostFor(store, owner, principal)
			ctx := contextFor(t)
			out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
			assertReceived(t, out, err)
			worker := durablework.NewWorker(owner, store, store, store, store)
			batch, err := worker.Claim(ctx, "first", 1, time.Minute)
			if err != nil || len(batch) != 1 {
				t.Fatalf("claim: %+v %v", batch, err)
			}
			old := batch[0]
			gate := &transactionGate{runner: store, ready: make(chan struct{}), release: make(chan struct{})}
			started := make(chan struct{})
			contender := &transactionGate{runner: store, requested: started}
			if newFirst {
				h.Runner = gate
				worker.Runner = contender
			} else {
				worker.Runner = gate
				h.Runner = contender
			}
			recordDone := make(chan error, 1)
			completeDone := make(chan error, 1)
			record := func() {
				rev := contract.Revision("1")
				out, err := h.Record(ctx, command("new", "input", "second", &rev, future()), &principal)
				if err == nil {
					received, ok := out.AsReceived()
					if !ok {
						err = errors.New("unconfirmed new work")
					} else if _, ok = received.Receipt.AsApplied(); !ok {
						err = errors.New("new work rejected")
					}
				}
				recordDone <- err
			}
			complete := func() { completeDone <- worker.Complete(ctx, old.Claim, durablework.Project(old)) }
			if newFirst {
				go record()
			} else {
				go complete()
			}
			awaitStage(t, ctx, gate.ready)
			if newFirst {
				go complete()
			} else {
				go record()
			}
			awaitStage(t, ctx, started)
			close(gate.release)
			if err := <-recordDone; err != nil {
				t.Fatal(err)
			}
			if err := <-completeDone; err != nil {
				t.Fatal(err)
			}
			h.Runner = store
			worker.Runner = store
			got, err := h.Observe(ctx, "input", &principal)
			if err != nil || got.Input.Revision != 2 || got.Job.WorkRevision != 2 || got.Job.CompletedRevision != 1 || got.Job.State != "ready" || got.Job.ID != old.Claim.JobID || got.Projection.InputRevision != 1 {
				t.Fatalf("concurrent work lost: %+v %v", got, err)
			}
			next, err := worker.Claim(ctx, "second", 1, time.Minute)
			if err != nil || len(next) != 1 || next[0].Input.Revision != 2 || next[0].Input.Text != "second" {
				t.Fatalf("latest work: %+v %v", next, err)
			}
			if err = worker.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
				t.Fatal(err)
			}
			got, err = h.Observe(ctx, "input", &principal)
			if err != nil || got.Job.CompletedRevision != 2 || got.Job.State != "done" || got.Projection.InputRevision != 2 {
				t.Fatalf("normal new completion: %+v %v", got, err)
			}
		})
	}
}
func behaviorClaimRequestBoundsAndFailedProjectionRollBackCompletion(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	worker := durablework.NewWorker(owner, store, store, store, store)
	for _, limit := range []int{-1, 0, 65} {
		if _, err = worker.Claim(ctx, "bounded", limit, time.Minute); !errors.Is(err, runtime.ErrWorkBounds) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	for _, lease := range []time.Duration{-1, 0, time.Microsecond, 6 * time.Minute} {
		if _, err = worker.Claim(ctx, "bounded", 1, lease); !errors.Is(err, runtime.ErrWorkBounds) {
			t.Fatalf("lease %s: %v", lease, err)
		}
	}
	if _, err = worker.Claim(context.Background(), "bounded", 1, time.Minute); !errors.Is(err, runtime.ErrWorkBounds) {
		t.Fatalf("infinite context: %v", err)
	}
	if _, err = worker.Claim(nil, "bounded", 1, time.Minute); !errors.Is(err, runtime.ErrWorkBounds) {
		t.Fatalf("nil context: %v", err)
	}
	batch, err := worker.Claim(ctx, "bounded", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("normal claim: %+v %v", batch, err)
	}
	bad := durablework.Project(batch[0])
	bad.TextDigest = "sha256:invalid"
	if err = worker.Complete(ctx, batch[0].Claim, bad); err == nil {
		t.Fatal("invalid projection committed")
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.CompletedRevision != 0 || got.Job.State != "leased" || got.Projection != nil {
		t.Fatalf("partial completion visible: %+v %v", got, err)
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}
func behaviorMaximumRevisionClaimsAndCompletesWithoutOverflow(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		if _, err := store.LockInput(ctx, tx, owner, "maximum"); err != nil {
			return err
		}
		now, err := store.Now(ctx, tx)
		if err != nil {
			return err
		}
		input := demo.Input{ID: "maximum", Revision: math.MaxInt64, Text: "hello", CreatedAt: now, UpdatedAt: now}
		if err = store.SaveInput(ctx, tx, owner, input); err != nil {
			return err
		}
		_, err = store.Trigger(ctx, tx, contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "maximum"}, "project", math.MaxInt64, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	batch, err := worker.Claim(ctx, "maximum", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.ClaimedRevision != math.MaxInt64 {
		t.Fatalf("maximum claim: %+v %v", batch, err)
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	rev := contract.Revision("9223372036854775807")
	out, err := h.Record(ctx, command("overflow", "maximum", "changed", &rev, future()), &principal)
	receipt := assertReceived(t, out, err)
	rejected, ok := receipt.AsRejected()
	if !ok || rejected.Reason != "unsupported" {
		t.Fatalf("overflow was admitted: %+v", receipt)
	}
	got, err := h.Observe(ctx, "maximum", &principal)
	if err != nil || got.Input.Revision != math.MaxInt64 || got.Job.CompletedRevision != math.MaxInt64 || got.Projection.InputRevision != math.MaxInt64 || got.Job.State != "done" {
		t.Fatalf("maximum progress regressed: %+v %v", got, err)
	}
}
func behaviorLeaseExpiryIsCheckedAfterWaitingForObjectLock(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	clock := &workClock{now: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
	worker := durablework.NewWorker(owner, store, store, store, clock)
	batch, err := worker.Claim(ctx, "old", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	ready := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := store.LockInput(ctx, tx, owner, "input"); err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	awaitStage(t, ctx, ready)
	started := make(chan struct{})
	waiting := &transactionGate{runner: store}
	if _, ok := store.(*postgres.Store); ok {
		waiting.started = started
	} else {
		waiting.requested = started
	}
	worker.Runner = waiting
	completed := make(chan error, 1)
	go func() { completed <- worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])) }()
	awaitStage(t, ctx, started)
	clock.Advance(time.Minute)
	close(release)
	if err = <-held; err != nil {
		t.Fatal(err)
	}
	if err = <-completed; !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("pre-lock time allowed expired completion: %v", err)
	}
	worker.Runner = store
	next, err := worker.Claim(ctx, "normal", 1, time.Minute)
	if err != nil || len(next) != 1 || next[0].Claim.Epoch != 2 {
		t.Fatalf("replacement: %+v %v", next, err)
	}
	if err = worker.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
		t.Fatal(err)
	}
}

func behaviorConcurrentWorkersClaimOneOriginalJobAndCompleteNormally(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	original := assertReceived(t, out, err)
	type answer struct {
		work []durablework.Work
		err  error
	}
	start := make(chan struct{})
	answers := make(chan answer, 12)
	for i := 0; i < 12; i++ {
		adapter := store
		if pg, ok := store.(*postgres.Store); ok {
			adapter = reopen(t, pg)
		}
		name := fmt.Sprintf("worker-%02d", i)
		go func() {
			select {
			case <-start:
			case <-ctx.Done():
				answers <- answer{err: ctx.Err()}
				return
			}
			worker := durablework.NewWorker(owner, adapter, adapter, adapter, adapter)
			batch, err := worker.Claim(ctx, name, 1, time.Minute)
			answers <- answer{work: batch, err: err}
		}()
	}
	close(start)
	var claimed []durablework.Work
	for i := 0; i < 12; i++ {
		answer := <-answers
		if answer.err != nil {
			t.Fatal(answer.err)
		}
		claimed = append(claimed, answer.work...)
	}
	if len(claimed) != 1 {
		t.Fatalf("one Job assigned to %d workers", len(claimed))
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	if err = worker.Complete(ctx, claimed[0].Claim, durablework.Project(claimed[0])); err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.ID != claimed[0].Claim.JobID || got.Job.CompletedRevision != 1 || got.Input.Revision != 1 {
		t.Fatalf("original job: %+v %v", got, err)
	}
	result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "source"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("original absent")
	}
	assertReceiptSame(t, original, found.Receipt)
}

func behaviorCloseReopenRetainsClaimAndOriginalReceipt(t *testing.T, newStore func(*testing.T) workStore) {
	setup := newStore(t)
	store := setup
	// Keep the schema-creating PG connection alive solely for registered cleanup.
	// SQLite instead excludes a second writable Host until the first closes.
	if pg, ok := setup.(*postgres.Store); ok {
		store = reopen(t, pg)
		admissionReopeners.Store(store, func(t *testing.T) admissionStore { return reopen(t, pg) })
	}
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	wire := command("original", "input", "hello", nil, future())
	out, err := h.Record(ctx, wire, &principal)
	original := assertReceived(t, out, err)
	clock := &workClock{now: time.Date(2100, 1, 1, 0, 0, 0, 123456000, time.UTC)}
	worker := durablework.NewWorker(owner, store, store, store, clock)
	batch, err := worker.Claim(ctx, "old", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	old := batch[0]
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := reopenAdmissionStore(t, store).(workStore)
	h = hostFor(replacement, owner, principal)
	worker = durablework.NewWorker(owner, replacement, replacement, replacement, clock)
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.ID != old.Claim.JobID || got.Input.Text != "hello" || got.Input.Revision != 1 || got.Job.WorkRevision != 1 || got.Job.CompletedRevision != 0 || got.Job.State != "leased" || got.Projection != nil {
		t.Fatalf("reopened claim: %+v %v", got, err)
	}
	live, err := worker.Claim(ctx, "too-early", 1, time.Minute)
	if err != nil || len(live) != 0 {
		t.Fatalf("live lease lost: %+v %v", live, err)
	}
	// A persisted token must still satisfy all bindings after a fresh connection.
	renewed, err := worker.Renew(ctx, old.Claim, time.Minute)
	if err != nil || renewed.Epoch != 1 || renewed.ClaimedRevision != 1 || !renewed.LeaseUntil.Equal(old.Claim.LeaseUntil) {
		t.Fatalf("persisted token: %+v %v", renewed, err)
	}
	clock.Advance(time.Minute)
	next, err := worker.Claim(ctx, "replacement", 1, time.Minute)
	if err != nil || len(next) != 1 || next[0].Claim.Epoch != 2 || next[0].Claim.JobID != old.Claim.JobID || next[0].Input.Text != "hello" {
		t.Fatalf("replacement: %+v %v", next, err)
	}
	if err = worker.Complete(ctx, old.Claim, durablework.Project(old)); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("old persisted token accepted: %v", err)
	}
	if err = worker.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
		t.Fatal(err)
	}
	out, err = h.Record(ctx, wire, &principal)
	assertReceiptSame(t, original, assertReceived(t, out, err))
	queried, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("original unavailable after reopen")
	}
	assertReceiptSame(t, original, found.Receipt)
}

func behaviorExactOwnerTimeBoundaries(t *testing.T, newStore func(*testing.T) workStore) {
	// Explicit nanosecond values express independent expected boundaries. Both
	// zero and fractional seconds expose SQLite's default variable time encoding.
	for _, fraction := range []int{0, 123456000} {
		for _, operation := range []string{"complete", "renew"} {
			for _, boundary := range []struct {
				name    string
				offset  time.Duration
				allowed bool
			}{
				{"before", 999 * time.Microsecond, true}, {"equal", time.Millisecond, false}, {"after", 1001 * time.Microsecond, false},
			} {
				t.Run(fmt.Sprintf("%d/%s/%s", fraction, operation, boundary.name), func(t *testing.T) {
					store := newStore(t)
					ctx := contextFor(t)
					start := time.Date(2100, 1, 1, 0, 0, 0, fraction, time.UTC)
					clock := &workClock{now: start}
					h := hostFor(store, owner, principal)
					h.Clock = clock
					out, err := h.Record(ctx, command("source", "input", "hello", nil, "2101-01-01T00:00:00.000000Z"), &principal)
					assertReceived(t, out, err)
					worker := durablework.NewWorker(owner, store, store, store, clock)
					clock.Advance(-time.Microsecond)
					batch, err := worker.Claim(ctx, "before-due", 1, time.Millisecond)
					if err != nil || len(batch) != 0 {
						t.Fatalf("before due: %+v %v", batch, err)
					}
					clock.Advance(time.Microsecond)
					batch, err = worker.Claim(ctx, "at-due", 1, time.Millisecond)
					if err != nil || len(batch) != 1 {
						t.Fatalf("at exact due: %+v %v", batch, err)
					}
					work := batch[0]
					if !work.Claim.LeaseUntil.Equal(start.Add(time.Millisecond)) {
						t.Fatalf("lease precision: %+v", work.Claim)
					}
					clock.Advance(boundary.offset)
					if operation == "complete" {
						err = worker.Complete(ctx, work.Claim, durablework.Project(work))
					} else {
						_, err = worker.Renew(ctx, work.Claim, time.Minute)
					}
					if boundary.allowed {
						if err != nil {
							t.Fatalf("valid before deadline: %v", err)
						}
					} else {
						if !errors.Is(err, runtime.ErrClaim) {
							t.Fatalf("expired %s at %s accepted: %v", operation, boundary.name, err)
						}
						next, err := worker.Claim(ctx, "normal", 1, time.Minute)
						if err != nil || len(next) != 1 || next[0].Claim.JobID != work.Claim.JobID || next[0].Claim.Epoch != 2 {
							t.Fatalf("expiry replacement: %+v %v", next, err)
						}
						if err = worker.Complete(ctx, next[0].Claim, durablework.Project(next[0])); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func behaviorClaimStorageScopeAndBounds(t *testing.T, newStore func(*testing.T) workStore) {
	store, other := newStore(t), newStore(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	worker := durablework.NewWorker(owner, store, store, store, store)
	batch, err := worker.Claim(ctx, "normal", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	claim := batch[0].Claim
	var expired runtime.Tx
	err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		expired = tx
		if _, err := other.Scan(ctx, tx, time.Now(), 1); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("foreign scan: %v", err)
		}
		if _, err := other.Claim(ctx, tx, runtime.Job{ID: claim.JobID, Object: claim.Object, Phase: claim.Phase}, "other", time.Now(), time.Now().Add(time.Minute)); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("foreign claim: %v", err)
		}
		if err := other.Complete(ctx, tx, claim, time.Now()); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("foreign complete: %v", err)
		}
		if _, err := other.Renew(ctx, tx, claim, time.Now(), time.Now().Add(time.Minute)); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("foreign renew: %v", err)
		}
		wrong := claim
		wrong.Object.OwnerID = "wrong-owner"
		if err := store.Complete(ctx, tx, wrong, time.Now()); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("owner scope: %v", err)
		}
		for _, limit := range []int{0, 65} {
			if _, err := store.Scan(ctx, tx, time.Now(), limit); !errors.Is(err, runtime.ErrWorkBounds) {
				t.Fatalf("scan bound: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Scan(ctx, expired, time.Now(), 1); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("expired scan: %v", err)
	}
	for _, field := range []string{"tenant", "owner", "object-revision"} {
		forged := claim
		switch field {
		case "tenant":
			forged.Object.TenantID = "wrong-tenant"
		case "owner":
			forged.Object.OwnerID = "wrong-owner"
		case "object-revision":
			r := contract.Revision("1")
			forged.Object.Revision = &r
		}
		if err = worker.Complete(ctx, forged, durablework.Project(batch[0])); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("forged %s: %v", field, err)
		}
	}
	if err = worker.Complete(ctx, claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}

func behaviorBoundedBatchRetainsEveryOriginalResponsibility(t *testing.T, newStore func(*testing.T) workStore) {
	store := newStore(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("input-%d", i)
		out, err := h.Record(ctx, command(id, id, "hello", nil, future()), &principal)
		assertReceived(t, out, err)
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	seen := map[contract.ID]bool{}
	for round := 0; round < 4; round++ {
		batch, err := worker.Claim(ctx, "bounded", 2, time.Minute)
		if err != nil || len(batch) > 2 {
			t.Fatalf("bounded batch: %d %v", len(batch), err)
		}
		for _, work := range batch {
			if seen[work.Claim.JobID] {
				t.Fatal("original responsibility claimed twice")
			}
			seen[work.Claim.JobID] = true
			if err = worker.Complete(ctx, work.Claim, durablework.Project(work)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("only %d/5 responsibility completed", len(seen))
	}
	for i := 0; i < 5; i++ {
		got, err := h.Observe(ctx, contract.ID(fmt.Sprintf("input-%d", i)), &principal)
		if err != nil || got.Job.WorkRevision != 1 || got.Job.CompletedRevision != 1 || got.Job.State != "done" || got.Projection == nil || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
			t.Fatalf("normal bounded progress: %+v %v", got, err)
		}
	}
}
