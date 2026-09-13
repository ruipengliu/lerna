package answer

import (
	"context"
	"encoding/json"
	"lerna/answers"
	"lerna/authorization"
	"lerna/brain"
	"lerna/tasks"
	"time"
)

var edgeNames = []string{"pause-before-admission", "cancel-before-admission", "unknown-reservation", "core-reply-lost", "budget-exhausted"}

type lostReservation struct{ *answers.Port }

func (p lostReservation) ReserveDecision(ctx context.Context, s tasks.RunSnapshot) (tasks.RunSnapshot, error) {
	out, e := p.Port.ReserveDecision(ctx, s)
	if e == nil {
		return tasks.RunSnapshot{}, &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return out, e
}

type lostPublication struct{ *answers.Port }

func (p lostPublication) Commit(ctx context.Context, c tasks.WorkChange) (tasks.RunSnapshot, error) {
	out, e := p.Port.Commit(ctx, c)
	if e == nil && c.Kind == "complete" {
		return tasks.RunSnapshot{}, &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return out, e
}
func edge(ctx context.Context, name string) error {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, l)
	if e != nil {
		return e
	}
	defer h.destroy()
	budget := l
	if name == "budget-exhausted" {
		budget.InputTokens = 500
	}
	s, e := h.submit(ctx, budget)
	if e != nil {
		return e
	}
	m := &controlledModel{}
	if name == "pause-before-admission" || name == "cancel-before-admission" {
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			current, e := h.core.Load(ctx, s.Task.Ref)
			if e != nil {
				return brain.Result{}, e
			}
			control, e := h.core.Controls(tasks.ControlLimits{MaxOperations: 32, MaxObservations: 32, MaxChecks: 4, PollInterval: 100 * time.Millisecond, StopTimeout: time.Second, IOTimeout: time.Second})
			if e != nil {
				return brain.Result{}, e
			}
			op, e := h.auth.NewOperation(ctx, h.token)
			if e != nil {
				return brain.Result{}, e
			}
			intent := "PAUSE"
			if name == "cancel-before-admission" {
				intent = "CANCEL"
			}
			_, e = control.Request(ctx, h.token, tasks.ControlRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: current.Task.Version, Intent: intent})
			if e != nil {
				return brain.Result{}, e
			}
			data, _ := json.Marshal(brain.Answer{Text: "Memory, Brain, Execution", Sources: []string{r.Input.Blocks[0].Ref}})
			return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}
	}
	b, e := brain.NewAnswer(m, h.access, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
	if e != nil {
		return e
	}
	var port tasks.WorkStore = h.port
	if name == "unknown-reservation" {
		port = lostReservation{h.port}
	}
	if name == "core-reply-lost" {
		port = lostPublication{h.port}
	}
	runner, e := tasks.NewRunner(port, b, limits(), wallClock{})
	if e != nil {
		return e
	}
	out, runErr := runner.Run(ctx, s)
	current, e := h.core.Load(ctx, s.Task.Ref)
	if e != nil {
		return e
	}
	switch name {
	case "pause-before-admission", "cancel-before-admission":
		return demand(runErr == nil && out.Task.State != "COMPLETED" && current.Task.Result == "" && current.Task.ModelUsedTokens == 70 && current.Task.ModelReservedTokens == 0)
	case "unknown-reservation":
		return demand(authorization.Is(runErr, authorization.OutcomeUnknown) && m.calls == 0 && current.Task.ModelReservedTokens == 700 && len(current.Generations) == 1)
	case "budget-exhausted":
		return demand(runErr == nil && m.calls == 0 && current.Task.StopReason == "GENERATION_BUDGET_EXCEEDED" && current.Task.ModelReservedTokens == 0)
	case "core-reply-lost":
		if runErr != nil {
			return runErr
		}
		again, e := h.port.Recover(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		return demand(out.Task.State == "COMPLETED" && again.Task.Result == out.Task.Result && again.Task.Version == out.Task.Version && m.calls == 1)
	}
	return demand(false)
}
