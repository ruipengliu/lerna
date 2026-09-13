// Package cleanup schedules trusted bounded cleanup steps. Durable progress and
// retry identities belong to each step, never to the in-memory scheduler.
package cleanup

import (
	"context"
	"errors"
	"sync"
	"time"
)

var Invalid = errors.New("invalid cleanup worker configuration")
var Busy = errors.New("cleanup worker already running")

type Job struct {
	Name string
	// Run performs at most one bounded batch and must honor cancellation.
	// Success means the attempt succeeded, not that its backlog is empty.
	Run func(context.Context) error
}
type Config struct{ Interval, Timeout time.Duration }
type Result struct {
	Name, State string
	Err         error
}
type Worker struct {
	jobs   []Job
	config Config
	active chan struct{}
	mu     sync.RWMutex
	status []Result
}

func New(jobs []Job, config Config) (*Worker, error) {
	if len(jobs) < 1 || len(jobs) > 16 || config.Interval < 10*time.Millisecond || config.Interval > time.Hour || config.Timeout <= 0 || config.Timeout > 10*time.Second {
		return nil, Invalid
	}
	w := &Worker{jobs: append([]Job(nil), jobs...), config: config, active: make(chan struct{}, 1)}
	seen := map[string]bool{}
	for _, j := range w.jobs {
		if len(j.Name) < 1 || len(j.Name) > 128 || j.Run == nil || seen[j.Name] {
			return nil, Invalid
		}
		seen[j.Name] = true
		w.status = append(w.status, Result{Name: j.Name, State: "not_run"})
	}
	return w, nil
}
func (w *Worker) Status() []Result {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]Result(nil), w.status...)
}
func (w *Worker) set(index int, state string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status[index] = Result{Name: w.jobs[index].Name, State: state, Err: err}
}
func (w *Worker) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case w.active <- struct{}{}:
		return nil
	default:
		return Busy
	}
}
func (w *Worker) cycle(ctx context.Context) {
	for i, j := range w.jobs {
		if ctx.Err() != nil {
			return
		}
		w.set(i, "running", nil)
		bounded, cancel := context.WithTimeout(ctx, w.config.Timeout)
		err := j.Run(bounded)
		cancel()
		state := "succeeded"
		if err != nil {
			state = "failed"
		}
		w.set(i, state, err)
	}
}

// Sweep runs each configured job once. Per-job failures remain in the returned
// results and do not block other jobs; the returned error is orchestration only.
func (w *Worker) Sweep(ctx context.Context) ([]Result, error) {
	if err := w.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-w.active }()
	w.cycle(ctx)
	return w.Status(), ctx.Err()
}

// Run owns one loop until cancellation. Hosts cancel and join it before closing
// stores. No callbacks/goroutines are detached to abandon timed-out writes.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-w.active }()
	for {
		w.cycle(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(w.config.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
