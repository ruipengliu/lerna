package interaction

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func command[I, O any](s *Service, name string, cas bool, handler func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (O, error)) runtime.Method {
	return runtime.Method{Contract: api.Contract[I, O](name, "interaction", "command", cas, false), Participants: s.config.Participants, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		out, err := handler(ctx, tx, a, c, in)
		return runtime.Applied(out), err
	}}
}
func query[I, O any](name string, handler func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) runtime.Method {
	return runtime.Method{Contract: api.Contract[I, O](name, "interaction", "query", false, false), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return handler(ctx, store, scope, a, q, in)
	}}
}
func (s *Service) Register(r *runtime.Registry) error {
	methods := []runtime.Method{
		command[CreateSessionInput, SessionOutput](s, "session.create", false, s.CreateSessionTx),
		command[CreateBranchInput, SessionOutput](s, "session.branch.create", false, s.CreateBranchTx),
		command[SelectBranchInput, SessionOutput](s, "session.branch.select", true, s.SelectBranchTx),
		query[ReadInput, SessionView]("session.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (SessionView, error) {
			return s.ReadSession(ctx, store, scope, a, q.TargetID, in)
		}),
	}
	for _, name := range []string{"session.archive", "session.reopen", "session.delete"} {
		methods = append(methods, command[SessionControlInput, SessionOutput](s, name, true, s.ControlSessionTx))
	}
	if s.ports.Content != nil && s.ports.Delivery != nil && api.ValidID(s.config.DiscoveryOwnerID) {
		for _, name := range []string{"session.submit_goal", "session.steer"} {
			methods = append(methods, command[GoalInput, SubmissionOutput](s, name, false, s.SubmitGoalTx))
		}
		if s.ports.Closure != nil {
			methods = append(methods, command[GoalInput, SubmissionOutput](s, "session.enqueue_goal_after", false, s.SubmitGoalTx))
		}
		methods = append(methods, command[WithdrawInput, SubmissionOutput](s, "submission.withdraw", true, s.WithdrawTx), query[ReadInput, SubmissionView]("submission.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (SubmissionView, error) {
			return s.ReadSubmission(ctx, store, scope, a, q.TargetID)
		}), command[ReplyInput, ReplyOutput](s, "session.reply", false, s.ReplyTx))
		if s.ports.Requests != nil {
			methods = append(methods, command[InputInput, SubmissionOutput](s, "interaction.input", false, s.ForwardInputTx))
		}
		if err := r.RegisterJob(JobDispatch, s.Dispatch); err != nil {
			return err
		}
	}
	for _, m := range methods {
		if err := r.Register(m); err != nil {
			return err
		}
	}
	if s.ports.ScheduleGate != nil && s.ports.Calendar != nil && s.ports.Delivery != nil && s.ports.Closure != nil && s.ports.Content != nil && api.ValidID(s.config.DiscoveryOwnerID) {
		for _, m := range []runtime.Method{
			command[ScheduleInput, ScheduleOutput](s, "schedule.create", false, s.CreateScheduleTx),
			command[ScheduleInput, ScheduleOutput](s, "schedule.update", true, s.UpdateScheduleTx),
			query[ReadInput, Schedule]("schedule.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (Schedule, error) {
				return s.ReadSchedule(ctx, store, scope, a, q.TargetID)
			}),
			query[ReadInput, Occurrence]("occurrence.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (Occurrence, error) {
				return s.ReadOccurrence(ctx, store, scope, a, q.TargetID)
			}),
		} {
			if err := r.Register(m); err != nil {
				return err
			}
		}
		for _, name := range []string{"schedule.pause", "schedule.resume", "schedule.delete"} {
			if err := r.Register(command[ScheduleControlInput, ScheduleOutput](s, name, true, s.ControlScheduleTx)); err != nil {
				return err
			}
		}
		if err := r.RegisterJob(JobTrigger, s.Trigger); err != nil {
			return err
		}
		if err := r.RegisterJob(JobOccurrence, s.Occur); err != nil {
			return err
		}
	}
	if len(s.bindings) > 0 && s.ports.Content != nil {
		for _, m := range []runtime.Method{
			command[SurfaceInput, Surface](s, "surface.create", false, s.CreateSurfaceTx), command[SurfaceInput, Surface](s, "surface.update", true, s.UpdateSurfaceTx), command[SurfaceControlInput, Surface](s, "surface.close", true, s.CloseSurfaceTx),
			query[ReadInput, Surface]("surface.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (Surface, error) {
				return s.ReadSurface(ctx, store, scope, a, q.TargetID)
			}),
			command[OpenPresentationInput, Presentation](s, "presentation.open", false, s.OpenPresentationTx), command[OpenPresentationInput, Presentation](s, "presentation.switch", true, s.SwitchPresentationTx), command[BeginPresentationInput, Presentation](s, "presentation.begin", true, s.BeginPresentationTx), command[ClosePresentationInput, Presentation](s, "presentation.close", true, s.ClosePresentationTx), command[RenderAckInput, Presentation](s, "presentation.rendered", false, s.RenderedTx),
			query[RenderReadInput, RenderView]("presentation.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in RenderReadInput) (RenderView, error) {
				return s.ReadPresentation(ctx, store, scope, a, q.TargetID, in)
			}),
		} {
			if err := r.Register(m); err != nil {
				return err
			}
		}
		if s.ports.Delivery != nil {
			if err := r.Register(command[ApplicationEventInput, ApplicationEventOutput](s, "application_event", false, s.ApplicationEventTx)); err != nil {
				return err
			}
			if err := r.Register(query[ReadInput, ApplicationEvent]("application_event.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (ApplicationEvent, error) {
				return s.ReadApplicationEvent(ctx, store, scope, a, q.TargetID)
			})); err != nil {
				return err
			}
			if err := r.RegisterJob(JobApplicationEvent, s.DeliverApplicationEvent); err != nil {
				return err
			}
		}
	}
	return nil
}
