package taskcontext

import (
	"context"
	"lerna/answers"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"time"
)

// DerivedSources validates a new output's storage independently of the context
// snapshot. It must preserve the original read identity and exact revision.
type DerivedSources interface {
	Derive(context.Context, contextassembly.Request, contextassembly.Dependency, string) (*wire.ContentSource, int64, error)
}
type dependencyAssembly interface {
	Dependencies(context.Context, contextassembly.Request) ([]contextassembly.Dependency, error)
}
type lineage struct {
	session  *Session
	assembly dependencyAssembly
	sources  DerivedSources
}

// Lineage binds output governance to the same immutable decision as the model.
// The artifact host must also install the matching dynamic source resolver.
func (s *Session) Lineage(sources DerivedSources) (answers.LineageProvider, error) {
	if s == nil || sources == nil {
		return nil, contextassembly.Invalid
	}
	a, ok := s.assembly.(dependencyAssembly)
	if !ok {
		return nil, contextassembly.Invalid
	}
	return &lineage{s, a, sources}, nil
}
func (l *lineage) Sources(ctx context.Context, task tasks.Task, storage string) (answers.Lineage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if storage == "" || len(storage) > 256 {
		return answers.Lineage{}, contextassembly.Invalid
	}
	s := l.session
	if e := s.check(task, s.request.Location); e != nil {
		return answers.Lineage{}, e
	}
	deps, e := l.assembly.Dependencies(ctx, s.request)
	if e != nil {
		return answers.Lineage{}, e
	}
	if len(deps) > 16 {
		return answers.Lineage{}, contextassembly.BudgetExceeded
	}
	out := answers.Lineage{}
	for _, dep := range deps {
		source, until, e := l.sources.Derive(ctx, s.request, dep, storage)
		if e != nil {
			return answers.Lineage{}, e
		}
		if source == nil || source.Kind == "" || source.Key == "" || source.Revision == 0 || until <= 0 {
			return answers.Lineage{}, contextassembly.Invalidated
		}
		out.Sources = append(out.Sources, source)
		if out.RetainUntil == 0 || until < out.RetainUntil {
			out.RetainUntil = until
		}
	}
	if e = s.assembly.Validate(ctx, s.request); e != nil {
		return answers.Lineage{}, e
	}
	return out, nil
}
