//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"fmt"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
)

func TestPGScanClaimsSnapshotAndProjectsOutsideTransaction(t *testing.T) {
	store := database(t)
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

func TestPGOldCompletionPreservesNewRevisionAndExactSnapshot(t *testing.T) {
	store := database(t)
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

func TestPGRenewBindsClaimAndExpiresWithoutReplacement(t *testing.T) {
	store := database(t)
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
	shadowStore := reopen(t, store)
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
	replacementStore := reopen(t, store)
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

type workClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *workClock) Now(context.Context, runtime.Tx) (time.Time, error) { return c.Time(), nil }
func (c *workClock) Time() time.Time                                    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *workClock) Advance(d time.Duration)                            { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

func TestPGTriggerRejectsRegressingAndOutOfRangeRevisions(t *testing.T) {
	store := database(t)
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

// transactionGate delegates every SQL operation to the real adapter, pausing
// only at an authorized Host Tx seam. Signals name actual pre-commit stages;
// they are synchronization inputs, not invocation-count business assertions.
type transactionGate struct {
	runner  runtime.TxRunner
	started chan struct{}
	ready   chan struct{}
	release chan struct{}
}

func (g *transactionGate) Within(ctx context.Context, o contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
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

func TestPGConcurrentNewWorkAndCompletionBothCommitOrders(t *testing.T) {
	for _, newFirst := range []bool{true, false} {
		name := "completion-first"
		if newFirst {
			name = "new-work-first"
		}
		t.Run(name, func(t *testing.T) {
			store := database(t)
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
			contender := &transactionGate{runner: store, started: started}
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

func TestPGBoundedScanSkipsHeldObjectAndRetainsAllResponsibility(t *testing.T) {
	store := database(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("input-%d", i)
		out, err := h.Record(ctx, command(id, id, "hello", nil, future()), &principal)
		assertReceived(t, out, err)
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := store.LockInput(ctx, tx, owner, "input-0"); err != nil {
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
	batch, err := worker.Claim(ctx, "bounded", 1, time.Minute)
	if err != nil || len(batch) != 0 {
		t.Fatalf("scan exceeded single held candidate: %+v %v", batch, err)
	}
	batch, err = worker.Claim(ctx, "bounded", 2, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Input.ID != "input-1" {
		t.Fatalf("held object blocked normal candidate: %+v %v", batch, err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	seen := map[contract.ID]bool{}
	for len(batch) > 0 {
		for _, work := range batch {
			if seen[work.Claim.JobID] {
				t.Fatal("duplicated claim")
			}
			seen[work.Claim.JobID] = true
			if err = worker.Complete(ctx, work.Claim, durablework.Project(work)); err != nil {
				t.Fatal(err)
			}
		}
		batch, err = worker.Claim(ctx, "bounded", 2, time.Minute)
		if err != nil || len(batch) > 2 {
			t.Fatalf("unbounded claim: %d %v", len(batch), err)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("lost responsibility: %d", len(seen))
	}
	for i := 0; i < 5; i++ {
		got, err := h.Observe(ctx, contract.ID(fmt.Sprintf("input-%d", i)), &principal)
		if err != nil || got.Job.CompletedRevision != 1 || got.Input.Revision != 1 || got.Projection.InputRevision != 1 {
			t.Fatalf("normal projection: %+v %v", got, err)
		}
	}
}

func TestPGIndependentWorkersClaimOneOriginalJobAndCompleteNormally(t *testing.T) {
	store := database(t)
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
		adapter := reopen(t, store)
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

func TestPGClaimRequestBoundsAndFailedProjectionRollBackCompletion(t *testing.T) {
	store := database(t)
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

func TestPGClaimV2MigrationPreservesV1AndReportsExactArtifacts(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	versions, err := store.MigrationVersions(ctx)
	if err != nil || len(versions) != 2 || versions[0].Version != 1 || versions[0].Checksum != "sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e" || versions[1].Version != 2 || versions[1].Checksum != "sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297" {
		t.Fatalf("v1/v2 metadata: %+v %v", versions, err)
	}
	t.Logf("applied migrations: %+v", versions)
	h := hostFor(store, owner, principal)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	adapter := reopen(t, store)
	worker := durablework.NewWorker(owner, adapter, adapter, adapter, adapter)
	batch, err := worker.Claim(ctx, "v2", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("actual v2 claim: %+v %v", batch, err)
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}

func TestPGMaximumRevisionClaimsAndCompletesWithoutOverflow(t *testing.T) {
	store := database(t)
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

func TestPGLeaseExpiryIsCheckedAfterWaitingForObjectLock(t *testing.T) {
	store := database(t)
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
	worker.Runner = &transactionGate{runner: store, started: started}
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
