package contextassembly

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"lerna/brain"
	"sort"
	"time"
)

const (
	Denied         Error = "PERMISSION_DENIED"
	Invalidated    Error = "CONTEXT_INVALIDATED"
	BudgetExceeded Error = "INPUT_BUDGET_EXCEEDED"
)

type Reference struct {
	Namespace, Collection, Key string
	Revision                   uint64
}
type Candidate struct {
	Reference Reference
	Required  bool
}

// Request comes from a trusted task host. Providers must check its subject,
// facts and policy versions against authority, not treat these as credentials.
type Request struct {
	Key                                                Key
	Subject, Purpose, Location, Storage, PolicyVersion string
	FactsVersion                                       uint64
	MaxBytes                                           int
	Candidates                                         []Candidate
}
type Source struct {
	Block                brain.Block
	Applicable, CanStore bool
	// Claim and Value are trusted schema projections for conflict detection.
	// They are never persisted; only source references describe conflicts.
	Claim, Value string
}
type Facts interface {
	Load(context.Context, Request) (brain.Input, error)
	Validate(context.Context, Request) error
}

// Memories enforces current revision, applicability, schema, processing,
// disclosure and metadata retention. retained additionally requires permission
// to store the body at Request.Storage. References confer no authority.
// Missing may be returned only if recording the requested missing reference
// is authorized. Denied forbids binding even an optional missing reference.
type Memories interface {
	Load(context.Context, Request, Reference) (Source, error)
	Validate(context.Context, Request, Reference, bool) error
}
type Conflict struct{ Left, Right Reference }
type Status struct {
	Missing, Inapplicable, Trimmed []Reference
	Conflicts                      []Conflict
}
type Result struct {
	Input  brain.Input
	Status Status
}
type dependency struct {
	Candidate          Candidate
	Fingerprint, State string
	Stored             *brain.Block `json:",omitempty"`
}
type document struct {
	PolicyBound  bool `json:",omitempty"`
	FactsSHA256  string
	Dependencies []dependency
	Status       Status
}
type Assembler struct {
	store    Store
	facts    Facts
	memories Memories
	policy   SnapshotPolicy
}

func New(store Store, facts Facts, memories Memories) (*Assembler, error) {
	if store == nil || facts == nil || memories == nil {
		return nil, Invalid
	}
	return &Assembler{store: store, facts: facts, memories: memories}, nil
}
func label(s string) bool { return len(s) > 0 && len(s) <= 256 }
func validReference(r Reference) bool {
	return label(r.Namespace) && label(r.Collection) && label(r.Key) && r.Revision > 0 && r.Revision <= 1<<32
}
func validRequest(r Request) bool {
	if !label(r.Key.Namespace) || !label(r.Key.TaskID) || r.Key.Decision == 0 || r.Key.Decision > 1<<32 || !label(r.Subject) || !label(r.Purpose) || !label(r.Location) || !label(r.Storage) || !label(r.PolicyVersion) || r.FactsVersion == 0 || r.MaxBytes < 1 || r.MaxBytes > brain.MaxInputBytes || len(r.Candidates) > 16 {
		return false
	}
	seen := map[Reference]bool{}
	for _, c := range r.Candidates {
		if !validReference(c.Reference) || c.Reference.Namespace != r.Key.Namespace || seen[c.Reference] {
			return false
		}
		seen[c.Reference] = true
	}
	return true
}
func encode(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, Invalid
	}
	return b, nil
}
func digest(v any) string               { b, _ := encode(v); h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func fits(in brain.Input, max int) bool { b, e := encode(in); return e == nil && len(b) <= max }
func validSource(s Source) bool {
	return label(s.Block.Ref) && len(s.Block.Text) <= brain.MaxInputBytes && label(s.Block.Subject) && s.Block.Role == "memory" && len(s.Block.ReplyTo) == 0 && len(s.Claim) <= 256 && len(s.Value) <= 4096
}
func fingerprint(s Source) string {
	// Retention permission is checked separately on every release.
	s.CanStore = false
	return digest(s)
}
func (a *Assembler) base(ctx context.Context, r Request) (brain.Input, error) {
	if e := a.facts.Validate(ctx, r); e != nil {
		return brain.Input{}, e
	}
	in, e := a.facts.Load(ctx, r)
	if e != nil {
		return brain.Input{}, e
	}
	if len(in.Blocks) > 16 || !fits(in, r.MaxBytes) {
		return brain.Input{}, BudgetExceeded
	}
	return in, nil
}
func normalize(r Request) Request {
	r.Candidates = append([]Candidate(nil), r.Candidates...)
	sort.SliceStable(r.Candidates, func(i, j int) bool { return r.Candidates[i].Required && !r.Candidates[j].Required })
	return r
}
func (a *Assembler) Assemble(ctx context.Context, r Request) (result Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// A dependency can fail closed as Denied when its I/O is interrupted.
	// Once our own attempt is cancelled, that is not a usable policy verdict.
	// Preserve any durable original snapshot but release no context body.
	defer func() {
		if ctx.Err() != nil {
			result, err = Result{}, Unavailable
		}
	}()
	if !validRequest(r) {
		return Result{}, Invalid
	}
	r = normalize(r)
	semantic := digest(r)
	original, e := a.store.Read(ctx, r.Key)
	if e == nil {
		return a.release(ctx, r, semantic, original)
	}
	if e != Missing {
		return Result{}, e
	}
	base, e := a.base(ctx, r)
	if e != nil {
		return Result{}, e
	}
	doc := document{FactsSHA256: digest(base)}
	in := base
	type claim struct {
		value string
		ref   Reference
	}
	claims := map[string][]claim{}
	for _, c := range r.Candidates {
		src, e := a.memories.Load(ctx, r, c.Reference)
		if e != nil {
			if e != Missing {
				return Result{}, e
			}
			if c.Required {
				return Result{}, Missing
			}
			doc.Status.Missing = append(doc.Status.Missing, c.Reference)
			doc.Dependencies = append(doc.Dependencies, dependency{Candidate: c, State: "missing"})
			continue
		}
		if !validSource(src) {
			return Result{}, Invalid
		}
		dep := dependency{Candidate: c, Fingerprint: fingerprint(src), State: "selected"}
		if !src.Applicable {
			if c.Required {
				return Result{}, Missing
			}
			dep.State = "inapplicable"
			doc.Status.Inapplicable = append(doc.Status.Inapplicable, c.Reference)
		} else {
			if src.Claim != "" {
				for _, old := range claims[src.Claim] {
					if old.value != src.Value {
						doc.Status.Conflicts = append(doc.Status.Conflicts, Conflict{old.ref, c.Reference})
					}
				}
				claims[src.Claim] = append(claims[src.Claim], claim{src.Value, c.Reference})
			}
			in.Blocks = append(in.Blocks, src.Block)
			if !fits(in, r.MaxBytes) {
				in.Blocks = in.Blocks[:len(in.Blocks)-1]
				if c.Required {
					return Result{}, BudgetExceeded
				}
				dep.State = "trimmed"
				doc.Status.Trimmed = append(doc.Status.Trimmed, c.Reference)
			} else if src.CanStore {
				block := src.Block
				dep.Stored = &block
			}
		}
		doc.Dependencies = append(doc.Dependencies, dep)
	}
	// Bind policy before the snapshot. Cleanup can retire this decision even
	// before Bind finishes, fencing a late write after authority invalidation.
	if a.policy != nil {
		deps := make([]Dependency, 0, len(doc.Dependencies))
		for _, dep := range doc.Dependencies {
			deps = append(deps, Dependency{Reference: dep.Candidate.Reference, State: dep.State, Retained: dep.Stored != nil})
		}
		if e := a.policy.PrepareSnapshot(ctx, r, deps); e != nil {
			return Result{}, e
		}
		doc.PolicyBound = true
	}
	raw, e := encode(doc)
	if e != nil || len(raw) > 65536 {
		return Result{}, BudgetExceeded
	}
	// Both source metadata and any retained body must be authorized before Bind.
	if e = a.validate(ctx, r, doc); e != nil {
		return Result{}, e
	}
	bound, e := a.store.Bind(ctx, Snapshot{Key: r.Key, Subject: r.Subject, SemanticSHA256: semantic, Document: raw})
	if e != nil {
		return Result{}, e
	}
	return a.release(ctx, r, semantic, bound)
}
func parse(raw []byte) (document, error) {
	var d document
	if len(raw) > 65536 {
		return d, Invalidated
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil {
		return d, Invalidated
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return d, Invalidated
	}
	return d, nil
}
func (a *Assembler) validate(ctx context.Context, r Request, d document) error {
	if len(d.Dependencies) != len(r.Candidates) || len(d.Status.Conflicts) > 120 {
		return Invalidated
	}
	if e := a.facts.Validate(ctx, r); e != nil {
		return e
	}
	for i, dep := range d.Dependencies {
		if dep.Candidate != r.Candidates[i] {
			return Invalidated
		}
		switch dep.State {
		case "missing":
			if dep.Stored != nil || dep.Candidate.Required {
				return Invalidated
			}
			if e := a.memories.Validate(ctx, r, dep.Candidate.Reference, false); e != Missing {
				if e != nil {
					return e
				}
				return Invalidated
			}
			continue
		case "inapplicable", "trimmed":
			if dep.Stored != nil || dep.Candidate.Required {
				return Invalidated
			}
		case "selected":
		default:
			return Invalidated
		}
		if e := a.memories.Validate(ctx, r, dep.Candidate.Reference, dep.Stored != nil); e != nil {
			return e
		}
	}
	return nil
}
func (a *Assembler) release(ctx context.Context, r Request, semantic string, s Snapshot) (Result, error) {
	if s.Key != r.Key || s.Subject != r.Subject || s.SemanticSHA256 != semantic {
		return Result{}, IdentityConflict
	}
	doc, e := parse(s.Document)
	if e != nil {
		return Result{}, e
	}
	if e = a.validate(ctx, r, doc); e != nil {
		return Result{}, e
	}
	if e = a.validatePolicy(ctx, r, doc); e != nil {
		return Result{}, e
	}
	base, e := a.base(ctx, r)
	if e != nil {
		return Result{}, e
	}
	if digest(base) != doc.FactsSHA256 {
		return Result{}, Invalidated
	}
	out := Result{Input: base, Status: doc.Status}
	for _, dep := range doc.Dependencies {
		if dep.State != "selected" {
			continue
		}
		var block brain.Block
		if dep.Stored != nil {
			block = *dep.Stored
		} else {
			src, e := a.memories.Load(ctx, r, dep.Candidate.Reference)
			if e != nil {
				return Result{}, e
			}
			if !validSource(src) || !src.Applicable || fingerprint(src) != dep.Fingerprint {
				return Result{}, Invalidated
			}
			block = src.Block
		}
		out.Input.Blocks = append(out.Input.Blocks, block)
	}
	if len(out.Input.Blocks) > 32 || !fits(out.Input, r.MaxBytes) {
		return Result{}, BudgetExceeded
	}
	if e = a.validate(ctx, r, doc); e != nil {
		return Result{}, e
	}
	if e = a.validatePolicy(ctx, r, doc); e != nil {
		return Result{}, e
	}
	return out, nil
}

// Validate reconstructs the original authorized context without rebinding it.
func (a *Assembler) Validate(ctx context.Context, r Request) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if ctx.Err() != nil {
			err = Unavailable
		}
	}()
	if !validRequest(r) {
		return Invalid
	}
	r = normalize(r)
	s, e := a.store.Read(ctx, r.Key)
	if e != nil {
		return e
	}
	_, e = a.release(ctx, r, digest(r), s)
	return e
}
