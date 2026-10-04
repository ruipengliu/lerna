package target

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/sqlclosetest"
)

// This is a mechanical database/sql lifetime test, with no physical database
// scope. It does not claim a native SQLite fault or a business effect.
func TestTargetClosePreservesFirstNativeFailure(t *testing.T) {
	cause := errors.New("mechanical native close failure")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: cause})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	released := false
	holder := &Target{db: db, cfg: Config{IOTimeout: time.Second}, gate: make(chan struct{}, 1), release: func() error {
		released = true
		return nil
	}}
	if err := holder.Close(); !errors.Is(err, cause) {
		t.Fatalf("first Close lost original cause: %v", err)
	}
	if err := holder.Close(); !errors.Is(err, cause) {
		t.Fatalf("second Close erased unknown native outcome: %v", err)
	}
	if released {
		t.Fatal("writer released despite unconfirmed native closure")
	}
}

func TestObserverClosePreservesFirstNativeFailure(t *testing.T) {
	cause := errors.New("mechanical observer native close failure")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: cause})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	holder := &Observer{db: db, cfg: ObserverConfig{IOTimeout: time.Second}}
	for range 2 {
		if err := holder.Close(); !errors.Is(err, cause) {
			t.Fatalf("Observer Close erased original native uncertainty: %v", err)
		}
	}
}

func TestTargetConcurrentCloseWaitIsBoundedBeforeNativeResult(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	cause := errors.New("mechanical blocked native close")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: cause, CloseStarted: started, CloseRelease: release})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	holder := &Target{db: db, cfg: Config{IOTimeout: 20 * time.Millisecond}, gate: make(chan struct{}, 1), release: func() error { return nil }}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- holder.Close() }()
	select {
	case <-started:
	case <-ctx.Done():
		close(release)
		<-first
		t.Fatal("native close did not reach mechanical gate")
	}
	go func() { second <- holder.Close() }()
	var secondErr error
	returned := false
	select {
	case secondErr = <-second:
		returned = true
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-first:
		if !errors.Is(err, cause) {
			t.Error("first native cause lost", err)
		}
	case <-ctx.Done():
		t.Fatal("first mechanical Close did not join")
	}
	if !returned {
		select {
		case secondErr = <-second:
		case <-ctx.Done():
			t.Fatal("second mechanical Close did not join")
		}
	}
	if !returned || !errors.Is(secondErr, context.DeadlineExceeded) {
		t.Fatalf("concurrent Close bypassed its finite gate wait: returned=%t cause=%v", returned, secondErr)
	}
}

func TestTargetOpenFailureRetainsAcquiredCleanupHolder(t *testing.T) {
	startup, cleanup := errors.New("mechanical open failure"), errors.New("mechanical acquired release unknown")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cfg := Config{Path: "/mechanical-no-physical-file", Identity: "mechanical", Window: time.Minute, QueryMode: QueryEnabled, IOTimeout: time.Second, BusyTimeout: time.Millisecond, Now: time.Now}
	holder, err := openTarget(ctx, cfg,
		func(context.Context, string) (func() error, error) { return func() error { return cleanup }, nil },
		func(string, string) (*sql.DB, error) { return nil, startup })
	if holder == nil || !errors.Is(err, startup) || !errors.Is(err, cleanup) {
		t.Fatalf("acquired cleanup responsibility lost: holder=%t startup=%t cleanup=%t", holder != nil, errors.Is(err, startup), errors.Is(err, cleanup))
	}
	if err = holder.Close(); !errors.Is(err, cleanup) {
		t.Fatal("cleanup holder erased original release uncertainty", err)
	}
	if _, err = holder.Read(ctx, "fake-resource"); err == nil {
		t.Fatal("cleanup-only holder served business")
	}
}

func TestTargetCloseDrainTimeoutRemainsRetryable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	nativeClosed := make(chan struct{})
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseDone: nativeClosed})
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	holder := &Target{db: db, cfg: Config{IOTimeout: 20 * time.Millisecond}, gate: make(chan struct{}, 1), release: func() error {
		select {
		case <-nativeClosed:
			return nil
		default:
			return errors.New("writer release preceded actual native Close")
		}
	}}
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, leave, err := holder.enter(ctx)
		if err != nil {
			joined <- err
			return
		}
		close(entered)
		// Mechanical lifetime callback, deliberately held beyond operation ctx.
		// The finite test supervisor owns its release and joins it; this is not
		// a SQLite query or a promise that native operations respect cancellation.
		select {
		case <-release:
		case <-ctx.Done():
		}
		leave()
		joined <- nil
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		close(release)
		<-joined
		t.Fatal("physical owner operation did not enter")
	}
	firstErr := holder.Close()
	select {
	case <-nativeClosed:
		t.Error("Close reached native driver before active owner exit")
	default:
	}
	close(release)
	if err := <-joined; err != nil {
		t.Fatal("physical owner callback did not join", err)
	}
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatal("active owner drain did not time out finitely", firstErr)
	}
	if _, err := holder.Write(ctx, Request{Key: "fake-key", Resource: "fake-resource", Data: []byte{1}}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatal("closing target admitted another operation after drain timeout", err)
	}
	if err := holder.Close(); err != nil {
		t.Fatal("pre-native timeout was incorrectly made permanent", err)
	}
	select {
	case <-nativeClosed:
	default:
		t.Fatal("retry did not actually close native connection")
	}
	if err := holder.Close(); err != nil {
		t.Fatal("confirmed repeated Close failed", err)
	}
}
