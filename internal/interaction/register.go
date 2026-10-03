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
	return nil
}
