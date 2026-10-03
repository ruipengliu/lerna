package task

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func command[I, O any](s *Service, name string, cas, accepted bool, handler func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (runtime.Outcome, error)) runtime.Method {
	return runtime.Method{Contract: api.Contract[I, O](name, "orchestrator", "command", cas, accepted), Participants: s.config.Participants, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		return handler(ctx, tx, a, c, in)
	}}
}
func query[I, O any](s *Service, name string, handler func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) runtime.Method {
	return runtime.Method{Contract: api.Contract[I, O](name, "orchestrator", "query", false, false), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return handler(ctx, store, scope, a, q, in)
	}}
}
func (s *Service) Register(r *runtime.Registry) error {
	methods := []runtime.Method{
		command[SubmitInput, TaskOutput](s, "task.submit", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SubmitInput) (runtime.Outcome, error) {
			out, err := s.SubmitTx(ctx, tx, a, c, in)
			return runtime.Applied(out), err
		}),
		query[ReadInput, api.Task](s, "task.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (api.Task, error) {
			t, e := s.readState(ctx, store, scope, a, q.TargetID, in.Revision)
			return t.Task, e
		}),
	}
	methods = append(methods,
		command[ReviseInput, TaskOutput](s, "task.revise", true, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ReviseInput) (runtime.Outcome, error) {
			out, e := s.ReviseTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[SteerInput, TaskOutput](s, "task.steer", false, true, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SteerInput) (runtime.Outcome, error) {
			out, e := s.SteerTx(ctx, tx, a, c, in)
			return runtime.Accepted(out), e
		}),
		command[InputAnswer, InputOutput](s, "task.input", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in InputAnswer) (runtime.Outcome, error) {
			out, e := s.InputTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[AcceptInput, AcceptOutput](s, "task.accept_result", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in AcceptInput) (runtime.Outcome, error) {
			out, e := s.AcceptTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[BudgetInput, TaskOutput](s, "task.adjust_budget", true, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in BudgetInput) (runtime.Outcome, error) {
			out, e := s.AdjustBudgetTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[AttachInput, AttachOutput](s, "task.attach_evidence", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in AttachInput) (runtime.Outcome, error) {
			out, e := s.AttachTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[BillingInput, BillingOutput](s, "task.billing_reconcile", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in BillingInput) (runtime.Outcome, error) {
			out, e := s.BillingTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[ControlWindowInput, api.ControlSnapshot](s, "task.control_window", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ControlWindowInput) (runtime.Outcome, error) {
			out, e := s.ControlWindowTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		query[ResultInput, ResultOutput](s, "task.result", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ResultInput) (ResultOutput, error) {
			return s.Result(ctx, store, scope, a, q.TargetID, in)
		}),
		query[TaskListInput, api.Page[api.Task]](s, "task.list", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in TaskListInput) (api.Page[api.Task], error) {
			return s.List(ctx, store, scope, a, in)
		}),
		command[AllocateInput, AllocationOutput](s, "budget.allocate", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in AllocateInput) (runtime.Outcome, error) {
			out, e := s.AllocateTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[AllocationCloseInput, AllocationOutput](s, "budget.close", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in AllocationCloseInput) (runtime.Outcome, error) {
			out, e := s.CloseAllocationTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[SettleInput, AllocationOutput](s, "budget.settle", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SettleInput) (runtime.Outcome, error) {
			out, e := s.SettleTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		query[BudgetReadInput, BudgetReadResponse](s, "budget.read", s.budgetQuery),
		command[AdjustmentInput, AdjustmentOutput](s, "billing.adjustment.submit", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in AdjustmentInput) (runtime.Outcome, error) {
			out, e := s.AdjustmentTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[ChildCreateInput, ChildOutput](s, "child.create", false, true, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ChildCreateInput) (runtime.Outcome, error) {
			out, e := s.ChildCreateTx(ctx, tx, a, c, in)
			return runtime.Accepted(out), e
		}),
		command[ChildSendInput, ChildOutput](s, "child.send", true, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ChildSendInput) (runtime.Outcome, error) {
			out, e := s.ChildSendTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		command[ChildCloseInput, ChildCloseOutput](s, "child.close", true, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ChildCloseInput) (runtime.Outcome, error) {
			out, e := s.ChildCloseTx(ctx, tx, a, c, in)
			return runtime.Applied(out), e
		}),
		query[ReadInput, ChildHandle](s, "child.read", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ReadInput) (ChildHandle, error) {
			return s.ChildRead(ctx, store, scope, a, q.TargetID)
		}),
		query[api.ListInput, api.Page[ChildHandle]](s, "child.list", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in api.ListInput) (api.Page[ChildHandle], error) {
			return s.ChildList(ctx, store, scope, a, in)
		}),
		query[ChildWaitInput, ChildWaitOutput](s, "child.wait", func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in ChildWaitInput) (ChildWaitOutput, error) {
			if q.TargetID != in.ChildID {
				return ChildWaitOutput{}, invalid("target_mismatch")
			}
			return s.ChildWait(ctx, store, scope, a, in)
		}),
	)
	for _, name := range []string{"task.pause", "task.resume", "task.cancel"} {
		methods = append(methods, command[ControlInput, TaskOutput](s, name, true, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ControlInput) (runtime.Outcome, error) {
			out, err := s.ControlTx(ctx, tx, a, c, in)
			return runtime.Applied(out), err
		}))
	}
	for _, m := range methods {
		if err := r.Register(m); err != nil {
			return err
		}
	}
	return s.registerJobs(r)
}
