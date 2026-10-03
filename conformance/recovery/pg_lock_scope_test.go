//go:build integration

package recovery_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	"github.com/ruipengliu/lerna/runtime"
)

// The signal follows successful acquisition through the actual storage port.
// Cleanup releases and joins the bounded transaction before schema cleanup.
func holdPGScopeLock(t *testing.T, store *postgres.Store, lock func(context.Context, runtime.Tx) error) (func(), <-chan error) {
	t.Helper()
	ctx := contextFor(t)
	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if err := lock(ctx, tx); err != nil {
				return err
			}
			close(acquired)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("held transaction: %v", err)
				}
			case <-ctx.Done():
				t.Error("held transaction did not exit by deadline", ctx.Err())
			}
		})
	}
	t.Cleanup(unlock)
	select {
	case <-acquired:
	case err := <-done:
		t.Fatalf("lock acquisition failed: %v", err)
	case <-ctx.Done():
		t.Fatal("lock acquisition deadline", ctx.Err())
	}
	return unlock, done
}

func TestPGDifferentSchemasRecordWhileCommandLocked(t *testing.T) {
	a, b := database(t), database(t)
	ctx := contextFor(t)
	observer := hostFor(reopen(t, b), owner, principal)
	ref := contract.CommandRef{Owner: owner, CommandID: "same-command"}
	_, held := holdPGScopeLock(t, a, func(ctx context.Context, tx runtime.Tx) error {
		_, err := a.LockCommand(ctx, tx, ref)
		return err
	})
	hb := hostFor(b, owner, principal)
	type answer struct {
		out contract.TransportOutcome
		err error
	}
	completed := make(chan answer, 1)
	go func() {
		out, err := hb.Record(ctx, command("same-command", "input", "hello", nil, future()), &principal)
		completed <- answer{out, err}
	}()
	var result answer
	select {
	case result = <-completed:
	case <-ctx.Done():
		t.Fatal("independent record deadline", ctx.Err())
	}
	receipt := assertReceived(t, result.out, result.err)
	applied, ok := receipt.AsApplied()
	if !ok || applied.CommandRef != ref || applied.Revision != "1" || applied.ObjectRef.ID != "input" {
		t.Fatalf("independent admission: %+v", receipt)
	}
	got, err := observer.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "hello" || got.Input.Revision != 1 || got.Job.WorkRevision != 1 {
		t.Fatalf("independent input: %+v %v", got, err)
	}
	queried, err := contract.GetCommand(ctx, readWire(ref), &principal, observer.Permissions, observer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("independent command receipt missing")
	}
	assertReceiptSame(t, receipt, found.Receipt)
	assertPGScopeLockHeld(t, held)
}

func TestPGSameSchemaStoresPreserveInputMutualExclusion(t *testing.T) {
	a := database(t)
	b := reopen(t, a)
	ctx := contextFor(t)
	h := hostFor(a, owner, principal)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	before, err := h.Observe(ctx, "input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	unlock, held := holdPGScopeLock(t, a, func(ctx context.Context, tx runtime.Tx) error {
		_, err := a.LockInput(ctx, tx, owner, "input")
		return err
	})
	worker := durablework.NewWorker(owner, b, b, b, b)
	batch, err := worker.Claim(ctx, "same-schema", 1, time.Minute)
	if err != nil || len(batch) != 0 {
		t.Fatalf("same schema bypassed held input: %+v %v", batch, err)
	}
	assertPGScopeLockHeld(t, held)
	unlock()
	batch, err = worker.Claim(ctx, "same-schema", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.JobID != before.Job.ID {
		t.Fatalf("released original work: %+v %v", batch, err)
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Job.ID != before.Job.ID || got.Job.State != "done" || got.Job.CompletedRevision != 1 || got.Projection == nil || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("released completed work: %+v %v", got, err)
	}
}

func TestPGSameSchemaStoresPreserveCommandMutualExclusion(t *testing.T) {
	a := database(t)
	b := reopen(t, a)
	ctx := contextFor(t)
	ref := contract.CommandRef{Owner: owner, CommandID: "same-command"}
	unlock, held := holdPGScopeLock(t, a, func(ctx context.Context, tx runtime.Tx) error {
		_, err := a.LockCommand(ctx, tx, ref)
		return err
	})
	h := hostFor(b, owner, principal)
	original := command("same-command", "input", "hello", nil, future())
	_, err := h.Record(ctx, original, &principal)
	assertReason(t, err, "dependency_unavailable")
	queried, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := queried.AsNotFound(); !ok {
		t.Fatal("blocked command created a receipt")
	}
	assertPGScopeLockHeld(t, held)
	unlock()
	out, err := h.Record(ctx, original, &principal)
	receipt := assertReceived(t, out, err)
	out, err = hostFor(a, owner, principal).Record(ctx, original, &principal)
	assertReceiptSame(t, receipt, assertReceived(t, out, err))
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "hello" || got.Input.Revision != 1 || got.Job.WorkRevision != 1 {
		t.Fatalf("released command normal control: %+v %v", got, err)
	}
}

func assertPGScopeLockHeld(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("holder exited before independent work finished: %v", err)
	default:
	}
}

func TestPGDifferentSchemasClaimAndCompleteWhileInputLocked(t *testing.T) {
	a, b := database(t), database(t)
	ctx := contextFor(t)
	ha, hb := hostFor(a, owner, principal), hostFor(b, owner, principal)
	observer := hostFor(reopen(t, b), owner, principal)
	out, err := ha.Record(ctx, command("same-command", "input", "first schema", nil, future()), &principal)
	assertReceived(t, out, err)
	out, err = hb.Record(ctx, command("same-command", "input", "hello", nil, future()), &principal)
	receipt := assertReceived(t, out, err)
	before, err := hb.Observe(ctx, "input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	_, held := holdPGScopeLock(t, a, func(ctx context.Context, tx runtime.Tx) error {
		_, err := a.LockInput(ctx, tx, owner, "input")
		return err
	})
	worker := durablework.NewWorker(owner, b, b, b, b)
	batch, err := worker.Claim(ctx, "independent-schema", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("independent schema claim while A holds input: %+v %v", batch, err)
	}
	if batch[0].Claim.JobID != before.Job.ID || batch[0].Input.Text != "hello" || batch[0].Input.Revision != 1 {
		t.Fatalf("independent original work changed: %+v", batch[0])
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
	got, err := observer.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "hello" || got.Job.ID != before.Job.ID || got.Job.CompletedRevision != 1 || got.Job.State != "done" || got.Projection == nil || got.Projection.InputRevision != 1 || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("independent completed projection: %+v %v", got, err)
	}
	queried, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "same-command"}), &principal, observer.Permissions, observer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("independent fixed receipt missing")
	}
	assertReceiptSame(t, receipt, found.Receipt)
	assertPGScopeLockHeld(t, held)
	first, err := ha.Observe(ctx, "input", &principal)
	if err != nil || first.Input.Text != "first schema" || first.Job.CompletedRevision != 0 || first.Projection != nil {
		t.Fatalf("other schema changed: %+v %v", first, err)
	}
}
