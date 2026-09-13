package fetch

import (
	"context"
	"errors"
	"time"
)

type guardKey struct{}

// WithGuard binds trusted execution admission to this acquisition context.
// Network adapters call CheckGuard at request, dial and release boundaries.
// A guard is never constructed from response content or caller JSON.
func WithGuard(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, guardKey{}, check)
}
func CheckGuard(ctx context.Context) error {
	if check, ok := ctx.Value(guardKey{}).(func(context.Context) error); ok && check != nil {
		return check(ctx)
	}
	return nil
}

// WatchGuard checks current task qualification while the network is waiting.
// It cancels only the acquisition child context, leaving the original caller
// context available to retain finite failure facts. Checks are serialized and
// bounded; the watcher is joined before returning to avoid background polling.
func WatchGuard(ctx context.Context) (context.Context, func()) {
	check, ok := ctx.Value(guardKey{}).(func(context.Context) error)
	if !ok || check == nil {
		return ctx, func() {}
	}
	watched, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watched.Done():
				return
			case <-ticker.C:
				// Poll cadence is not a transaction deadline. A composed task and
				// source check may need the existing one-second authority bound;
				// the acquisition parent still enforces its original total timeout.
				bounded, stop := context.WithTimeout(watched, time.Second)
				err := check(bounded)
				stop()
				if err != nil {
					cause := Denied
					if errors.Is(err, TimedOut) {
						cause = TimedOut
					}
					cancel(cause)
					return
				}
			}
		}
	}()
	return watched, func() { cancel(context.Canceled); <-done }
}
