package extraction

import (
	"context"
	"time"

	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
)

// CurrentSources is a trusted host adapter for current revision metadata. It
// must enforce source-specific disclosure/processing policy without reading
// source bodies. It is not a lossless event journal: intervening revisions may
// be missed between polls. Returning an older permitted revision when the
// latest is forbidden is not allowed.
type CurrentSources interface {
	Current(context.Context, SourceScope, string, string) (*wire.ContentSource, error)
}

type TriggerPoller struct {
	dispatcher *TriggerDispatcher
	sources    CurrentSources
}

func NewTriggerPoller(dispatcher *TriggerDispatcher, sources CurrentSources) (*TriggerPoller, error) {
	if dispatcher == nil || sources == nil {
		return nil, memory.Invalid
	}
	return &TriggerPoller{dispatcher, sources}, nil
}

// Poll handles one explicitly scheduled host wake for one registered source.
// It starts no goroutine/listener and reads no source body. Dispatch persists
// the original task submission and charges a revision only once, including
// after reconstruction of this host object or loss of the submission reply.
func (p *TriggerPoller) Poll(ctx context.Context, id string, scope SourceScope) (tasks.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d := p.dispatcher
	guard, err := NewTriggerGuard(d.store, d.auth, d.clock, d.binding, id)
	if err != nil {
		return tasks.Task{}, err
	}
	r, err := guard.current(ctx, d.binding)
	if err != nil {
		return tasks.Task{}, err
	}
	allowed := false
	for _, registered := range r.Spec.Sources {
		if registered == scope {
			allowed = true
			break
		}
	}
	if !allowed {
		return tasks.Task{}, memory.Denied
	}
	event, err := p.sources.Current(ctx, scope, d.binding.Location, r.Purpose)
	if err != nil {
		return tasks.Task{}, err
	}
	if event == nil || event.Kind != scope.Kind || event.Key != scope.Key || event.Revision == 0 || len(event.ProtoReflect().GetUnknown()) != 0 {
		return tasks.Task{}, memory.Invalid
	}
	// Dispatch checks current authority, cancellation and source fences again
	// after metadata I/O, before preparing or submitting any task.
	return d.Dispatch(ctx, id, event)
}
