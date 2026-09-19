// Package task binds context assembly to authenticated Core facts.
package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"time"
)

type Core interface {
	Current(context.Context, tasks.Ref) (tasks.RunSnapshot, error)
	CheckDecision(context.Context, tasks.Qualification) error
}
type Content interface {
	Assemble(context.Context, tasks.Task, string, int) (brain.Input, error)
	Validate(context.Context, tasks.Task, string) error
}
type Clock interface{ Now() (time.Time, error) }
type Scope struct{ Purpose, Location, Storage, PolicyVersion string }
type Facts struct {
	core     Core
	content  Content
	clock    Clock
	baseline tasks.RunSnapshot
	scope    Scope
}

func text(s string) bool { return len(s) > 0 && len(s) <= 256 }
func NewFacts(core Core, content Content, clock Clock, baseline tasks.RunSnapshot, scope Scope) (*Facts, error) {
	if core == nil || content == nil || clock == nil || !text(scope.Purpose) || !text(scope.Location) || !text(scope.Storage) || !text(scope.PolicyVersion) || len(baseline.Work) != 1 || baseline.UpdateVersion == ^uint64(0) {
		return nil, contextassembly.Invalid
	}
	// Own the baseline so later host mutations cannot alter the frozen facts.
	raw, e := json.Marshal(baseline)
	if e != nil || len(raw) > 1048576 {
		return nil, contextassembly.Invalid
	}
	var owned tasks.RunSnapshot
	if json.Unmarshal(raw, &owned) != nil {
		return nil, contextassembly.Invalid
	}
	return &Facts{core, content, clock, owned, scope}, nil
}
func factHash(t tasks.Task) [32]byte {
	// Version, usage and control are not user facts; they have separate checks.
	raw, _ := json.Marshal(struct {
		Ref                                           tasks.Ref
		Subject, Resource, Goal, GoalRef, GuidanceRef string
		Constraints                                   tasks.Constraints
		Inputs                                        []string
		Facts                                         []tasks.InputFact
	}{t.Ref, t.Subject, t.Resource, t.Goal, t.GoalRef, t.GuidanceRef, t.Constraints, t.InputRefs, t.InputFacts})
	return sha256.Sum256(raw)
}
func (f *Facts) request(r contextassembly.Request) bool {
	b := f.baseline
	return r.Key.Namespace == b.Task.Ref.Namespace && r.Key.TaskID == b.Task.Ref.TaskID && r.Subject == b.Task.Subject && r.FactsVersion == b.UpdateVersion+1 && r.Purpose == f.scope.Purpose && r.Location == f.scope.Location && r.Storage == f.scope.Storage && r.PolicyVersion == f.scope.PolicyVersion && r.MaxBytes > 0 && r.MaxBytes <= brain.MaxInputBytes
}
func (f *Facts) Validate(ctx context.Context, r contextassembly.Request) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !f.request(r) {
		return contextassembly.Denied
	}
	current, e := f.core.Current(ctx, f.baseline.Task.Ref)
	if e != nil {
		return e
	}
	if e = f.core.CheckDecision(ctx, tasks.QualificationOf(current)); e != nil {
		return e
	}
	now, e := f.clock.Now()
	if e != nil {
		return contextassembly.Unavailable
	}
	b := f.baseline
	if len(current.Work) != 1 || current.Task.State != "RUNNING" || (current.Task.Control.Intent != "" && current.Task.Control.Intent != "RUN") || current.Task.Owner != b.Task.Owner || current.Task.OwnerEpoch != b.Task.OwnerEpoch || current.UpdateVersion != b.UpdateVersion || factHash(current.Task) != factHash(b.Task) || current.Task.Constraints.DeadlineUnix <= now.Unix() {
		return contextassembly.Invalidated
	}
	w, old := current.Work[0], b.Work[0]
	if w.ID != old.ID || w.Worker != old.Worker || w.Generation != old.Generation || !w.InFlight || w.Done || w.LeaseUntil <= now.UnixNano() {
		return contextassembly.Invalidated
	}
	return f.content.Validate(ctx, b.Task, r.Location)
}
func (f *Facts) Load(ctx context.Context, r contextassembly.Request) (brain.Input, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e := f.Validate(ctx, r); e != nil {
		return brain.Input{}, e
	}
	out, e := f.content.Assemble(ctx, f.baseline.Task, r.Location, r.MaxBytes)
	if e != nil {
		return brain.Input{}, e
	}
	if e = f.Validate(ctx, r); e != nil {
		return brain.Input{}, e
	}
	return out, nil
}

var _ contextassembly.Facts = (*Facts)(nil)
