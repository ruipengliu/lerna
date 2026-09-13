package contextmemory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/artifacts"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type DerivedMemory interface {
	ValidateDerived(context.Context, memory.Binding, *wire.MemoryRef, uint64, string, string, string, int64, memory.Checker) error
}
type sourceIdentity struct {
	key      string
	revision uint64
}

// Sources maps opaque artifact source identities to exact Memory references.
// The host restores this bounded trusted mapping before allowing artifact use.
// Missing mappings fail closed; arbitrary content cannot register new sources.
type Sources struct {
	memory   DerivedMemory
	policy   func(artifacts.SourceAuthority) memory.Checker
	fallback artifacts.Sources
	location string
	refs     map[sourceIdentity]contextassembly.Reference
}

func SourceReference(ref contextassembly.Reference) *wire.ContentSource {
	raw, _ := json.Marshal([]string{ref.Namespace, ref.Collection, ref.Key})
	hash := sha256.Sum256(raw)
	return &wire.ContentSource{Kind: "memory", Key: hex.EncodeToString(hash[:]), Revision: ref.Revision}
}
func NewSources(m DerivedMemory, policy func(artifacts.SourceAuthority) memory.Checker, fallback artifacts.Sources, location string, refs []contextassembly.Reference) (*Sources, error) {
	if m == nil || policy == nil || fallback == nil || !label(location) || len(refs) > 512 {
		return nil, contextassembly.Invalid
	}
	out := &Sources{m, policy, fallback, location, map[sourceIdentity]contextassembly.Reference{}}
	for _, ref := range refs {
		if !label(ref.Namespace) || !label(ref.Collection) || !label(ref.Key) || ref.Revision == 0 || ref.Revision > 1<<32 {
			return nil, contextassembly.Invalid
		}
		source := SourceReference(ref)
		id := sourceIdentity{source.Key, source.Revision}
		if _, ok := out.refs[id]; ok {
			return nil, contextassembly.Invalid
		}
		out.refs[id] = ref
	}
	return out, nil
}
func (s *Sources) Check(ctx context.Context, source *wire.ContentSource, action, purpose, location string, until int64) error {
	if source == nil || (source.Kind == "memory" || source.Kind == "memory-missing") {
		return memory.Denied
	}
	return s.fallback.Check(ctx, source, action, purpose, location, until)
}
func (s *Sources) CheckAuthorized(ctx context.Context, view artifacts.SourceAuthority, b artifacts.Binding, source *wire.ContentSource, action, purpose, location string, until int64) error {
	if source == nil || view == nil {
		return memory.Denied
	}
	if source.Kind != "memory" && source.Kind != "memory-missing" {
		if fallback, ok := s.fallback.(artifacts.AuthorizedSources); ok {
			return fallback.CheckAuthorized(ctx, view, b, source, action, purpose, location, until)
		}
		return s.fallback.Check(ctx, source, action, purpose, location, until)
	}
	ref, ok := s.refs[sourceIdentity{source.Key, source.Revision}]
	if !ok || ref.Namespace != b.Namespace {
		return memory.Denied
	}
	id, e := view.Identity(b.Token)
	if e != nil || id.Namespace != b.Namespace {
		return memory.Denied
	}
	// The artifact service has already authorized content.delete. Removing this
	// derived artifact does not remove its Memory source or disclose its body.
	// The service independently checks discovery before returning metadata.
	if action == "delete" {
		return nil
	}
	binding := memory.Binding{Token: b.Token, Subject: id.Subject, Namespace: b.Namespace, Location: s.location, Recipient: location}
	if source.Kind == "memory-missing" {
		service, ok := s.memory.(missingMemory)
		if !ok {
			return memory.Denied
		}
		checker, ok := s.policy(view).(memory.MissingChecker)
		if !ok {
			return memory.Denied
		}
		return service.ValidateMissing(ctx, binding, wireRef(ref), purpose, action, location, until, checker)
	}
	return s.memory.ValidateDerived(ctx, binding, wireRef(ref), ref.Revision, purpose, action, location, until, s.policy(view))
}

var _ artifacts.Sources = (*Sources)(nil)
var _ artifacts.AuthorizedSources = (*Sources)(nil)
