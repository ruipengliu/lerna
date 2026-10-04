package interaction

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) ApplicationEventTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ApplicationEventInput) (ApplicationEventOutput, error) {
	view, err := s.renderGate(ctx, tx, a, c.TargetID, in.Generation, in.IntentRevision)
	if err != nil {
		return ApplicationEventOutput{}, err
	}
	if in.SurfaceRef != view.Presentation.SurfaceRef {
		return ApplicationEventOutput{}, api.E("revision_conflict", "event_surface_changed")
	}
	binding := s.bindings[bindingKey(view.Surface.BindingRef)]
	var rule *EventRule
	for i := range binding.Events {
		if binding.Events[i].Name == in.Name {
			rule = &binding.Events[i]
			break
		}
	}
	if rule == nil {
		return ApplicationEventOutput{}, api.E("unsupported", "application_event_unregistered")
	}
	if rule.RequiresRendered && !view.Presentation.Presented {
		return ApplicationEventOutput{}, api.E("invalid_state", "dependent_body_unrendered")
	}
	var schema api.Schema
	if err = api.Decode(rule.Schema, &schema); err != nil {
		return ApplicationEventOutput{}, err
	}
	validator, err := api.NewValidator(schema)
	if err != nil {
		return ApplicationEventOutput{}, err
	}
	if err = validator.Validate(in.Payload); err != nil {
		return ApplicationEventOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ApplicationEventOutput{}, err
	}
	id := s.config.Identity.NewID("application_event")
	r := applicationEventRecord{ApplicationEvent: ApplicationEvent{EventID: id, Revision: 1, PresentationRef: tx.Scope().Ref(c.TargetID, view.Presentation.Revision), SurfaceRef: in.SurfaceRef, Name: in.Name, State: "queued", Command: api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: rule.OwnerID, CommandID: s.config.Identity.NewID("command"), Method: rule.Method, TargetID: rule.TargetID, ExpectedRevision: rule.ExpectedRevision, ExpiresAt: api.Time(now.Add(time.Duration(rule.AcceptForSeconds) * time.Second)), Payload: in.Payload}}, Auth: a}
	if err = tx.Create(ctx, applicationEvents, id, a.SubjectID, r); err != nil {
		return ApplicationEventOutput{}, err
	}
	if _, err = tx.Raise(ctx, JobApplicationEvent, id, tx.Scope().Ref(id, 1), now); err != nil {
		return ApplicationEventOutput{}, err
	}
	return ApplicationEventOutput{EventRef: tx.Scope().Ref(id, 1), State: "queued", QueryMethod: "application_event.read"}, nil
}
func (s *Service) ReadApplicationEvent(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (ApplicationEvent, error) {
	return currentDisclosure(ctx, s, store, scope, a, func(tx runtime.Tx) (ApplicationEvent, error) {
		var r applicationEventRecord
		if _, err := tx.Get(ctx, applicationEvents, id, &r); err != nil {
			return ApplicationEvent{}, err
		}
		if err := access(a, scope, r.Auth.SubjectID); err != nil {
			return ApplicationEvent{}, api.E("forbidden", "application_event_redacted")
		}
		return r.ApplicationEvent, nil
	})
}
func saveApplicationEvent(ctx context.Context, tx runtime.Tx, r *applicationEventRecord) error {
	old := r.Revision
	r.Revision++
	return tx.Put(ctx, applicationEvents, r.EventID, old, *r)
}
func (s *Service) DeliverApplicationEvent(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var prepared applicationEventRecord
	var now time.Time
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		if _, e := tx.Get(ctx, applicationEvents, work.Job.SourceRef.ObjectID, &prepared); e != nil {
			return e
		}
		var e error
		now, e = tx.Now(ctx)
		if e != nil {
			return e
		}
		if prepared.State == "queued" {
			prepared.State = "sending"
			if e = saveApplicationEvent(ctx, tx, &prepared); e != nil {
				return e
			}
		}
		return tx.Guard(ctx, work.Claim)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	if prepared.State != "sending" {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), nil)
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	receipt, err := s.ports.Delivery.Lookup(ctx, scope, prepared.Auth, prepared.Command.LogicalServiceID, prepared.Command.CommandID)
	if api.IsCode(err, "not_found") {
		if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		receipt, err = s.ports.Delivery.Send(ctx, scope, prepared.Auth, prepared.Command)
	}
	if err != nil {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Ready(now), nil)
	}
	if err = checkReceipt(prepared.Command, receipt); err != nil {
		return err
	}
	disposition := runtime.Done()
	if receipt.Stage == "accepted" {
		disposition = runtime.Waiting(now.Add(time.Second))
	}
	return runtime.Finish(ctx, store, scope, s.config.Participants, work, disposition, func(tx runtime.Tx) error {
		var r applicationEventRecord
		if _, e := tx.Get(ctx, applicationEvents, work.Job.SourceRef.ObjectID, &r); e != nil {
			return e
		}
		if r.State != "sending" {
			return nil
		}
		r.Receipt = &receipt
		if receipt.Stage != "accepted" {
			r.State = receipt.Stage
		}
		return saveApplicationEvent(ctx, tx, &r)
	})
}
