package localextractionsource

import (
	"context"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// Check supplies current source constraints to Memory authorization. It is a
// trusted host seam: collection and user grants are checked by Memory itself.
// This check does not register retained consumers or prove permanent erasure.
func (s *Source) Check(ctx context.Context, ref *wire.ContentSource, action, purpose, location string, retainUntil int64) error {
	if ref == nil || len(ref.ProtoReflect().GetUnknown()) != 0 {
		return memory.Denied
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now, err := s.clock.Now()
	if err != nil {
		return memory.Unavailable
	}
	e, ok := s.entries[key{ref.Kind, ref.Key, ref.Revision}]
	if !ok || retainUntil <= now.Unix() || retainUntil > e.Restrictions.RetainUntil || !contains(e.Restrictions.Purposes, purpose) {
		return memory.Denied
	}
	var allowed []string
	switch action {
	case "store", "store_reference":
		// This provider has no broader reference-only residency policy. A
		// retained reference inherits the source storage boundary; Memory
		// separately checks discovery and the host's reference-storage grant.
		allowed = e.Restrictions.Storage
	case "process":
		allowed = e.Restrictions.Processing
	case "discover", "disclose":
		allowed = e.Restrictions.Recipients
	default:
		return memory.Denied
	}
	if !contains(allowed, location) {
		return memory.Denied
	}
	_, err = readFile(ctx, e)
	return err
}
