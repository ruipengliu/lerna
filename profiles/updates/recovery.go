package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"lerna/brain"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"input-committed", "interaction-consumed", "generation-unknown", "limits-committed"}

type checkpoint struct {
	Token  string
	Ref    tasks.Ref
	Update tasks.UpdateRequest
	Reply  tasks.InputRequest
	Adjust tasks.AdjustRequest
}

func saveCheckpoint(root string, c checkpoint) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(root, "checkpoint.json"), b, 0600)
}
func RunProbe(ctx context.Context, root, point string) error {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := open(ctx, root, "", "test-model-location", l)
	if e != nil {
		return e
	}
	defer h.close()
	budget := l
	budget.Requests = 2
	s, e := h.submit(ctx, budget)
	if e != nil {
		return e
	}
	fact, e := h.put(ctx, "recovered fact")
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	cp := checkpoint{Token: h.token, Ref: s.Task.Ref}
	switch point {
	case "input-committed":
		cp.Update = tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Intent: "append", InputRefs: []string{fact}}
		if e = saveCheckpoint(root, cp); e != nil {
			return e
		}
		if _, e = h.inputs.SubmitUpdate(ctx, cp.Update); e != nil {
			return e
		}
		os.Exit(73)
	case "interaction-consumed":
		question, e := h.put(ctx, "supply a fact")
		if e != nil {
			return e
		}
		wait, e := h.updates.CreateWait(ctx, h.token, tasks.WaitRequest{ChangeID: "create-question", Qualification: tasks.QualificationOf(s), Questions: []tasks.Question{{QuestionRef: question, Constraint: tasks.AnswerConstraint{Kind: "text", MaxBytes: 100}, Dependency: "goal", Responder: "operator", ExpiresUnix: h.now().Add(time.Minute).Unix()}}})
		if e != nil {
			return e
		}
		cp.Reply = tasks.InputRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: wait.Version, InteractionID: wait.Interactions[0].ID, AnswerRef: fact}
		if e = saveCheckpoint(root, cp); e != nil {
			return e
		}
		if _, e = h.inputs.ProvideInput(ctx, cp.Reply); e != nil {
			return e
		}
		os.Exit(73)
	case "generation-unknown", "limits-committed":
		s, e = start(ctx, h, s)
		if e != nil {
			return e
		}
		if point == "limits-committed" {
			cp.Adjust = tasks.AdjustRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 1000}, Deadline: tasks.LimitValue{Present: true, Value: uint64(h.now().Add(2 * time.Second).Unix())}}}
			if e = saveCheckpoint(root, cp); e != nil {
				return e
			}
			if _, e = h.inputs.AdjustLimits(ctx, cp.Adjust); e != nil {
				return e
			}
			os.Exit(73)
		}
		cp.Update = tasks.UpdateRequest{OperationID: op, Ref: s.Task.Ref, ExpectedVersion: s.Task.Version, Intent: "append", InputRefs: []string{fact}}
		if e = saveCheckpoint(root, cp); e != nil {
			return e
		}
		if e = h.generation.BeginRequest(ctx, tasks.QualificationOf(s), 0); e != nil {
			return e
		}
		model := &controlledModel{fn: func(context.Context, brain.Request) (brain.Result, error) {
			if _, e = h.inputs.SubmitUpdate(ctx, cp.Update); e != nil {
				return brain.Result{}, e
			}
			os.Exit(73)
			return brain.Result{}, nil
		}}
		_, e = model.Generate(ctx, brain.Request{MaxInput: 600, MaxOutput: 100})
		return e
	}
	return fmt.Errorf("unknown crash point")
}
func recovery(ctx context.Context, executable, point string) error {
	root, e := os.MkdirTemp("", "update-recovery-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	cmd := exec.CommandContext(ctx, executable, "updates-crash-probe", root, point)
	err := cmd.Run()
	status, ok := err.(*exec.ExitError)
	if !ok || status.ExitCode() != 73 {
		return fmt.Errorf("crash probe %s: %v", point, err)
	}
	raw, e := os.ReadFile(filepath.Join(root, "checkpoint.json"))
	if e != nil {
		return e
	}
	var cp checkpoint
	if e = json.Unmarshal(raw, &cp); e != nil {
		return e
	}
	h, e := open(ctx, root, cp.Token, "test-model-location", tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if e != nil {
		return e
	}
	defer h.close()
	var receipt tasks.InputReceipt
	switch point {
	case "input-committed", "generation-unknown":
		receipt, e = h.inputs.SubmitUpdate(ctx, cp.Update)
	case "interaction-consumed":
		receipt, e = h.inputs.ProvideInput(ctx, cp.Reply)
	case "limits-committed":
		receipt, e = h.inputs.AdjustLimits(ctx, cp.Adjust)
	}
	if e != nil {
		return e
	}
	lookup, e := h.inputs.Lookup(ctx, receipt.OperationID)
	if e != nil || lookup != receipt {
		return fmt.Errorf("original receipt lost")
	}
	s, e := h.core.Load(ctx, cp.Ref)
	if e != nil {
		return e
	}
	switch point {
	case "input-committed":
		return demand(s.Task.State == "QUEUED" && len(s.Task.InputFacts) == 1 && len(s.Task.InputRefs) == 2)
	case "interaction-consumed":
		page, e := h.inputs.Interactions(ctx, cp.Ref)
		return demand(e == nil && page.Interactions[0].State == "answered" && s.Task.State == "QUEUED")
	case "limits-committed":
		return demand(s.Task.Constraints.ModelTokens == 1000 && s.Task.ModelReservedTokens == 700 && s.Task.Constraints.DeadlineUnix == int64(cp.Adjust.Patch.Deadline.Value) && s.Work[0].InFlight)
	case "generation-unknown":
		if s.Task.ModelReservedTokens != 700 || s.Generations[0].Started != 1 || !s.Work[0].InFlight {
			return fmt.Errorf("unknown generation refunded")
		}
		m := &controlledModel{}
		if _, e = h.run(ctx, s, m); !authorization.Is(e, authorization.Conflict) || m.calls != 0 {
			return fmt.Errorf("replacement dispatched before stop")
		}
		// The parent has observed child exit 73. That is evidence that this local
		// scripted generation stopped, not evidence of zero provider consumption.
		stopped, e := h.port.Observe(ctx, tasks.ScriptObservation(s, "STOPPED"))
		if e != nil {
			return e
		}
		out, e := h.run(ctx, stopped, m)
		return demand(e == nil && out.Task.State == "COMPLETED" && out.Task.ModelReservedTokens == 700 && out.Task.ModelUsedTokens == 70 && m.calls == 1)
	}
	return fmt.Errorf("unknown recovery case")
}
