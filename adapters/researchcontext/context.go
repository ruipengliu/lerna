// Package researchcontext combines host-selected discovery, page evidence and
// observed failures for one task decision without upgrading their trust roles.
package researchcontext

import (
	"context"
	"encoding/json"
	"lerna/adapters/fetchcontext"
	"lerna/adapters/searchcontext"
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

func New(pages fetchcontext.Evidence, search searchcontext.Evidence, failures fetchcontext.FailureEvidence, task tasks.Task, location string, refs References) (*Context, error) {
	all := append(append(append([]string(nil), refs.Search...), refs.Pages...), refs.Failures...)
	if len(all) < 1 || len(all) > 8 {
		return nil, contextassembly.Invalid
	}
	seen := map[string]bool{}
	for _, ref := range all {
		if seen[ref] {
			return nil, contextassembly.Invalid
		}
		seen[ref] = true
	}
	c := &Context{}
	if len(refs.Search) > 0 {
		constructor := searchcontext.New
		if refs.AnswerFromSearch {
			constructor = searchcontext.NewForAnswer
		}
		part, err := constructor(search, task, location, refs.Search)
		if err != nil {
			return nil, err
		}
		c.parts = append(c.parts, part)
	}
	if len(refs.Pages)+len(refs.Failures) > 0 {
		part, err := fetchcontext.NewWithFailures(pages, failures, task, location, refs.Pages, refs.Failures)
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
		raw, err := json.Marshal(out)
		if err != nil || len(raw) > limit {
			return brain.Input{}, contextassembly.BudgetExceeded
		}
	}
	// Each owned part validates at the end of Assemble. With one part that
	// already is the complete context; repeating it adds no newer assembly
	// boundary. With multiple parts, recheck earlier parts after assembling
	// later ones so revocation during the combined assembly is still detected.
	if len(c.parts) > 1 {
		if err := c.Validate(ctx, task, location); err != nil {
			return brain.Input{}, err
		}
	}
	return out, nil
}
