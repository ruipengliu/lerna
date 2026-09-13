package execution

import (
	"context"
	"lerna/authorization"
)

// StartGuard revalidates the original decision dependencies before committing
// the first may-have-started marker. It runs outside the authorization transaction
// so a context provider can query that authority without transaction reentry.
// This point-in-time check does not make independent stores globally atomic.
// Started operations bypass it and retain their original reconciliation path.
type StartGuard interface {
	ValidateStart(context.Context, Request) error
}

// BindStartGuard creates a trusted host binding. A personalized host must restore
// the original guard before exposing its execution service after restart. The
// guard cannot be supplied or disabled by a wire request. Clones share start slots.
func (s *Service) BindStartGuard(guard StartGuard) (*Service, error) {
	if s == nil || guard == nil {
		return nil, failure(authorization.Invalid)
	}
	out := *s
	out.startGuard = guard
	return &out, nil
}
