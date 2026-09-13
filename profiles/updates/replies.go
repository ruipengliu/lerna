package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/brain"
	"lerna/tasks"
	"time"
)

func replyAssociation(ctx context.Context) error {
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
	questions := []string{"Should the response include a summary?", "Should the response include a source list?"}
	refs := []string{}
	request := tasks.WaitRequest{ChangeID: "two-questions", Qualification: tasks.QualificationOf(s)}
	for _, text := range questions {
		ref, err := h.put(ctx, text)
		if err != nil {
			return err
		}
		refs = append(refs, ref)
		request.Questions = append(request.Questions, tasks.Question{QuestionRef: ref, Constraint: tasks.AnswerConstraint{Kind: "choice", MaxBytes: 10, Choices: []string{"yes", "no"}}, Dependency: "input", DependencyRef: s.Task.InputRefs[0], Responder: "operator", ExpiresUnix: h.now().Add(time.Minute).Unix()})
	}
	page, e := h.updates.CreateWait(ctx, h.token, request)
	if e != nil {
		return e
	}
	answer, e := h.put(ctx, "yes")
	if e != nil {
		return e
	}
	version := page.Version
	for _, q := range page.Interactions {
		op, err := h.operation(ctx)
		if err != nil {
			return err
		}
		r, err := h.inputs.ProvideInput(ctx, tasks.InputRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: version, InteractionID: q.ID, AnswerRef: answer})
		if err != nil {
			return err
		}
		version = r.Version
	}
	current, e := h.core.Load(ctx, s.Task.Ref)
	if e != nil {
		return e
	}
	m := &controlledModel{fn: func(_ context.Context, r brain.Request) (brain.Result, error) {
		bodies := map[string]string{}
		var links []brain.ReplyTo
		for _, b := range r.Input.Blocks {
			bodies[b.Ref] = b.Text
			if b.Ref == answer {
				links = b.ReplyTo
			}
		}
		if len(links) != 2 {
			return brain.Result{}, fmt.Errorf("lost reply relationships")
		}
		for i, link := range links {
			if link.InteractionID != page.Interactions[i].ID || link.QuestionRef != refs[i] || bodies[link.QuestionRef] != questions[i] {
				return brain.Result{}, fmt.Errorf("wrong question context")
			}
		}
		data, _ := json.Marshal(brain.Answer{Text: "Include summary and sources", Sources: []string{answer, refs[0], refs[1]}})
		return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
	}}
	out, e := h.run(ctx, current, m)
	return demand(e == nil && out.Task.State == "COMPLETED" && m.calls == 1)
}
