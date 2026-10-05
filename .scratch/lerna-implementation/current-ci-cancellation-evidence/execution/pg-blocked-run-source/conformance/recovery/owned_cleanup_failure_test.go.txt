//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

func TestOwnedFixtureCleanupAfterConfirmedPGHolderFailure(t *testing.T) {
	ctx := contextFor(t)
	// The owned empty schema gives a real driver undefined-table error, rather
	// than a fabricated transaction error or private business-row assertion.
	f, err := createOwnedFixture(ctx, "postgres", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := f.Cleanup()
		var cause *pgconn.PgError
		if !errors.As(err, &cause) || cause.Code != "42P01" {
			t.Errorf("reported holder SQL cause: %v", err)
		}
	})
	if _, err = f.Open(ctx); err != nil {
		t.Fatal(err)
	}
	peer := f.PGPeer(t, 0, 10*time.Second)
	neighbor := newOwnedFixture(t, "postgres", true)
	adjacent := hostFor(neighbor.Store(), owner, principal)
	wire := command("neighbor", "input", "neighbor", nil, future())
	receipt := assertReceivedResult(t, adjacent, ctx, wire)
	hold := f.startPGTransaction(ctx, peer, func(ctx context.Context, tx runtime.Tx) error {
		_, err := peer.LoadSchedule(ctx, tx, owner, "input", 1)
		return err
	})
	result := hold.ReleaseAndJoin()
	var cause *pgconn.PgError
	if !errors.As(result, &cause) || cause.Code != "42P01" {
		t.Fatalf("real holder SQL error: %v", result)
	}
	if err := f.Store().(interface{ Migrate(context.Context) error }).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	canceled, cancelHolder := context.WithCancel(ctx)
	cancellation := f.startPGTransaction(canceled, peer, func(ctx context.Context, tx runtime.Tx) error {
		_, err := peer.LockInput(ctx, tx, owner, "cancelled-holder")
		return err
	})
	select {
	case <-cancellation.acquired:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelHolder()
	canceledResult := cancellation.ReleaseAndJoin()
	if !errors.Is(canceledResult, context.Canceled) {
		t.Fatalf("real holder cancellation: %v", canceledResult)
	}
	handles := []waitStore{f.Store(), peer}
	for _, handle := range handles {
		if err := handle.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error { _, err := handle.Now(ctx, tx); return err }); err != nil {
			t.Fatalf("normal live handle: %v", err)
		}
	}
	err = f.Cleanup()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup lost second holder cause: %v", err)
	}
	if !errors.Is(err, result) || !errors.As(err, &cause) {
		t.Fatalf("cleanup discarded exited holder failure: %v", err)
	}
	assertOwnedScopeExists(t, f, false)
	for _, handle := range handles {
		if err := handle.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error { _, err := handle.Now(ctx, tx); return err }); err == nil {
			t.Fatal("cleanup left a previously live writer/peer open")
		}
	}
	for range 2 {
		err = f.Cleanup()
		if !errors.Is(err, result) {
			t.Fatalf("repeat cleanup discarded cause: %v", err)
		}
		assertOwnedScopeExists(t, f, false)
	}
	if _, err = f.Open(ctx); err == nil {
		t.Fatal("cleaned fixture admitted writer")
	}
	got, err := adjacent.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "neighbor" {
		t.Fatalf("neighbor damaged: %+v %v", got, err)
	}
	out, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "neighbor"}), &principal, adjacent.Permissions, adjacent, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := out.AsFound()
	if !ok {
		t.Fatal("neighbor receipt absent")
	}
	assertReceiptSame(t, receipt, found.Receipt)
}

func TestOwnedFixtureCleanupRetainsUnconfirmedPGCallbackAndRetries(t *testing.T) {
	ctx := contextFor(t)
	f, err := createOwnedFixture(ctx, "postgres", true)
	if err != nil {
		t.Fatal(err)
	}
	// A real SQL lock is acquired before the deliberately uncooperative callback
	// blocks. Expired join time is not proof that this Within callback exited.
	entered, resume := make(chan struct{}), make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(resume) }) }
	holderCtx, cancelHolder := context.WithCancel(ctx)
	t.Cleanup(func() {
		unblock()
		cancelHolder()
		if err := f.Cleanup(); err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	peer := f.PGPeer(t, 0, 10*time.Second)
	h := hostFor(f.Store(), owner, principal)
	wire := command("original", "input", "hello", nil, future())
	receipt := assertReceivedResult(t, h, ctx, wire)
	neighbor := newOwnedFixture(t, "postgres", true)
	adjacent := hostFor(neighbor.Store(), owner, principal)
	assertReceivedResult(t, adjacent, ctx, command("neighbor", "input", "neighbor", nil, future()))
	hold := f.startPGTransaction(holderCtx, peer, func(ctx context.Context, tx runtime.Tx) error {
		if _, err := peer.LockInput(ctx, tx, owner, "input"); err != nil {
			return err
		}
		close(entered)
		<-resume
		return ctx.Err()
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for range 2 {
		joinCtx, cancelJoin := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err = f.cleanup(joinCtx)
		cancelJoin()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unconfirmed cleanup cause: %v", err)
		}
		assertOwnedScopeExists(t, f, true)
		out, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "original"}), &principal, h.Permissions, h, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := out.AsFound()
		if !ok {
			t.Fatal("active scope lost original receipt")
		}
		assertReceiptSame(t, receipt, found.Receipt)
		if _, err = f.Open(ctx); err == nil {
			t.Fatal("unconfirmed fixture admitted writer")
		}
	}
	cancelHolder()
	unblock()
	if err = hold.ReleaseAndJoin(); !errors.Is(err, context.Canceled) {
		t.Fatalf("real Within exit cause: %v", err)
	}
	if err = f.Cleanup(); !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmed cleanup diagnostic: %v", err)
	}
	assertOwnedScopeExists(t, f, false)
	if err = f.Cleanup(); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeat cleanup diagnostic: %v", err)
	}
	got, err := adjacent.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "neighbor" {
		t.Fatalf("neighbor damaged: %+v %v", got, err)
	}
}
