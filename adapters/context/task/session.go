package task

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
)

type Assembly interface {
	Assemble(context.Context, contextassembly.Request) (contextassembly.Result, error)
	Validate(context.Context, contextassembly.Request) error
}

// Session binds Brain.Context to one Core decision. The host restores the same
// Request and grant binding after interruption; it must not silently replace it.
type Session struct {
	assembly Assembly
	request  contextassembly.Request
	facts    [32]byte
}

func NewSession(a Assembly, r contextassembly.Request, task tasks.Task) (*Session, error) {
	if a == nil || r.Key.Namespace != task.Ref.Namespace || r.Key.TaskID != task.Ref.TaskID || r.Subject != task.Subject || r.Key.Decision == 0 || r.FactsVersion == 0 || r.MaxBytes < 1 || r.MaxBytes > brain.MaxInputBytes || len(r.Candidates) > 16 {
		return nil, contextassembly.Invalid
	}
	r.Candidates = append([]contextassembly.Candidate(nil), r.Candidates...)
	return &Session{a, r, factHash(task)}, nil
}
func (s *Session) check(t tasks.Task, location string) error {
	if location != s.request.Location || t.Ref.Namespace != s.request.Key.Namespace || t.Ref.TaskID != s.request.Key.TaskID || t.Subject != s.request.Subject {
		return contextassembly.Denied
	}
	if factHash(t) != s.facts {
		return contextassembly.Invalidated
	}
	return nil
}
func (s *Session) Assemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	if e := s.check(t, location); e != nil {
		return brain.Input{}, e
	}
	if max < s.request.MaxBytes || max > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	out, e := s.assembly.Assemble(ctx, s.request)
	if e != nil {
		return brain.Input{}, e
	}
	if len(out.Status.Missing)+len(out.Status.Inapplicable)+len(out.Status.Trimmed)+len(out.Status.Conflicts) > 0 {
		raw, e := json.Marshal(out.Status)
		if e != nil {
			return brain.Input{}, contextassembly.Invalidated
		}
		for _, block := range out.Input.Blocks {
			if block.Ref == "context-status" {
				return brain.Input{}, contextassembly.Invalidated
			}
		}
		out.Input.Blocks = append(out.Input.Blocks, brain.Block{Ref: "context-status", Text: string(raw), Subject: t.Subject, Role: "context-status"})
	}
	raw, e := json.Marshal(out.Input)
	if e != nil || len(raw) > max {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	if e = s.assembly.Validate(ctx, s.request); e != nil {
		return brain.Input{}, e
	}
	return out.Input, nil
}
func (s *Session) Validate(ctx context.Context, t tasks.Task, location string) error {
	if e := s.check(t, location); e != nil {
		return e
	}
	return s.assembly.Validate(ctx, s.request)
}

var _ brain.Context = (*Session)(nil)
