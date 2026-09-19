package answer

import (
	"context"
	"encoding/json"
	"fmt"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"strings"
	"time"
)

var names = []string{"controlled-answer", "private-source", "source-revoked-after-response", "input-overflow", "malformed-output", "encoded-output-overflow", "repair-budget", "truncated-output", "foreign-source", "unknown-usage", "abnormal-usage", "capability-unavailable", "unknown-content-save", "query-after-delete"}

func check(ctx context.Context, name string) error {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	if name == "repair-budget" {
		l.Requests = 2
	}
	h, e := fresh(ctx, l)
	if e != nil {
		return e
	}
	defer h.destroy()
	s, e := h.submit(ctx, l)
	if e != nil {
		return e
	}
	m := &controlledModel{}
	wantCalls := 1
	wantReason := ""
	success := false
	good := func(r brain.Request) brain.Result {
		b, _ := json.Marshal(brain.Answer{Text: "Memory, Brain, Execution", Sources: []string{r.Input.Blocks[0].Ref}})
		return brain.Result{Content: b, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}
	}
	switch name {
	case "controlled-answer", "query-after-delete":
		success = true
	case "private-source":
		e = h.policy.Replace([]contentpolicy.Rule{h.rule(false)})
		wantCalls = 0
		wantReason = "PROCESSING_DENIED"
	case "source-revoked-after-response":
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			if err := h.policy.Replace([]contentpolicy.Rule{h.rule(false)}); err != nil {
				return brain.Result{}, err
			}
			return good(r), nil
		}
		wantReason = "PROCESSING_DENIED"
	case "input-overflow":
		s.Task.Goal = strings.Repeat("x", 32769)
		m.fn = nil
		wantCalls = 0
		wantReason = "INPUT_BUDGET_EXCEEDED"
	case "encoded-output-overflow":
		m.fn = func(context.Context, brain.Request) (brain.Result, error) {
			return brain.Result{Content: []byte(`{"answer":"` + strings.Repeat("<", 2000) + `","sources":[]}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}
		wantReason = "OUTPUT_INVALID"
	case "malformed-output":
		m.fn = func(context.Context, brain.Request) (brain.Result, error) {
			return brain.Result{Content: []byte(`{"answer":"x","answer":"y","sources":[]}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}
		wantReason = "OUTPUT_INVALID"
	case "repair-budget":
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			if m.calls == 1 {
				return brain.Result{Content: []byte(`{"answer":"x","sources":null}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
			}
			return good(r), nil
		}
		success = true
		wantCalls = 2
	case "truncated-output":
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			v := good(r)
			v.Finish = "length"
			return v, nil
		}
		wantReason = "OUTPUT_TRUNCATED"
	case "foreign-source":
		m.fn = func(context.Context, brain.Request) (brain.Result, error) {
			return brain.Result{Content: []byte(`{"answer":"x","sources":["fabricated"]}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}
		wantReason = "OUTPUT_INVALID"
	case "unknown-usage":
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			v := good(r)
			v.Usage = brain.Usage{}
			return v, nil
		}
		success = true
	case "abnormal-usage":
		m.fn = func(_ context.Context, r brain.Request) (brain.Result, error) {
			v := good(r)
			v.Usage.Input = 601
			return v, nil
		}
		wantReason = "MODEL_USAGE_INVALID"
	case "capability-unavailable":
		wantCalls = 0
		wantReason = "CAPABILITY_UNAVAILABLE"
	case "unknown-content-save":
		success = true
		h.access, e = answers.NewContentAccess(&lostContent{Content: h.content}, h.policy, h.binding(), h.source, "task", wallClock{}, 5*time.Minute)
		h.port = answers.BindPort(h.generation, h.access, h.location)
	default:
		return fmt.Errorf("unknown answer case")
	}
	if e != nil {
		return e
	}
	var model brain.Model = m
	if name == "capability-unavailable" {
		model = unsupported{m}
	}
	// Oversized mandatory input is tested at the Brain seam; the public task size
	// already has its own submission bound, and must never be silently truncated.
	if name == "input-overflow" {
		started, e := start(ctx, h, s)
		if e != nil {
			return e
		}
		started.Task.Goal = s.Task.Goal
		b, e := brain.NewAnswer(m, h.access, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
		if e != nil {
			return e
		}
		_, e = b.Decide(ctx, tasks.DecisionInput{Task: started.Task, Work: started.Work[0], Generation: started.Generations[0]})
		return demand(e != nil && e.Error() == wantReason && m.calls == 0)
	}
	out, e := h.run(ctx, s, model)
	if e != nil {
		return e
	}
	if m.calls != wantCalls {
		return fmt.Errorf("%s: call count %d", name, m.calls)
	}
	if !success {
		return demand(out.Task.State != "COMPLETED" && out.Task.StopReason == wantReason)
	}
	if out.Task.State != "COMPLETED" {
		return fmt.Errorf("%s: %s/%s", name, out.Task.State, out.Task.StopReason)
	}
	if name == "unknown-usage" {
		if out.Task.ModelReservedTokens != 700 || out.Task.ModelUsedTokens != 0 {
			return fmt.Errorf("unknown usage released")
		}
	} else if out.Task.ModelReservedTokens != 0 || out.Task.ModelUsedTokens != uint64(wantCalls)*70 {
		return fmt.Errorf("incorrect accounting")
	}
	if name == "query-after-delete" {
		ref, e := answers.ParseReference(out.Task.Result)
		if e != nil {
			return e
		}
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		_, e = h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "DELETE", OperationId: op, Ref: ref, ExpectedRevision: 1, Purpose: "task"})
		if e != nil {
			return e
		}
		v, e := answers.Query(ctx, h.client, s.Task.Ref, h.content, h.binding(), "task")
		return demand(e == nil && v.Task.State == "COMPLETED" && v.Availability != "available" && len(v.Body) == 0 && !v.Delivered)
	}
	v, e := answers.Query(ctx, h.client, s.Task.Ref, h.content, h.binding(), "task")
	return demand(e == nil && v.Availability == "available" && len(v.Body) > 0 && !v.Delivered)
}

type unsupported struct{ *controlledModel }

func (m unsupported) Capabilities() brain.Capabilities {
	v := m.controlledModel.Capabilities()
	v.HardBounds = false
	return v
}

type lostContent struct{ answers.Content }

func (c *lostContent) Call(ctx context.Context, b artifacts.Binding, r *wire.ContentRequest) (*wire.ContentResponse, error) {
	out, e := c.Content.Call(ctx, b, r)
	if e == nil && r.Method == "PUT" {
		return nil, artifacts.Error("OUTCOME_UNKNOWN")
	}
	return out, e
}
func start(ctx context.Context, h *harness, s tasks.RunSnapshot) (tasks.RunSnapshot, error) {
	for _, kind := range []string{"claim", "start"} {
		v, e := h.port.Commit(ctx, tasks.WorkChange{ChangeID: "answer-" + kind, Kind: kind, Qualification: tasks.QualificationOf(s)})
		if e != nil {
			return tasks.RunSnapshot{}, e
		}
		s = v
	}
	return h.generation.ReserveDecision(ctx, s)
}
