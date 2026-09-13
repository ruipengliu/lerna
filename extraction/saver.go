package extraction

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"time"
)

type SaveAuthority interface {
	Check(context.Context, memory.Binding, string) error
}
type SaveSources interface {
	Validate(context.Context, []*wire.ContentSource, Restrictions) error
}
type MemoryWriter interface {
	Put(context.Context, memory.Binding, *wire.MemoryWrite) (memory.Receipt, error)
	InspectOperation(context.Context, memory.Binding, string) (memory.OperationState, error)
}
type SavingStore interface {
	CandidateLifecycle
	SaveJournal
}
type SaveTarget struct{ Collection, PolicyRef, Purpose string }
type Saver struct {
	store    SavingStore
	auth     SaveAuthority
	sources  SaveSources
	memory   MemoryWriter
	clock    memory.Clock
	binding  memory.Binding
	target   SaveTarget
	allocate func(context.Context) (string, error)
}

// Inspect reconciles an original save without starting or retrying effects.
func (s *Saver) Inspect(ctx context.Context, candidate string) (memory.OperationState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	intent, err := s.store.LookupSave(ctx, s.binding.Namespace, s.binding.Subject, candidate)
	if err != nil {
		return memory.OperationState{}, err
	}
	return s.memory.InspectOperation(ctx, s.binding, intent.Request.OperationId)
}

// SavedSchema bounds the complete projection emitted by this reference saver.
// Attribute/value are structured local-rule outputs; it performs no arbitrary
// text redaction and cannot grant broader source residency or disclosure.
func SavedSchema() schema.Resource {
	return schema.Resource{Type: "extracted-memory", ID: "urn:harness:extracted-memory", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:harness:extracted-memory","type":"object","additionalProperties":false,"required":["attribute","value"],"properties":{"attribute":{"type":"string","minLength":1,"maxLength":256},"value":{"type":"string","minLength":1,"maxLength":4096}}}`)}
}

func NewSaver(store SavingStore, auth SaveAuthority, sources SaveSources, writer MemoryWriter, clock memory.Clock, binding memory.Binding, target SaveTarget, allocate func(context.Context) (string, error)) (*Saver, error) {
	if store == nil || auth == nil || sources == nil || writer == nil || clock == nil || allocate == nil || binding.Token == "" || binding.Namespace == "" || binding.Subject == "" || binding.Location == "" || binding.Recipient != binding.Location || target.Collection == "" || target.PolicyRef == "" || target.Purpose == "" {
		return nil, memory.Invalid
	}
	return &Saver{store, auth, sources, writer, clock, binding, target, allocate}, nil
}

func (s *Saver) current(ctx context.Context, candidate string) (CandidateRecord, error) {
	r, err := s.store.Lookup(ctx, s.binding.Namespace, candidate)
	if err != nil {
		return r, err
	}
	if r.Subject != s.binding.Subject || r.Candidate.About != s.binding.Subject {
		return CandidateRecord{}, memory.Denied
	}
	now, err := s.clock.Now()
	if err != nil {
		return CandidateRecord{}, memory.Unavailable
	}
	contains := func(xs []string, v string) bool {
		for _, x := range xs {
			if x == v {
				return true
			}
		}
		return false
	}
	if r.Restrictions.RetainUntil <= now.Unix() || !contains(r.Restrictions.Storage, s.binding.Location) || !contains(r.Restrictions.Processing, s.binding.Location) || !contains(r.Restrictions.Purposes, s.target.Purpose) {
		return CandidateRecord{}, memory.Denied
	}
	if err = s.auth.Check(ctx, s.binding, "save"); err != nil {
		return CandidateRecord{}, err
	}
	refs := make([]*wire.ContentSource, 0, len(r.Candidate.Sources))
	for _, source := range r.Candidate.Sources {
		if source == nil || source.Ref == nil {
			return CandidateRecord{}, memory.Invalid
		}
		refs = append(refs, proto.Clone(source.Ref).(*wire.ContentSource))
	}
	if err = s.sources.Validate(ctx, refs, r.Restrictions); err != nil {
		return CandidateRecord{}, err
	}
	return r, nil
}

// Save runs one bounded attempt within a qualified host task. It never creates
// a task/scan loop. A durable pending intent fixes timestamps, payload and the
// original Memory operation before Put, including when its reply is lost.
func (s *Saver) Save(ctx context.Context, candidate string) (memory.OperationState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	intent, err := s.store.LookupSave(ctx, s.binding.Namespace, s.binding.Subject, candidate)
	if err == memory.Missing {
		r, e := s.current(ctx, candidate)
		if e != nil {
			return memory.OperationState{}, e
		}
		now, e := s.clock.Now()
		if e != nil {
			return memory.OperationState{}, memory.Unavailable
		}
		id, e := s.allocate(ctx)
		if e != nil {
			return memory.OperationState{}, e
		}
		body, e := json.Marshal(map[string]string{"attribute": r.Candidate.Attribute, "value": r.Candidate.Value})
		if e != nil {
			return memory.OperationState{}, memory.Invalid
		}
		definition := SavedSchema()
		write := &wire.MemoryWrite{OperationId: id, Ref: &wire.MemoryRef{Namespace: s.binding.Namespace, Collection: s.target.Collection, Key: candidate}, Spec: &wire.MemorySpec{Kind: r.Candidate.Kind, About: r.Candidate.About, Conditions: r.Candidate.Conditions, Sources: r.Candidate.Sources, Confidence: r.Candidate.Confidence, RecordedAt: now.Unix(), RetainUntil: r.Restrictions.RetainUntil, PolicyRef: s.target.PolicyRef, Purpose: s.target.Purpose, Content: &wire.DynamicPayload{TypeName: definition.Type, SchemaId: definition.ID, SchemaVersion: definition.Version, SchemaDigest: schema.Digest(definition.Document), Json: body}}}
		// A concurrent winner or uncertain journal reply must be recovered from
		// the original association; the newly allocated identity is never used.
		// The journal persists another controlled copy of the candidate body.
		// Existing Memory save permission does not authorize that retention.
		if e = s.auth.Check(ctx, s.binding, "retain"); e != nil {
			return memory.OperationState{}, e
		}
		e = s.store.ReserveSave(ctx, s.binding.Namespace, s.binding.Subject, candidate, write)
		if e != nil && e != memory.IdentityConflict && e != memory.Unavailable {
			return memory.OperationState{}, e
		}
		intent, err = s.store.LookupSave(ctx, s.binding.Namespace, s.binding.Subject, candidate)
	}
	if err != nil {
		return memory.OperationState{}, err
	}
	state, err := s.memory.InspectOperation(ctx, s.binding, intent.Request.OperationId)
	if err != nil {
		return memory.OperationState{}, err
	}
	if state.State == "committed" || state.State == "unknown" || state.State == "admission_expired" {
		return state, nil
	}
	if state.State != "not_admitted" {
		return memory.OperationState{}, memory.Unavailable
	}
	if intent.State != "pending" {
		return state, memory.ReplayUnavailable
	}
	if _, err = s.current(ctx, candidate); err != nil {
		return state, err
	}
	// Recheck the durable retirement fence immediately before the write. Memory
	// independently rechecks its own current authorization and all source rules.
	latest, err := s.store.LookupSave(ctx, s.binding.Namespace, s.binding.Subject, candidate)
	if err != nil {
		return state, err
	}
	if latest.State != "pending" || !proto.Equal(latest.Request, intent.Request) {
		return state, memory.ReplayUnavailable
	}
	_, writeErr := s.memory.Put(ctx, s.binding, proto.Clone(intent.Request).(*wire.MemoryWrite))
	state, err = s.memory.InspectOperation(ctx, s.binding, intent.Request.OperationId)
	if err != nil {
		return memory.OperationState{}, err
	}
	if state.State == "committed" {
		return state, nil
	}
	if writeErr != nil {
		return state, writeErr
	}
	return state, memory.Unavailable
}
