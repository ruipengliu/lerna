package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"lerna/brain"
	"lerna/tasks"
	"time"
)

var names = []string{"sdk-updated-context", "sdk-waiting-replay", "selective-invalidation", "expired-interaction", "budget-floor", "limits-preserve-pause", "deadline-shortened", "source-denied", "terminal-update", "cross-operation-identity"}

func check(ctx context.Context, name string) error {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, l)
	if e != nil {
		return e
	}
	defer h.destroy()
	s, e := h.submit(ctx, l)
	if e != nil {
		return e
	}
	fact, e := h.put(ctx, "new fact")
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	update := tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Intent: "append", InputRefs: []string{fact}}
	switch name {
	case "sdk-updated-context":
		goal, e := h.put(ctx, "Use the new target and the additional fact.")
		if e != nil {
			return e
		}
		r, e := h.inputs.SubmitUpdate(ctx, update)
		if e != nil {
			return e
		}
		op, e = h.operation(ctx)
		if e != nil {
			return e
		}
		_, e = h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: r.Version, Intent: "revise", GoalRef: goal})
		if e != nil {
			return e
		}
		current, e := h.core.Load(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		m := &controlledModel{fn: func(_ context.Context, r brain.Request) (brain.Result, error) {
			found := false
			for _, b := range r.Input.Blocks {
				if b.Ref == fact && b.Text == "new fact" && b.Subject == "operator" && b.Role == "append" {
					found = true
				}
			}
			if !found || r.Input.Goal != "Use the new target and the additional fact." {
				return brain.Result{}, fmt.Errorf("stale model input")
			}
			data, _ := json.Marshal(brain.Answer{Text: "new target with new fact", Sources: []string{fact, goal}})
			return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}}
		out, e := h.run(ctx, current, m)
		if e != nil || out.Task.State != "COMPLETED" || m.calls != 1 {
			return fmt.Errorf("context: %v state=%s reason=%s calls=%d", e, out.Task.State, out.Task.StopReason, m.calls)
		}
		return nil
	case "sdk-waiting-replay", "selective-invalidation", "expired-interaction":
		question, e := h.put(ctx, "Choose yes or no.")
		if e != nil {
			return e
		}
		waiting, e := h.updates.CreateWait(ctx, h.token, tasks.WaitRequest{ChangeID: "question", Qualification: tasks.QualificationOf(s), Questions: []tasks.Question{{QuestionRef: question, Constraint: tasks.AnswerConstraint{Kind: "choice", MaxBytes: 10, Choices: []string{"yes", "no"}}, Responder: "operator", Dependency: "goal", ExpiresUnix: h.now().Add(time.Minute).Unix()}, {QuestionRef: question, Constraint: tasks.AnswerConstraint{Kind: "text", MaxBytes: 100}, Responder: "operator", Dependency: "input", DependencyRef: s.Task.InputRefs[0], ExpiresUnix: h.now().Add(time.Minute).Unix()}}})
		if e != nil {
			return e
		}
		if name == "selective-invalidation" {
			goal, e := h.put(ctx, "new target")
			if e != nil {
				return e
			}
			_, e = h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: waiting.Version, Intent: "revise", GoalRef: goal})
			if e != nil {
				return e
			}
			v, e := h.inputs.Interactions(ctx, s.Task.Ref)
			return demand(e == nil && v.Interactions[0].State == "revoked" && v.Interactions[1].State == "pending")
		}
		answer, e := h.put(ctx, "yes")
		if e != nil {
			return e
		}
		in := tasks.InputRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: waiting.Version, InteractionID: waiting.Interactions[0].ID, AnswerRef: answer}
		if name == "expired-interaction" {
			h.clock.advance(time.Minute)
			in.OperationID, e = h.operation(ctx)
			if e != nil {
				return e
			}
			_, e = h.inputs.ProvideInput(ctx, in)
			v, readErr := h.inputs.Interactions(ctx, s.Task.Ref)
			return demand(authorization.Is(e, authorization.Conflict) && readErr == nil && v.Interactions[0].State == "expired")
		}
		r, e := h.inputs.ProvideInput(ctx, in)
		if e != nil {
			return e
		}
		again, e := h.inputs.ProvideInput(ctx, in)
		if e != nil || again != r {
			return fmt.Errorf("input replay")
		}
		look, e := h.inputs.Lookup(ctx, op)
		if e != nil || look != r {
			return fmt.Errorf("input lookup")
		}
		page, e := h.inputs.Interactions(ctx, s.Task.Ref)
		return demand(e == nil && page.Interactions[0].State == "answered" && page.Interactions[1].State == "pending" && len(page.Inputs) == 1)
	case "budget-floor":
		current, e := start(ctx, h, s)
		if e != nil {
			return e
		}
		req := tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: current.Task.Version, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 699}}}
		_, e = h.inputs.AdjustLimits(ctx, req)
		if !authorization.Is(e, authorization.Invalid) {
			return fmt.Errorf("lost floor")
		}
		req.Patch.Tokens.Value = 700
		r, e := h.inputs.AdjustLimits(ctx, req)
		if e != nil {
			return e
		}
		again, e := h.inputs.AdjustLimits(ctx, req)
		now, readErr := h.client.Get(ctx, s.Task.Ref)
		return demand(e == nil && readErr == nil && r == again && now.ModelReservedTokens == 700)
	case "limits-preserve-pause":
		controls, e := h.core.Controls(inputLimits().Control)
		if e != nil {
			return e
		}
		_, e = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Intent: "PAUSE"})
		if e != nil {
			return e
		}
		current, e := h.client.Get(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		op, e = h.operation(ctx)
		if e != nil {
			return e
		}
		_, e = h.inputs.AdjustLimits(ctx, tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: current.Version, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 1400}}})
		if e != nil {
			return e
		}
		got, e := h.client.Get(ctx, s.Task.Ref)
		return demand(e == nil && got.Control.Intent == "PAUSE" && got.State == "WAITING")
	case "deadline-shortened":
		_, e = h.inputs.AdjustLimits(ctx, tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Patch: tasks.LimitPatch{Deadline: tasks.LimitValue{Present: true, Value: uint64(h.now().Unix() - 1)}}})
		if e != nil {
			return e
		}
		got, e := h.client.Get(ctx, s.Task.Ref)
		return demand(e == nil && got.State == "WAITING" && got.StopReason == "deadline")
	case "source-denied":
		h.policy.Replace(nil)
		_, e = h.inputs.SubmitUpdate(ctx, update)
		got, readErr := h.client.Get(ctx, s.Task.Ref)
		return demand(e != nil && readErr == nil && got.Version == s.Task.Version)
	case "terminal-update":
		out, e := h.run(ctx, s, &controlledModel{})
		if e != nil {
			return e
		}
		update.ExpectedVersion = out.Task.Version
		_, e = h.inputs.SubmitUpdate(ctx, update)
		return demand(authorization.Is(e, authorization.Conflict))
	case "cross-operation-identity":
		_, e = h.inputs.SubmitUpdate(ctx, update)
		if e != nil {
			return e
		}
		_, e = h.inputs.AdjustLimits(ctx, tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: update.ExpectedVersion + 1, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 1400}}})
		return demand(authorization.Is(e, authorization.IdentityConflict))
	}
	return fmt.Errorf("unknown update check")
}
func start(ctx context.Context, h *harness, s tasks.RunSnapshot) (tasks.RunSnapshot, error) {
	for _, kind := range []string{"claim", "start"} {
		out, e := h.port.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(s)})
		if e != nil {
			return tasks.RunSnapshot{}, e
		}
		s = out
	}
	return h.generation.ReserveDecision(ctx, s)
}
