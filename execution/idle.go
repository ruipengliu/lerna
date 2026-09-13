package execution

import "context"

// WaitIdle waits for this service's dispatched driver and reconciliation calls
// to finish using their dependencies. The host must stop admitting new work
// before calling it; it is not a shutdown or cancellation operation.
func (s *Service) WaitIdle(ctx context.Context) error {
	acquired := 0
	defer func() {
		for acquired > 0 {
			<-s.slots
			acquired--
		}
	}()
	for acquired < cap(s.slots) {
		select {
		case s.slots <- struct{}{}:
			acquired++
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
