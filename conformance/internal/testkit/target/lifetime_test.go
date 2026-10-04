package target

import (
	"context"
	"errors"
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
