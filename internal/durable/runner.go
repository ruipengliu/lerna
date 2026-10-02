package durable

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var ErrDrain = errors.New("durable: drain deadline reached; handlers still own capacity")

type Handler interface {
	Handle(context.Context, *Work) error
}
type HandlerFunc func(context.Context, *Work) error

func (f HandlerFunc) Handle(ctx context.Context, w *Work) error { return f(ctx, w) }

type Reconciler interface {
	Reconcile(context.Context, string, int) (string, error)
}
type Binding struct {
	Kind       string
	Handler    Handler
	Reconciler Reconciler
}
type RunnerOptions struct {
	Scope                                                  Scope
	Capacity, Batch, Steps, ReconcileLimit                 int
	Scan, Lease, Renew, WorkTimeout, Drain, ReconcileEvery time.Duration
	OnError                                                func(error)
}
type Runner struct {
	engine      *Engine
	bindings    []Binding
	options     RunnerOptions
	holder      string
	slots       chan struct{}
	active      atomic.Int64
	wg          sync.WaitGroup
	started     atomic.Bool
	pollStopped atomic.Bool
	waitOnce    sync.Once
	waitDone    chan struct{}
	reconcile   atomic.Bool
}
type Work struct {
	claim     Claim
	scope     Scope
	engine    *Engine
	ctx       context.Context
	inTx      atomic.Bool
	remaining atomic.Int64
}

func (w *Work) Claim() Claim { return w.claim }
func (w *Work) Scope() Scope { return w.scope }

// Step bounds handler orchestration; domains still authorize the actual port.
func (w *Work) Step(ctx context.Context, fn func() error) error {
	if fn == nil {
		return ErrPrecondition
	}
	if w.ctx.Err() != nil {
		return w.ctx.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if w.remaining.Add(-1) < 0 {
		return ErrSteps
	}
	return fn()
}

// Within binds repositories to this work's scope and lifetime, with no nested
// entry even when a helper accidentally reuses the original handler context.
func (w *Work) Within(ctx context.Context, participants []Participant, fn func(*Tx) error) Result {
	if !w.inTx.CompareAndSwap(false, true) {
		return Result{RolledBack, 0, ErrNested}
	}
	defer w.inTx.Store(false)
	if ctx.Value(transactionMarker{}) != nil {
		return Result{RolledBack, 0, ErrNested}
	}
	bounded, cancel := context.WithCancel(w.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	if ctx.Err() != nil {
		cancel()
	}
	return w.engine.Within(bounded, w.scope, participants, fn)
}
func NewRunner(e *Engine, bindings []Binding, options RunnerOptions) (*Runner, error) {
	if e == nil || !options.Scope.Valid() || options.Capacity < 1 || options.Capacity > 32 || options.Batch < 1 || options.Batch > options.Capacity || options.Steps < 1 || options.Steps > 128 {
		return nil, ErrPrecondition
	}
	if options.Scan < time.Millisecond || options.Scan > time.Second || options.Lease < 100*time.Millisecond || options.Lease > 30*time.Second || options.Renew < time.Millisecond || options.Renew >= options.Lease/2 || options.WorkTimeout <= 0 || options.WorkTimeout > 30*time.Second || options.Drain <= 0 || options.Drain > 30*time.Second {
		return nil, ErrPrecondition
	}
	if options.ReconcileLimit == 0 {
		options.ReconcileLimit = 32
	}
	if options.ReconcileEvery == 0 {
		options.ReconcileEvery = 10 * time.Second
	}
	if options.ReconcileLimit < 1 || options.ReconcileLimit > 128 || options.ReconcileEvery < options.Scan {
		return nil, ErrPrecondition
	}
	seen := map[string]bool{}
	for _, b := range bindings {
		if !e.kinds[b.Kind] || b.Handler == nil || seen[b.Kind] {
			return nil, ErrUnsupported
		}
		seen[b.Kind] = true
	}
	if len(bindings) == 0 {
		return nil, ErrUnsupported
	}
	return &Runner{engine: e, bindings: append([]Binding(nil), bindings...), options: options, holder: NewID("boot"), slots: make(chan struct{}, options.Capacity), waitDone: make(chan struct{})}, nil
}
func (r *Runner) Active() int64    { return r.active.Load() }
func (r *Runner) HolderID() string { return r.holder }
func (r *Runner) report(err error) {
	if err != nil && r.options.OnError != nil {
		r.options.OnError(err)
	}
}
func (r *Runner) Run(ctx context.Context) error {
	if !r.started.CompareAndSwap(false, true) {
		return ErrPrecondition
	}
	ticker := time.NewTicker(r.options.Scan)
	defer ticker.Stop()
	next := 0
	lastReconcile := time.Now()
	cursors := make([]string, len(r.bindings))
	for ctx.Err() == nil {
		binding := r.bindings[next]
		next = (next + 1) % len(r.bindings)
		reserved := 0
		for reserved < r.options.Batch {
			select {
			case r.slots <- struct{}{}:
				reserved++
			default:
				goto claimed
			}
		}
	claimed:
		if reserved > 0 {
			start := time.Now()
			claims, result := r.engine.Claim(ctx, r.options.Scope, binding.Kind, r.holder, reserved, r.options.Lease)
			if result.Err != nil && ctx.Err() == nil {
				r.report(result.Err)
			}
			for i := len(claims); i < reserved; i++ {
				<-r.slots
			}
			for _, claim := range claims {
				if ctx.Err() != nil || !time.Now().Before(localStop(claim.Until(), start, r.options.Lease)) {
					<-r.slots
					continue
				}
				r.wg.Add(1)
				r.active.Add(1)
				go r.handle(ctx, binding, claim, start)
			}
		}
		if time.Since(lastReconcile) >= r.options.ReconcileEvery && r.reconcile.CompareAndSwap(false, true) {
			lastReconcile = time.Now()
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				defer r.reconcile.Store(false)
				for i, b := range r.bindings {
					if b.Reconciler == nil {
						continue
					}
					bounded, cancel := context.WithTimeout(ctx, r.options.WorkTimeout)
					cursor, err := b.Reconciler.Reconcile(bounded, cursors[i], r.options.ReconcileLimit)
					cancel()
					if err == nil {
						cursors[i] = cursor
					} else {
						r.report(err)
					}
				}
			}()
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		case <-r.engine.backend.Wake():
		}
	}
	r.pollStopped.Store(true)
	bounded, cancel := context.WithTimeout(context.Background(), r.options.Drain)
	defer cancel()
	return r.Wait(bounded)
}

// localStop is a conservative cancellation hint under the declared local-clock
// assumption. It grants no send permission; Guard/Finish use database time.
func localStop(until time.Time, requestStart time.Time, lease time.Duration) time.Time {
	margin := min(20*time.Millisecond, lease/10)
	return minTime(until.Add(-margin), requestStart.Add(lease-margin))
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (r *Runner) handle(parent context.Context, binding Binding, claim Claim, start time.Time) {
	defer r.wg.Done()
	defer func() { <-r.slots; r.active.Add(-1) }()
	ctx, cancel := context.WithTimeout(parent, r.options.WorkTimeout)
	defer cancel()
	finished := make(chan struct{})
	renewed := make(chan struct{})
	go func() {
		defer close(renewed)
		stop := localStop(claim.Until(), start, r.options.Lease)
		// Cancellation runs independently of a renewal blocked on a database lock.
		timer := time.AfterFunc(time.Until(stop), cancel)
		defer timer.Stop()
		ticker := time.NewTicker(r.options.Renew)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-finished:
				return
			case <-ticker.C:
				request := time.Now()
				bounded, end := context.WithDeadline(ctx, stop)
				until, result := r.engine.Renew(bounded, claim, r.options.Lease)
				end()
				if result.Outcome != Committed || ctx.Err() != nil || !time.Now().Before(stop) {
					cancel()
					return
				}
				stop = localStop(until, request, r.options.Lease)
				timer.Reset(time.Until(stop))
			}
		}
	}()
	w := &Work{claim: claim, scope: claim.Scope(), engine: r.engine, ctx: ctx}
	w.remaining.Store(int64(r.options.Steps))
	func() {
		defer func() {
			if v := recover(); v != nil {
				r.report(fmt.Errorf("durable handler panic: %T", v))
			}
		}()
		r.report(binding.Handler.Handle(ctx, w))
	}()
	close(finished)
	cancel()
	<-renewed
}

// Wait observes actual handler/reconcile exit. A timed out Run does not release
// slots or make it safe to close the underlying database while code still runs.
func (r *Runner) Wait(ctx context.Context) error {
	if !r.pollStopped.Load() {
		return ErrPrecondition
	}
	r.waitOnce.Do(func() { go func() { r.wg.Wait(); close(r.waitDone) }() })
	select {
	case <-r.waitDone:
		return nil
	case <-ctx.Done():
		return ErrDrain
	}
}
