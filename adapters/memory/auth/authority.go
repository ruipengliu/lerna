// Package auth binds Memory to current Harness authorization and trusted
// collection/source policy. Request fields cannot select their own resource.
package auth

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type Views interface {
	ViewActions(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)
	ReserveMemoryOperation(context.Context, string, string, string, *wire.AuthorizationAction) (authorization.MemoryAdmission, error)
	InspectMemoryOperation(context.Context, string, string) (authorization.MemoryOperationStatus, error)
}

// Sources returns memory.Unavailable only for an authorized but unavailable
// source operation; hidden or denied sources return memory.Denied.
type Sources interface {
	Check(context.Context, *wire.ContentSource, string, string, string, int64) error
}

// Collection is immutable trusted host configuration, not a record proposal.
// The registered schema bounds the complete writable field set; unsupported
// projections must use a separately approved schema, never a client override.
type Collection struct {
	// MissingMetadataUntil explicitly enables bounded collection-level absence
	// metadata. Zero disables it; record grants alone never enable this policy.
	MissingMetadataUntil                          int64
	Namespace, Name, Resource, PolicyRef, Purpose string
	Storage, Processing, Recipients               []string
	ReferenceStorage                              []string
	Schemas                                       map[string]string
}
type key struct{ namespace, collection string }
type Authority struct {
	views       Views
	sources     Sources
	collections map[key]Collection
}

func valid(s string) bool { return len(s) > 0 && len(s) <= 256 }
func locations(in []string) bool {
	if len(in) == 0 || len(in) > 16 {
		return false
	}
	for _, s := range in {
		if !valid(s) {
			return false
		}
	}
	return true
}
func New(views Views, sources Sources, collections []Collection) (*Authority, error) {
	if views == nil || sources == nil || len(collections) == 0 || len(collections) > 64 {
		return nil, memory.Invalid
	}
	out := &Authority{views: views, sources: sources, collections: map[key]Collection{}}
	for _, p := range collections {
		if !valid(p.Namespace) || !valid(p.Name) || !valid(p.Resource) || !valid(p.PolicyRef) || !valid(p.Purpose) || !locations(p.Storage) || (len(p.ReferenceStorage) > 0 && !locations(p.ReferenceStorage)) || !locations(p.Processing) || !locations(p.Recipients) || len(p.Schemas) == 0 || len(p.Schemas) > 32 {
			return nil, memory.Invalid
		}
		k := key{p.Namespace, p.Name}
		if _, ok := out.collections[k]; ok {
			return nil, memory.Invalid
		}
		p.Storage = append([]string(nil), p.Storage...)
		p.ReferenceStorage = append([]string(nil), p.ReferenceStorage...)
		p.Processing = append([]string(nil), p.Processing...)
		p.Recipients = append([]string(nil), p.Recipients...)
		schemas := map[string]string{}
		for name, digest := range p.Schemas {
			if !valid(name) || !valid(digest) {
				return nil, memory.Invalid
			}
			schemas[name] = digest
		}
		p.Schemas = schemas
		out.collections[k] = p
	}
	return out, nil
}
func contains(xs []string, value string) bool {
	for _, x := range xs {
		if x == value {
			return true
		}
	}
	return false
}
func (a *Authority) Check(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, spec *wire.MemorySpec, method string) error {
	return a.check(ctx, b, ref, spec, method, a.views.ViewActions)
}
func (a *Authority) check(ctx context.Context, b memory.Binding, ref *wire.MemoryRef, spec *wire.MemorySpec, method string, viewActions func(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)) error {
	if ref == nil || spec == nil || spec.Content == nil || b.Namespace != ref.Namespace {
		return memory.Denied
	}
	p, ok := a.collections[key{b.Namespace, ref.Collection}]
	if !ok || spec.PolicyRef != p.PolicyRef || spec.Purpose != p.Purpose || p.Schemas[spec.Content.TypeName] != spec.Content.SchemaDigest || spec.Content.SchemaDigest == "" {
		return memory.Denied
	}
	actions := []*wire.AuthorizationAction{}
	add := func(action, location string) {
		actions = append(actions, &wire.AuthorizationAction{Resource: p.Resource, Action: "memory." + action, Purpose: p.Purpose, Location: location})
	}
	sourceActions := []string{}
	location := b.Location
	switch method {
	case "put", "correct":
		if !contains(p.Storage, b.Location) || !contains(p.Processing, b.Location) {
			return memory.Denied
		}
		add(method, b.Location)
		add("store", b.Location)
		add("process", b.Location)
		sourceActions = []string{"store", "process"}
	case "retain-reference":
		if !contains(p.ReferenceStorage, b.Location) || !contains(p.Recipients, b.Location) || b.Recipient != b.Location {
			return memory.Denied
		}
		add("store_reference", b.Location)
		add("discover", b.Location)
		sourceActions = []string{"store_reference", "discover"}
	case "retain":
		if !contains(p.Storage, b.Location) || !contains(p.Processing, b.Location) || !contains(p.Recipients, b.Location) || b.Recipient != b.Location {
			return memory.Denied
		}
		for _, action := range []string{"store", "process", "discover", "disclose"} {
			add(action, b.Location)
		}
		sourceActions = []string{"store", "process", "discover", "disclose"}
	case "discover":
		if !contains(p.Recipients, b.Recipient) {
			return memory.Denied
		}
		location = b.Recipient
		add("discover", location)
		sourceActions = []string{"discover"}
	case "read":
		if !contains(p.Processing, b.Location) || !contains(p.Recipients, b.Recipient) {
			return memory.Denied
		}
		add("process", b.Location)
		add("discover", b.Recipient)
		add("disclose", b.Recipient)
		sourceActions = []string{"process"}
	default:
		return memory.Denied
	}
	check := func() error {
		view, e := viewActions(ctx, b.Token, actions)
		if e != nil || view.Identity.Namespace != b.Namespace || view.Identity.Subject != b.Subject || len(view.Allowed) != len(actions) {
			return memory.Denied
		}
		for _, allowed := range view.Allowed {
			if !allowed {
				return memory.Denied
			}
		}
		return nil
	}
	if e := check(); e != nil {
		return e
	}
	if len(spec.Sources) == 0 || len(spec.Sources) > 16 {
		return memory.Denied
	}
	for _, source := range spec.Sources {
		if source == nil || source.Ref == nil {
			return memory.Denied
		}
		for _, action := range sourceActions {
			if e := a.sources.Check(ctx, proto.Clone(source.Ref).(*wire.ContentSource), action, p.Purpose, location, spec.RetainUntil); e != nil {
				if e == memory.Unavailable {
					return memory.Unavailable
				}
				return memory.Denied
			}
		}
		if method == "read" {
			for _, action := range []string{"discover", "disclose"} {
				if e := a.sources.Check(ctx, proto.Clone(source.Ref).(*wire.ContentSource), action, p.Purpose, b.Recipient, spec.RetainUntil); e != nil {
					if e == memory.Unavailable {
						return memory.Unavailable
					}
					return memory.Denied
				}
			}
		}
	}
	return check()
}
