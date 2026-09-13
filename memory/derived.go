package memory

import (
	"context"
	wire "lerna/gen/harness/v1"
)

// ValidateDerived is a trusted in-process source-policy seam, not an SDK method.
// The host supplies a read-only checker tied to the current outer authorization
// snapshot. It must not supply caller-created grants or reopen that authority.
// Memory storage is independently read; this is not a cross-database atomic CAS.
func (s *Service) ValidateDerived(ctx context.Context, b Binding, ref *wire.MemoryRef, revision uint64, purpose, action, location string, until int64, checker Checker) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if checker == nil || !s.binding(b) || !validReference(ref) || !known(ref.ProtoReflect()) || ref.Namespace != b.Namespace || revision == 0 || revision > 1<<32 || !text(purpose, 256) || !text(location, 256) {
		return Denied
	}
	method := ""
	switch action {
	case "store", "retain":
		method = "retain"
	case "process", "disclose":
		method = "read"
	case "discover":
		method = "discover"
	default:
		return Denied
	}
	head, ok := s.store.(HeadStore)
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
	target := b
	target.Location = location
	target.Recipient = location
	if e = checker.Check(ctx, target, record.Ref, record.Spec, method); e != nil {
		return e
	}
	now, e := s.clock.Now()
	if e != nil {
		return Unavailable
	}
	spec := record.Spec
	if until <= now.Unix() || until > spec.RetainUntil || spec.RecordedAt > now.Unix() {
		return Denied
	}
	if spec.ValidFrom != nil && now.Unix() < *spec.ValidFrom || spec.ValidUntil != nil && now.Unix() >= *spec.ValidUntil {
		return ContextInvalidated
	}
	if s.validator.Validate(spec.Content) != nil {
		return ContextInvalidated
	}
	current, e := head.Head(ctx, refOf(ref))
	if e != nil {
		return Unavailable
	}
	if e = checker.Check(ctx, target, record.Ref, spec, method); e != nil {
		return e
	}
	if current != revision {
		return ContextInvalidated
	}
	return nil
}
