package cleanup

import (
	"context"
	"sync"
)

// Running is owned by a host, not by the request that constructed that host.
// Close must be called before closing any stores referenced by its jobs.
type Running struct {
	worker *Worker
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	err    error
}

func Start(jobs []Job, config Config) (*Running, error) {
	worker, err := New(jobs, config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Running{worker: worker, cancel: cancel, done: make(chan struct{})}
	go func() {
		r.err = worker.Run(ctx)
		close(r.done)
	}()
	return r, nil
}
func (r *Running) Close() error {
	r.once.Do(r.cancel)
	<-r.done
	if r.err == context.Canceled {
		return nil
	}
	return r.err
}
func (r *Running) Status() []Result { return r.worker.Status() }
