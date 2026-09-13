package memory

import (
	"context"
	wire "lerna/gen/harness/v1"
)

// MissingChecker authorizes collection-level absence metadata, separately from
// any record body. Implementations must deny unless explicitly configured.
type MissingChecker interface {
	CheckMissing(context.Context, Binding, *wire.MemoryRef, string, string, int64) error
}

// ValidateMissing checks a current absence claim under explicit metadata rights.
// It exposes no record body and allocates no read permit. Consumers must bind
// their original decision/read identity separately. The outer artifact authority
// may provide a read-only checker; independent stores are not a global snapshot.
func (s *Service) ValidateMissing(ctx context.Context, b Binding, ref *wire.MemoryRef, purpose, action, location string, until int64, checker MissingChecker) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if checker == nil || !s.binding(b) || !validReference(ref) || !known(ref.ProtoReflect()) || ref.Namespace != b.Namespace || !text(purpose, 256) || !text(location, 256) {
		return Denied
	}
	now, err := s.clock.Now()
	if err != nil {
		return Unavailable
	}
	if until <= now.Unix() {
		return Denied
	}
	target := b
	target.Location = location
	target.Recipient = location
	if err = checker.CheckMissing(ctx, target, ref, purpose, action, until); err != nil {
		return err
	}
	store, ok := s.store.(HeadStore)
	if !ok {
		return Unavailable
	}
	_, err = store.Head(ctx, refOf(ref))
	if err != nil && err != Missing {
		return Unavailable
	}
	if check := checker.CheckMissing(ctx, target, ref, purpose, action, until); check != nil {
		return check
	}
	if err == nil {
		return ContextInvalidated
	}
	return nil
}
