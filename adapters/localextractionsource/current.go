package localextractionsource

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

// Current returns only host-registered revision metadata. Manifest replacement
// is the trusted observation boundary; this method neither watches the file nor
// reads its body. The eventual qualified task checks its exact file digest.
func (s *Source) Current(ctx context.Context, scope extraction.SourceScope, location, purpose string) (*wire.ContentSource, error) {
	if ctx.Err() != nil {
		return nil, memory.Unavailable
	}
	if scope.Kind == "" || scope.Key == "" || location == "" || purpose == "" {
		return nil, memory.Invalid
	}
	now, err := s.clock.Now()
	if err != nil {
		return nil, memory.Unavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest Entry
	for k, e := range s.entries {
		if k.kind == scope.Kind && k.name == scope.Key && (latest.Ref == nil || k.revision > latest.Ref.Revision) {
			latest = e
		}
	}
	if latest.Ref == nil {
		return nil, memory.Missing
	}
	b := latest.Restrictions
	if b.RetainUntil <= now.Unix() || !contains(b.Processing, location) || !contains(b.Recipients, location) || !contains(b.Purposes, purpose) {
		return nil, memory.Denied
	}
	return proto.Clone(latest.Ref).(*wire.ContentSource), nil
}
