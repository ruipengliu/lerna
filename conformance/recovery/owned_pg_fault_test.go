//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/runtime"
)

// pgTransactionHold is the real, finite SQL lock fault adapter. The fixture
// joins it before closing its writer or dropping its scope. The lock callback
// remains in the storage mechanism story; this is not a resource registry.
type pgTransactionHold struct {
	release     chan struct{}
	done        chan error
	once        sync.Once
	heldContext context.Context
	joined      bool
	result      error
}

func (f *ownedFixture) holdPGTransaction(t *testing.T, store *postgres.Store, lock func(context.Context, runtime.Tx) error) *pgTransactionHold {
	t.Helper()
	if f.closing {
		t.Fatal("fixture is closing")
	}
	ctx := contextFor(t)
	acquired := make(chan struct{})
	hold := &pgTransactionHold{release: make(chan struct{}), done: make(chan error, 1)}
	f.holders = append(f.holders, hold)
	go func() {
		defer close(hold.done)
		hold.done <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if err := lock(ctx, tx); err != nil {
				return err
			}
			hold.heldContext = ctx
			close(acquired)
			select {
			case <-hold.release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-acquired:
	case err := <-hold.done:
		hold.joined = true
		hold.result = err
		t.Fatalf("real lock acquisition: %v", err)
	case <-ctx.Done():
		t.Fatal("real lock acquisition deadline", ctx.Err())
	}
	return hold
}
func (h *pgTransactionHold) Check(t *testing.T) {
	t.Helper()
	if err := h.heldContext.Err(); err != nil {
		t.Fatalf("input-lock holder authority expired before release: %v", err)
	}
	select {
	case err := <-h.done:
		h.joined = true
		h.result = err
		t.Fatalf("input-lock holder exited before release: %v", err)
	default:
	}
}
func (h *pgTransactionHold) ReleaseAndJoin() error {
	h.once.Do(func() { close(h.release) })
	if h.joined {
		return h.result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Second)
	defer cancel()
	select {
	case err := <-h.done:
		h.joined = true
		h.result = err
		return err
	case <-ctx.Done():
		return fmt.Errorf("real PG fault-holder exit unconfirmed: %w", ctx.Err())
	}
}
func (f *ownedFixture) joinPGHolders() error {
	var errs []error
	for _, holder := range f.holders {
		if err := holder.ReleaseAndJoin(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
