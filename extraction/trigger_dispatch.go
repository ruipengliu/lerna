package extraction

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"time"
)

type TriggerState interface {
	TriggerStore
	TriggerRounds
	SourceFence
}
type TriggerTaskHost interface {
	Submit(context.Context, string, tasks.Submission) (tasks.Task, error)
}
type TriggerInputs interface {
	Prepare(context.Context, TriggerRecord, *wire.ContentSource, string) (tasks.Submission, error)
}
type TriggerDispatcher struct {
	store    TriggerState
	auth     SaveAuthority
	clock    memory.Clock
	binding  memory.Binding
	core     TriggerTaskHost
	inputs   TriggerInputs
	allocate func(context.Context) (string, error)
}

func NewTriggerDispatcher(store TriggerState, auth SaveAuthority, clock memory.Clock, b memory.Binding, core TriggerTaskHost, inputs TriggerInputs, allocate func(context.Context) (string, error)) (*TriggerDispatcher, error) {
	if store == nil || auth == nil || clock == nil || core == nil || inputs == nil || allocate == nil || b.Token == "" || b.Namespace == "" || b.Subject == "" || b.Location == "" || b.Recipient != b.Location {
		return nil, memory.Invalid
	}
	return &TriggerDispatcher{store, auth, clock, b, core, inputs, allocate}, nil
}

// Dispatch is one bounded host event, never an autonomous scan loop. Persist
// the exact submission before Core admission; all retries use that request.
// Preparing controlled metadata can leave an unused expiring input if another
// dispatcher wins. It must not read source bodies or perform business actions.
func (d *TriggerDispatcher) Dispatch(ctx context.Context, id string, event *wire.ContentSource) (tasks.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if event == nil || event.Kind == "" || event.Key == "" || event.Revision == 0 {
		return tasks.Task{}, memory.Invalid
	}
	event = proto.Clone(event).(*wire.ContentSource)
	guard, err := NewTriggerGuard(d.store, d.auth, d.clock, d.binding, id)
	if err != nil {
		return tasks.Task{}, err
	}
	r, err := guard.current(ctx, d.binding)
	if err != nil {
		return tasks.Task{}, err
	}
	allowed := false
	for _, source := range r.Spec.Sources {
		if source.Kind == event.Kind && source.Key == event.Key {
			allowed = true
			break
		}
	}
	if !allowed {
		return tasks.Task{}, memory.Denied
	}
	if err = d.store.CheckSource(ctx, d.binding.Namespace, event); err != nil {
		return tasks.Task{}, err
	}
	round, err := d.store.GetTriggerRound(ctx, d.binding.Namespace, d.binding.Subject, id, event)
	if err == memory.Missing {
		op, e := d.allocate(ctx)
		if e != nil {
			return tasks.Task{}, e
		}
		submission, e := d.inputs.Prepare(ctx, r, proto.Clone(event).(*wire.ContentSource), op)
		if e != nil {
			return tasks.Task{}, e
		}
		if submission.OperationID != op || submission.Namespace != d.binding.Namespace {
			return tasks.Task{}, memory.Invalid
		}
		now, e := d.clock.Now()
		if e != nil {
			return tasks.Task{}, memory.Unavailable
		}
		e = d.store.ReserveTriggerRound(ctx, d.binding.Namespace, d.binding.Subject, id, now.Unix(), TriggerRound{Event: event, Submission: submission})
		if e != nil && e != memory.IdentityConflict && e != memory.Unavailable {
			return tasks.Task{}, e
		}
		round, err = d.store.GetTriggerRound(ctx, d.binding.Namespace, d.binding.Subject, id, event)
	}
	if err != nil {
		return tasks.Task{}, err
	}
	if _, err = guard.current(ctx, d.binding); err != nil {
		return tasks.Task{}, err
	}
	if err = d.store.CheckSource(ctx, d.binding.Namespace, event); err != nil {
		return tasks.Task{}, err
	}
	return d.core.Submit(ctx, d.binding.Token, round.Submission)
}

// TriggerGuard combines current continuous-scan authority with each ordinary
// phase permission. Hosts install it on the extraction driver and saver; Core
// still checks task qualification. A registration is never a reusable permit.
type TriggerGuard struct {
	store   TriggerStore
	auth    SaveAuthority
	clock   memory.Clock
	binding memory.Binding
	id      string
}

// TriggerID identifies host configuration; it is not an authorization result.
func (g *TriggerGuard) TriggerID() string { return g.id }

func NewTriggerGuard(store TriggerStore, auth SaveAuthority, clock memory.Clock, b memory.Binding, id string) (*TriggerGuard, error) {
	if store == nil || auth == nil || clock == nil || id == "" || len(id) > 256 || b.Namespace == "" || b.Subject == "" || b.Location == "" || b.Token == "" || b.Recipient != b.Location {
		return nil, memory.Invalid
	}
	return &TriggerGuard{store, auth, clock, b, id}, nil
}
func (g *TriggerGuard) current(ctx context.Context, b memory.Binding) (TriggerRecord, error) {
	if b.Namespace != g.binding.Namespace || b.Subject != g.binding.Subject || b.Location != g.binding.Location || b.Recipient != g.binding.Recipient {
		return TriggerRecord{}, memory.Denied
	}
	if err := g.auth.Check(ctx, b, "scan"); err != nil {
		return TriggerRecord{}, err
	}
	r, err := g.store.GetTrigger(ctx, b.Namespace, b.Subject, g.id)
	if err != nil {
		return TriggerRecord{}, err
	}
	now, err := g.clock.Now()
	if err != nil {
		return TriggerRecord{}, memory.Unavailable
	}
	if r.State != "active" || r.Spec.ExpiresUnix <= now.Unix() || r.Location != b.Location {
		return TriggerRecord{}, memory.Denied
	}
	return r, nil
}
func (g *TriggerGuard) Check(ctx context.Context, b memory.Binding, phase string) error {
	if _, err := g.current(ctx, b); err != nil {
		return err
	}
	if phase == "scan" {
		return nil
	}
	return g.auth.Check(ctx, b, phase)
}
