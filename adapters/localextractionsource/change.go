package localextractionsource

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/extraction"
	"lerna/memory"
	"reflect"
	"time"
)

type ChangeFence interface {
	extraction.CandidateInvalidator
	extraction.SourceFence
}

// Change is a trusted host event, never an instruction parsed from a source.
// It retires old source revisions before activating replacement metadata. A
// policy-only change also needs a new revision. Nil replacement removes only
// revisions through the event cutoff, preserving any newer registered source.
// Keep using SourceGuard for reads: restoring an old raw configuration must
// never bypass the durable fence. Hosts replay the same authoritative event
// after interruption; this method does not persist the host's source manifest.
func (s *Source) Change(ctx context.Context, fence ChangeFence, event extraction.SourceInvalidation, replacement *Entry) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if fence == nil || event.Namespace == "" || len(event.Namespace) > 256 || event.Kind == "" || len(event.Kind) > 128 || event.Key == "" || len(event.Key) > 256 || event.ThroughRevision == 0 {
		return 0, memory.Invalid
	}
	var next *Entry
	if replacement != nil {
		validated, err := New([]Entry{*replacement}, s.clock)
		if err != nil {
			return 0, err
		}
		for _, entry := range validated.entries {
			copy := entry
			next = &copy
		}
		if next.Ref.Kind != event.Kind || next.Ref.Key != event.Key || next.Ref.Revision <= event.ThroughRevision {
			return 0, memory.Invalid
		}
		if err = fence.CheckSource(ctx, event.Namespace, next.Ref); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make(map[key]Entry, len(s.entries)+1)
	for k, entry := range s.entries {
		if k.kind == event.Kind && k.name == event.Key {
			if k.revision <= event.ThroughRevision {
				continue
			}
			if next != nil {
				oldConfig, newConfig := entry, *next
				oldConfig.Ref, newConfig.Ref = nil, nil
				if !proto.Equal(entry.Ref, next.Ref) || !reflect.DeepEqual(oldConfig, newConfig) {
					return 0, memory.IdentityConflict
				}
			}
		}
		entries[k] = entry
	}
	if next != nil {
		entries[key{next.Ref.Kind, next.Ref.Key, next.Ref.Revision}] = *next
	}
	if len(entries) > 256 {
		return 0, memory.Capacity
	}
	count, err := fence.InvalidateSource(ctx, event)
	if err != nil {
		return 0, err
	}
	// Unknown invalidation never enables a replacement. A successful fence may
	// remain even when a later check fails; retry reconciles that original fact.
	if ctx.Err() != nil {
		return count, memory.Unavailable
	}
	if next != nil {
		if err = fence.CheckSource(ctx, event.Namespace, next.Ref); err != nil {
			return count, err
		}
	}
	s.entries = entries
	return count, nil
}
