package extractionexecution

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/execution"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"time"
)

type TriggerTasks interface {
	LookupOperation(context.Context, string, string, string) (tasks.Task, error)
}
type Triggered struct {
	driver *Driver
	store  extraction.TriggerState
	core   TriggerTasks
	id     string
	event  *wire.ContentSource
}

// BindTrigger accepts only a phase-guarded local extraction driver and binds
// it to the original Core task/submission. Execution still supplies current
// task qualification and authentic controlled input bytes before calling it.
func BindTrigger(driver *Driver, store extraction.TriggerState, core TriggerTasks, id string, event *wire.ContentSource) (*Triggered, error) {
	if driver == nil || store == nil || core == nil || event == nil || event.Kind == "" || event.Key == "" || event.Revision == 0 {
		return nil, memory.Invalid
	}
	guard, ok := driver.auth.(*extraction.TriggerGuard)
	if !ok || guard.TriggerID() != id {
		return nil, memory.Invalid
	}
	return &Triggered{driver, store, core, id, proto.Clone(event).(*wire.ContentSource)}, nil
}

func (d *Triggered) binding(ctx context.Context, c execution.Call) (tasks.Task, extraction.TriggerRound, error) {
	b := d.driver.binding
	round, err := d.store.GetTriggerRound(ctx, b.Namespace, b.Subject, d.id, d.event)
	if err != nil {
		return tasks.Task{}, round, err
	}
	task, err := d.core.LookupOperation(ctx, b.Token, b.Namespace, round.Submission.OperationID)
	if err != nil {
		return task, round, err
	}
	if c.Request.Qualification.Ref != task.Ref || len(round.Submission.InputRefs) != 1 || c.Request.InputRef != round.Submission.InputRefs[0] {
		return task, round, memory.Denied
	}
	return task, round, nil
}
func (d *Triggered) Start(ctx context.Context, c execution.Call) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	task, round, err := d.binding(ctx, c)
	if err != nil {
		return err
	}
	if task.Constraints.ModelRequests != 0 || task.Constraints.ModelTokens != 0 || task.Constraints.MaxSteps > round.Submission.Constraints.MaxSteps || task.Constraints.DeadlineUnix > round.Submission.Constraints.DeadlineUnix {
		return memory.Denied
	}
	if err = d.driver.auth.Check(ctx, d.driver.binding, "scan"); err != nil {
		return err
	}
	registration, err := d.store.GetTrigger(ctx, d.driver.binding.Namespace, d.driver.binding.Subject, d.id)
	if err != nil {
		return err
	}
	if registration.State != "active" || registration.Purpose != d.driver.purpose {
		return memory.Denied
	}
	sources, err := sourceInput(c.Input)
	if err != nil {
		return err
	}
	eventSeen := false
	for _, ref := range sources {
		scope := extraction.SourceScope{Kind: ref.Kind, Key: ref.Key}
		allowed := false
		for _, registered := range registration.Spec.Sources {
			if registered == scope {
				allowed = true
				break
			}
		}
		if !allowed {
			return memory.Denied
		}
		if proto.Equal(ref, d.event) {
			eventSeen = true
		}
		if err = d.store.CheckSource(ctx, d.driver.binding.Namespace, ref); err != nil {
			return err
		}
	}
	if !eventSeen {
		return memory.Denied
	}
	return d.driver.Start(ctx, c)
}
func (d *Triggered) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := d.binding(ctx, c); err != nil {
		return execution.Observation{}, err
	}
	// Inspection must preserve an existing effect fact after cancellation; the
	// underlying phase guard suppresses newly forbidden result disclosure.
	return d.driver.Inspect(ctx, c)
}
