// Package memory adapts governed Memory and fixed single-use read grants
// to the ContextAssembler. Neither request fields nor memory text grant access.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type Memory interface {
	ValidateProcessing(context.Context, memory.Binding, *wire.MemoryRef, uint64, string, string) error
	ValidateRetention(context.Context, memory.Binding, *wire.MemoryRef, uint64, string, string) error
	ValidateReferenceStorage(context.Context, memory.Binding, *wire.MemoryRef, uint64, string, string) error
}
type Grants interface {
	Authorize(context.Context, memory.Binding, memory.ReadIntent, string) error
}

type Reader interface {
	Get(context.Context, memory.Binding, *wire.MemoryGet, string) (*wire.MemoryReadResult, error)
}

// Projector implements a registered schema and trusted applicability policy.
// Its version is fixed in Config; it must not execute instructions in content.
type Projector interface {
	Project(contextassembly.Request, *wire.MemoryRecord) (contextassembly.Source, error)
}
type Authorization struct {
	Reference        contextassembly.Reference
	ReadID, Material string
}

// Config is an immutable per-decision host binding. Signed materials must come
// from the real grant issuer; this adapter cannot mint replacement read IDs.
// The host recovers these exact materials with the original task decision.
type Config struct {
	// Both fields are required to enable missing metadata. The host fixes the
	// deadline with its decision policy; a record read grant alone is insufficient.
	MissingChecker     memory.MissingChecker
	MissingRetainUntil int64
	// ReferenceOnly is an additional host retention restriction. It never grants
	// access or output storage; restoring a retained body under it is denied.
	ReferenceOnly                   bool
	Binding                         memory.Binding
	Decision                        contextassembly.Key
	FactsVersion                    uint64
	PolicyVersion, Purpose, Storage string
	Authorizations                  []Authorization
}
type Adapter struct {
	memory         Memory
	grants         Grants
	reader         Reader
	projector      Projector
	config         Config
	authorizations map[contextassembly.Reference]Authorization
}

func label(s string) bool { return len(s) > 0 && len(s) <= 256 }
func New(m Memory, reader Reader, grants Grants, projector Projector, c Config) (*Adapter, error) {
	if m == nil || reader == nil || grants == nil || projector == nil || !label(c.PolicyVersion) || !label(c.Purpose) || !label(c.Storage) || c.FactsVersion == 0 || !label(c.Binding.Subject) || !label(c.Binding.Token) || !label(c.Binding.Namespace) || !label(c.Binding.Location) || !label(c.Binding.Recipient) || c.Decision.Namespace != c.Binding.Namespace || !label(c.Decision.TaskID) || c.Decision.Decision == 0 || len(c.Authorizations) > 16 {
		return nil, contextassembly.Invalid
	}
	if (c.MissingChecker == nil) != (c.MissingRetainUntil == 0) || c.MissingRetainUntil < 0 {
		return nil, contextassembly.Invalid
	}
	a := &Adapter{memory: m, reader: reader, grants: grants, projector: projector, config: c, authorizations: map[contextassembly.Reference]Authorization{}}
	ids := map[string]bool{}
	for _, grant := range c.Authorizations {
		r := grant.Reference
		if r.Namespace != c.Binding.Namespace || !label(r.Collection) || !label(r.Key) || r.Revision == 0 || !label(grant.ReadID) || len(grant.Material) == 0 || len(grant.Material) > 65536 || ids[grant.ReadID] {
			return nil, contextassembly.Invalid
		}
		if _, ok := a.authorizations[r]; ok {
			return nil, contextassembly.Invalid
		}
		ids[grant.ReadID] = true
		a.authorizations[r] = grant
	}
	a.config.Authorizations = nil
	return a, nil
}
func (a *Adapter) scope(r contextassembly.Request) bool {
	c := a.config
	return r.Key == c.Decision && r.FactsVersion == c.FactsVersion && r.Subject == c.Binding.Subject && r.Purpose == c.Purpose && r.Location == c.Binding.Recipient && r.Storage == c.Storage && r.PolicyVersion == c.PolicyVersion
}
func wireRef(r contextassembly.Reference) *wire.MemoryRef {
	return &wire.MemoryRef{Namespace: r.Namespace, Collection: r.Collection, Key: r.Key}
}
func mapped(e error) error {
	switch e {
	case nil:
		return nil
	case memory.Denied:
		return contextassembly.Denied
	case memory.ContextInvalidated:
		return contextassembly.Invalidated
	case memory.Missing:
		return contextassembly.Missing
	default:
		return contextassembly.Unavailable
	}
}
func (a *Adapter) Validate(ctx context.Context, r contextassembly.Request, ref contextassembly.Reference, retained bool) error {
	if !a.scope(r) {
		return contextassembly.Denied
	}
	if _, ok := a.authorizations[ref]; !ok {
		return contextassembly.Denied
	}
	b := a.config.Binding
	grant := a.authorizations[ref]
	intent, e := memory.DescribeGet(b, &wire.MemoryGet{ReadId: grant.ReadID, Ref: wireRef(ref), Revision: ref.Revision, Purpose: r.Purpose})
	if e != nil {
		return contextassembly.Invalid
	}
	if e = a.grants.Authorize(ctx, b, intent, grant.Material); e != nil {
		return mapped(e)
	}
	if e := a.memory.ValidateProcessing(ctx, b, wireRef(ref), ref.Revision, r.Purpose, r.Location); e != nil {
		if e == memory.Denied && a.config.MissingChecker != nil && !retained {
			if missingErr := a.validateMissing(ctx, r, ref, r.Storage); missingErr != nil {
				return missingErr
			}
			return contextassembly.Missing
		}
		return mapped(e)
	}
	if e := a.memory.ValidateReferenceStorage(ctx, b, wireRef(ref), ref.Revision, r.Purpose, r.Storage); e != nil {
		return mapped(e)
	}
	if retained {
		if a.config.ReferenceOnly {
			return contextassembly.Denied
		}
		return mapped(a.memory.ValidateRetention(ctx, b, wireRef(ref), ref.Revision, r.Purpose, r.Storage))
	}
	return nil
}
func (a *Adapter) Load(ctx context.Context, r contextassembly.Request, ref contextassembly.Reference) (contextassembly.Source, error) {
	if e := a.Validate(ctx, r, ref, false); e != nil {
		if e == contextassembly.Missing {
			if err := a.reserveMissing(ctx, r, ref); err != nil {
				return contextassembly.Source{}, err
			}
			e = a.Validate(ctx, r, ref, false)
			if e == nil {
				e = contextassembly.Invalidated
			}
		}
		return contextassembly.Source{}, e
	}
	grant := a.authorizations[ref]
	result, e := a.reader.Get(ctx, a.config.Binding, &wire.MemoryGet{ReadId: grant.ReadID, Ref: wireRef(ref), Revision: ref.Revision, Purpose: r.Purpose}, grant.Material)
	if e != nil {
		return contextassembly.Source{}, mapped(e)
	}
	if result == nil || len(result.Records) != 1 || result.Coverage != "complete" {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	record := result.Records[0]
	if record == nil || record.Ref == nil || record.Ref.Namespace != ref.Namespace || record.Ref.Collection != ref.Collection || record.Ref.Key != ref.Key || record.Revision != ref.Revision || record.Spec == nil || record.Spec.Purpose != r.Purpose {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	out, e := a.projector.Project(r, record)
	if e != nil {
		return contextassembly.Source{}, e
	}
	// The projector cannot set retention rights, source identity or instruction role.
	raw, _ := json.Marshal(ref)
	sum := sha256.Sum256(raw)
	out.Block.Ref = "memory:" + hex.EncodeToString(sum[:])
	out.Block.Role = "memory"
	out.Block.Subject = r.Subject
	out.Block.ReplyTo = nil
	out.CanStore = false
	if e = a.Validate(ctx, r, ref, true); e == nil {
		out.CanStore = true
	} else if e != contextassembly.Denied {
		return contextassembly.Source{}, e
	}
	if e = a.Validate(ctx, r, ref, false); e != nil {
		return contextassembly.Source{}, e
	}
	return out, nil
}

var _ contextassembly.Memories = (*Adapter)(nil)
