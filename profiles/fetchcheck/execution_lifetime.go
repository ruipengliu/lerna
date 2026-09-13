package fetchcheck

import (
	"context"
	"lerna/execution"
	"lerna/fetch"
	"sync"
	"time"
)

// Copies of a research host share the lifetime of the stores borrowed by all
// initial and recovery execution services. A wait deadline never closes stores
// still in use; the final observer retains ownership until callbacks return.
type executionLifetime struct {
	mu       sync.Mutex
	services []*execution.Service
	closing  bool
	done     chan struct{}
}

func newExecutionLifetime() *executionLifetime { return &executionLifetime{done: make(chan struct{})} }
func (l *executionLifetime) track(service *execution.Service) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		return fetch.Unavailable
	}
	l.services = append(l.services, service)
	return nil
}
func (l *executionLifetime) close(release func()) {
	l.mu.Lock()
	if !l.closing {
		l.closing = true
		services := append([]*execution.Service(nil), l.services...)
		go func() {
			for _, service := range services {
				_ = service.WaitIdle(context.Background())
			}
			release()
			close(l.done)
		}()
	}
	l.mu.Unlock()
	timer := time.NewTimer(config().DriverTimeout + 3*config().IOTimeout)
	defer timer.Stop()
	select {
	case <-l.done:
	case <-timer.C:
	}
}
