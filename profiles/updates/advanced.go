package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"lerna/brain"
	"lerna/tasks"
	"sync"
	"time"
)

var advancedNames = []string{"concurrent-replies", "reply-schema", "question-policy-revoked", "unrelated-update-reply-context", "running-goal-race", "running-deadline", "uncooperative-generation", "stale-publication", "explicit-zero-and-absent", "version-conflict-atomic", "reply-question-association"}

func advanced(ctx context.Context, name string) error {
	if name == "reply-question-association" {
		return replyAssociation(ctx)
	}
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, l)
	if e != nil {
		return e
	}
	defer h.destroy()
	budget := l
	budget.Requests = 2
	s, e := h.submit(ctx, budget)
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	switch name {
	case "concurrent-replies", "reply-schema", "question-policy-revoked", "unrelated-update-reply-context":
		question, e := h.put(ctx, "Choose yes or no")
		if e != nil {
			return e
		}
		answer, e := h.put(ctx, "yes")
		if e != nil {
			return e
		}
		page, e := h.updates.CreateWait(ctx, h.token, tasks.WaitRequest{ChangeID: "wait", Qualification: tasks.QualificationOf(s), Questions: []tasks.Question{{QuestionRef: question, Constraint: tasks.AnswerConstraint{Kind: "choice", MaxBytes: 10, Choices: []string{"yes", "no"}}, Dependency: "input", DependencyRef: s.Task.InputRefs[0], Responder: "operator", ExpiresUnix: h.now().Add(time.Minute).Unix()}}})
		if e != nil {
			return e
		}
		in := tasks.InputRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: page.Version, InteractionID: page.Interactions[0].ID, AnswerRef: answer}
		if name == "question-policy-revoked" {
			h.policy.Replace(nil)
			_, e = h.inputs.Interactions(ctx, s.Task.Ref)
			return demand(e != nil)
		}
		if name == "reply-schema" {
			in.AnswerRef, e = h.put(ctx, "not an allowed answer")
			if e != nil {
				return e
			}
			_, e = h.inputs.ProvideInput(ctx, in)
			now, read := h.client.Get(ctx, s.Task.Ref)
			return demand(e != nil && read == nil && now.Version == page.Version)
		}
		if name == "concurrent-replies" {
			other, e := h.operation(ctx)
			if e != nil {
				return e
			}
			var wg sync.WaitGroup
			errors := make(chan error, 2)
			gate := make(chan struct{})
			for _, id := range []string{op, other} {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					<-gate
					r := in
					r.OperationID = id
					_, err := h.inputs.ProvideInput(ctx, r)
					errors <- err
				}(id)
			}
			close(gate)
			wg.Wait()
			close(errors)
			won := 0
			for err := range errors {
				if err == nil {
					won++
				} else if !authorization.Is(err, authorization.Conflict) {
					return err
				}
			}
			p, e := h.inputs.Interactions(ctx, s.Task.Ref)
			return demand(e == nil && won == 1 && p.Version == page.Version+1 && len(p.Inputs) == 1 && p.Interactions[0].State == "answered")
		}
		fact, e := h.put(ctx, "unrelated fact")
		if e != nil {
			return e
		}
		updateOp, e := h.operation(ctx)
		if e != nil {
			return e
		}
		receipt, e := h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: updateOp, Ref: s.Task.Ref, ExpectedVersion: page.Version, Intent: "append", InputRefs: []string{fact}})
		if e != nil {
			return e
		}
		current, e := h.inputs.Interactions(ctx, s.Task.Ref)
		if e != nil || current.Interactions[0].State != "pending" {
			return fmt.Errorf("append consumed question")
		}
		in.ExpectedVersion = receipt.Version
		if _, e = h.inputs.ProvideInput(ctx, in); e != nil {
			return e
		}
		next, e := h.core.Load(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		m := &controlledModel{fn: func(_ context.Context, r brain.Request) (brain.Result, error) {
			found := false
			for _, b := range r.Input.Blocks {
				if b.Ref == answer && b.Text == "yes" && b.Role == "reply" {
					found = true
				}
			}
			if !found {
				return brain.Result{}, fmt.Errorf("missing answer")
			}
			data, _ := json.Marshal(brain.Answer{Text: "Selected yes", Sources: []string{answer}})
			return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}}
		out, e := h.run(ctx, next, m)
		if e != nil || out.Task.State != "COMPLETED" {
			return fmt.Errorf("reply context: %v", e)
		}
		replay, e := h.inputs.ProvideInput(ctx, in)
		return demand(e == nil && replay.Version == next.Task.Version)
	case "running-goal-race", "running-deadline", "uncooperative-generation":
		goal, e := h.put(ctx, "The revised goal.")
		if e != nil {
			return e
		}
		entered := make(chan struct{})
		release := make(chan struct{})
		returned := make(chan struct{})
		m := &controlledModel{fn: func(c context.Context, r brain.Request) (brain.Result, error) {
			close(entered)
			if name == "uncooperative-generation" {
				<-release
			} else {
				<-c.Done()
			}
			defer close(returned)
			data, _ := json.Marshal(brain.Answer{Text: "stale answer", Sources: []string{r.Input.Blocks[0].Ref}})
			return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
		}}
		done := make(chan error, 1)
		go func() { _, err := h.run(ctx, s, m); done <- err }()
		<-entered
		current, e := h.client.Get(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		if name == "running-deadline" {
			_, e = h.inputs.AdjustLimits(ctx, tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: current.Version, Patch: tasks.LimitPatch{Deadline: tasks.LimitValue{Present: true, Value: uint64(h.now().Unix() - 1)}}})
		} else {
			_, e = h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: current.Version, Intent: "revise", GoalRef: goal})
		}
		if e != nil {
			return e
		}
		select {
		case e = <-done:
			if e != nil {
				return e
			}
		case <-time.After(4 * time.Second):
			return fmt.Errorf("update stop exceeded bound")
		}
		currentRun, e := h.core.Load(ctx, s.Task.Ref)
		if e != nil {
			return e
		}
		if currentRun.Task.Result != "" {
			return fmt.Errorf("stale answer published")
		}
		if name == "uncooperative-generation" {
			// The old request is still executing. Neither the ledger nor the durable
			// work slot can be recycled, even by a fresh runner instance.
			if !currentRun.Work[0].InFlight || currentRun.Task.ModelReservedTokens != 700 {
				return fmt.Errorf("released unknown work")
			}
			replacement := &controlledModel{}
			_, err := h.run(ctx, currentRun, replacement)
			if err == nil || replacement.calls != 0 {
				return fmt.Errorf("overlapping replacement")
			}
			close(release)
			<-returned
			deadline := time.Now().Add(3 * time.Second)
			for {
				currentRun, e = h.core.Load(ctx, s.Task.Ref)
				if e != nil {
					return e
				}
				if !currentRun.Work[0].InFlight && currentRun.Task.ModelUsedTokens == 70 {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("late settlement/stop lost")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if name == "running-deadline" {
			return demand(currentRun.Task.State == "WAITING" && currentRun.Task.StopReason == "deadline" && !currentRun.Work[0].InFlight)
		}
		out, e := h.run(ctx, currentRun, &controlledModel{})
		return demand(e == nil && out.Task.State == "COMPLETED" && out.Task.Attempts == 2 && out.Task.ModelUsedTokens == 140 && out.Task.ModelReservedTokens == 0)
	case "stale-publication":
		s, e = start(ctx, h, s)
		if e != nil {
			return e
		}
		q := tasks.QualificationOf(s)
		if e = h.generation.BeginRequest(ctx, q, 0); e != nil {
			return e
		}
		input, e := h.access.Assemble(ctx, s.Task, h.location, brain.MaxInputBytes)
		if e != nil {
			return e
		}
		m := &controlledModel{}
		result, e := m.Generate(ctx, brain.Request{Input: input, MaxInput: 600, MaxOutput: 100})
		if e != nil {
			return e
		}
		ref, e := h.access.Save(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]}, result.Content)
		if e != nil {
			return e
		}
		if e = h.generation.Settle(ctx, q, tasks.GenerationUsage{Requests: 1, Tokens: 70}); e != nil {
			return e
		}
		goal, e := h.put(ctx, "changed after artifact preparation")
		if e != nil {
			return e
		}
		_, e = h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Intent: "revise", GoalRef: goal})
		if e != nil {
			return e
		}
		_, e = h.port.Commit(ctx, tasks.WorkChange{ChangeID: "stale-output", Kind: "complete", Qualification: q, Finished: true, Proposal: tasks.Proposal{Kind: "answer", BaseVersion: q.Version, Complete: true, Result: ref}})
		now, read := h.client.Get(ctx, s.Task.Ref)
		if e == nil || read != nil || now.Result != "" || now.ModelUsedTokens != 70 {
			return fmt.Errorf("stale publication err=%v read=%v state=%s result=%s used=%d", e, read, now.State, now.Result, now.ModelUsedTokens)
		}
		return nil
	case "explicit-zero-and-absent":
		for _, patch := range []tasks.LimitPatch{{}, {Tokens: tasks.LimitValue{Present: false, Value: 42}}, {Tokens: tasks.LimitValue{Present: true, Value: 0}}} {
			_, e = h.inputs.AdjustLimits(ctx, tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Patch: patch})
			if !authorization.Is(e, authorization.Invalid) {
				return fmt.Errorf("ambiguous zero: %v", e)
			}
		}
		now, e := h.client.Get(ctx, s.Task.Ref)
		return demand(e == nil && now.Version == s.Task.Version && now.Constraints == s.Task.Constraints)
	case "version-conflict-atomic":
		fact, e := h.put(ctx, "new fact")
		if e != nil {
			return e
		}
		_, e = h.inputs.SubmitUpdate(ctx, tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version + 1, Intent: "append", InputRefs: []string{fact}})
		now, read := h.client.Get(ctx, s.Task.Ref)
		return demand(authorization.Is(e, authorization.Conflict) && read == nil && now.Version == s.Task.Version && len(now.InputFacts) == 0 && len(now.InputRefs) == 1)
	}
	return fmt.Errorf("unknown advanced check")
}
