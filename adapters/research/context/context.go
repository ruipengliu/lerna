// Package context combines host-selected discovery, page evidence and
// observed failures for one task decision without upgrading their trust roles.
package context

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"time"
)

// References are fixed by the host from original execution outcomes. A model or
// search result cannot promote a discovery response into the Pages collection.
type References struct {
	Search, Pages, Failures []string
	AnswerFromSearch        bool
}
type Context struct{ parts []brain.Context }

var _ brain.Context = (*Context)(nil)

func New(pages PageEvidence, search SearchEvidence, failures FailureEvidence, task tasks.Task, location string, refs References) (*Context, error) {
	all := append(append(append([]string(nil), refs.Search...), refs.Pages...), refs.Failures...)
	if _, err := bindContext(task, location, all); err != nil {
		return nil, err
	}
	c := &Context{}
	if len(refs.Search) > 0 {
		constructor := NewSearch
		if refs.AnswerFromSearch {
			constructor = NewSearchForAnswer
		}
		part, err := constructor(search, task, location, refs.Search)
		if err != nil {
			return nil, err
		}
		c.parts = append(c.parts, part)
	}
	if len(refs.Pages)+len(refs.Failures) > 0 {
		part, err := NewPagesWithFailures(pages, failures, task, location, refs.Pages, refs.Failures)
		if err != nil {
			return nil, err
		}
		c.parts = append(c.parts, part)
	}
	return c, nil
}
func (c *Context) Validate(ctx context.Context, task tasks.Task, location string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, part := range c.parts {
		if err := part.Validate(ctx, task, location); err != nil {
			return err
		}
	}
	return nil
}
func (c *Context) Assemble(ctx context.Context, task tasks.Task, location string, limit int) (brain.Input, error) {
	if limit < 1 || limit > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out brain.Input
	for i, part := range c.parts {
		input, err := part.Assemble(ctx, task, location, limit)
		if err != nil {
			return brain.Input{}, err
		}
		if i == 0 {
			out.Goal = input.Goal
			out.Constraints = input.Constraints
		} else if input.Goal != out.Goal || input.Constraints != out.Constraints {
			return brain.Input{}, contextassembly.Invalidated
		}
		out.Blocks = append(out.Blocks, input.Blocks...)
		if err := checkSize(out, limit); err != nil {
			return brain.Input{}, err
		}
	}
	return finishAssembly(ctx, c, task, location, out, limit, len(c.parts))
}

// boundContext fixes task facts and processing location for each projection.
// ContextAssembler still owns decision qualification and retention.
type boundContext struct {
	facts             [32]byte
	location, subject string
}

func fingerprint(t tasks.Task) [32]byte {
	raw, _ := json.Marshal(struct {
		Ref                                           tasks.Ref
		Subject, Resource, Goal, GoalRef, GuidanceRef string
		Constraints                                   tasks.Constraints
		Inputs                                        []string
		Facts                                         []tasks.InputFact
	}{t.Ref, t.Subject, t.Resource, t.Goal, t.GoalRef, t.GuidanceRef, t.Constraints, t.InputRefs, t.InputFacts})
	return sha256.Sum256(raw)
}

func bindContext(t tasks.Task, location string, refs []string) (boundContext, error) {
	if t.Ref.Namespace == "" || t.Ref.TaskID == "" || t.Subject == "" || location == "" || len(location) > 256 || len(refs) < 1 || len(refs) > 8 {
		return boundContext{}, contextassembly.Invalid
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		id, err := answers.ParseReference(ref)
		if err != nil || id.Namespace != t.Ref.Namespace || seen[ref] {
			return boundContext{}, contextassembly.Invalid
		}
		seen[ref] = true
	}
	return boundContext{facts: fingerprint(t), location: location, subject: t.Subject}, nil
}

func (c boundContext) check(t tasks.Task, location string) error {
	if location != c.location || fingerprint(t) != c.facts {
		return contextassembly.Invalidated
	}
	return nil
}

func taskInput(t tasks.Task) brain.Input {
	constraints, _ := json.Marshal(t.Constraints)
	return brain.Input{Goal: t.Goal, Constraints: string(constraints)}
}

func checkSize(in brain.Input, limit int) error {
	encoded, err := json.Marshal(in)
	if err != nil || len(encoded) > limit {
		return contextassembly.BudgetExceeded
	}
	return nil
}

// A single read/part already ends at its current authority check. Multiple
// sources or parts must revalidate together after the last one is assembled.
// Preserve this grouping: every additional read consumes the original budget.
func finishAssembly(ctx context.Context, source brain.Context, t tasks.Task, location string, in brain.Input, limit, parts int) (brain.Input, error) {
	if err := checkSize(in, limit); err != nil {
		return brain.Input{}, err
	}
	if parts > 1 {
		if err := source.Validate(ctx, t, location); err != nil {
			return brain.Input{}, err
		}
	}
	return in, nil
}
