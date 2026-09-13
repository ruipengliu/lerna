package memory

import (
	"context"
	wire "lerna/gen/harness/v1"
)

const ContextInvalidated Error = "CONTEXT_INVALIDATED"

// HeadStore is a private current-version lookup used after source authorization.
// Its result must never be exposed directly as a public existence oracle.
type HeadStore interface {
	Head(context.Context, Ref) (uint64, error)
}

// ValidateCurrent checks a decision's dependency, not a historical disclosure.
// No body is returned and no read permit is allocated. A subsequent body read
// still needs its own authorization. Success is point-in-time, not a cross-store
// atomic promise extending through a model request or an external action.
func (s *Service) ValidateCurrent(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose string) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) || !validReference(ref) || !known(ref.ProtoReflect()) || ref.Namespace != b.Namespace || revision == 0 || revision > 1<<32 || !text(purpose, 256) {
		return Denied
	}
	headStore, ok := s.store.(HeadStore)
	if !ok {
		return Unavailable
	}
	row, e := s.store.Read(ctx, refOf(ref), revision)
	if e != nil {
		return Denied
	}
	record, e := decode(row)
	if e != nil {
		return Unavailable
	}
	if record.Spec.Purpose != purpose {
		return Denied
	}
	if e = s.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
		return e
	}
	if s.validator.Validate(record.Spec.Content) != nil {
		return ContextInvalidated
	}
	now, e := s.clock.Now()
	if e != nil {
		return Unavailable
	}
	if record.Spec.ValidFrom != nil && now.Unix() < *record.Spec.ValidFrom || record.Spec.ValidUntil != nil && now.Unix() >= *record.Spec.ValidUntil {
		return ContextInvalidated
	}
	current, e := headStore.Head(ctx, refOf(ref))
	if e != nil {
		return Unavailable
	}
	// Check policy again after the store lookup, before revealing staleness.
	if e = s.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
		return e
	}
	if current != revision {
		return ContextInvalidated
	}
	return nil
}

// ValidateRetention separately authorizes a context body at the snapshot's
// storage location. Permission to disclose to a model never implies retention.
func (s *Service) ValidateRetention(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose, storage string) error {
	return s.validateTarget(ctx, b, ref, revision, purpose, storage, "retain")
}

// ValidateProcessing checks the actual model processing location independently
// from the Memory service's own location and ordinary disclosure permission.
func (s *Service) ValidateProcessing(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose, location string) error {
	return s.validateTarget(ctx, b, ref, revision, purpose, location, "read")
}
func (s *Service) validateTarget(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose, location, method string) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !text(location, 256) {
		return Denied
	}
	if e := s.ValidateCurrent(ctx, b, ref, revision, purpose); e != nil {
		return e
	}
	row, e := s.store.Read(ctx, refOf(ref), revision)
	if e != nil {
		return Unavailable
	}
	record, e := decode(row)
	if e != nil {
		return Unavailable
	}
	target := b
	target.Location = location
	target.Recipient = location
	if e = s.authority.Check(ctx, target, record.Ref, record.Spec, method); e != nil {
		return e
	}
	return s.ValidateCurrent(ctx, b, ref, revision, purpose)
}

// ValidateReferenceStorage authorizes the reference and derived metadata at the
// snapshot location. It is independent of both discovery and body retention.
func (s *Service) ValidateReferenceStorage(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose, storage string) error {
	return s.validateTarget(ctx, b, ref, revision, purpose, storage, "retain-reference")
}
