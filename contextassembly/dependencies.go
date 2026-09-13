package contextassembly

import (
	"context"
	"time"
)

// Dependency describes the original governed use of a source. Metadata states
// are retained because absence, inapplicability and conflict can influence output
// too. A derived-output policy must not silently discard these dependencies.
type Dependency struct {
	Reference Reference
	State     string
	Retained  bool
}

// Dependencies returns only references and states from the validated immutable
// snapshot. It grants no storage/disclosure rights to a new derived object; that
// object's source-policy resolver must independently enforce its purpose,
// recipient, retention and current authorization.
func (a *Assembler) Dependencies(ctx context.Context, r Request) ([]Dependency, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !validRequest(r) {
		return nil, Invalid
	}
	r = normalize(r)
	original, e := a.store.Read(ctx, r.Key)
	if e != nil {
		return nil, e
	}
	if _, e = a.release(ctx, r, digest(r), original); e != nil {
		return nil, e
	}
	doc, e := parse(original.Document)
	if e != nil {
		return nil, e
	}
	out := make([]Dependency, 0, len(doc.Dependencies))
	for _, dep := range doc.Dependencies {
		out = append(out, Dependency{Reference: dep.Candidate.Reference, State: dep.State, Retained: dep.Stored != nil})
	}
	return out, nil
}
