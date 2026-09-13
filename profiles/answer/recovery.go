package answer

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/brain"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
)

var crashPoints = []string{"reserved-before-dispatch", "request-outcome-unknown", "output-before-admission", "publication-reply-lost"}

type checkpoint struct {
	Token string
	Ref   tasks.Ref
}

func RunProbe(ctx context.Context, root, point string) error {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := open(ctx, root, "", "test-model-location", l)
	if e != nil {
		return e
	}
	defer h.close()
	s, e := h.submit(ctx, l)
	if e != nil {
		return e
	}
	s, e = start(ctx, h, s)
	if e != nil {
		return e
	}
	data, _ := json.Marshal(checkpoint{h.token, s.Task.Ref})
	if e = os.WriteFile(filepath.Join(root, "checkpoint.json"), data, 0600); e != nil {
		return e
	}
	if point == "reserved-before-dispatch" {
		os.Exit(73)
	}
	q := tasks.QualificationOf(s)
	if e = h.generation.BeginRequest(ctx, q, 0); e != nil {
		return e
	}
	if point == "request-outcome-unknown" {
		os.Exit(73)
	}
	m := &controlledModel{}
	input, e := h.access.Assemble(ctx, s.Task, h.location, brain.MaxInputBytes)
	if e != nil {
		return e
	}
	result, e := m.Generate(ctx, brain.Request{Input: input, MaxInput: 600, MaxOutput: 100})
	if e != nil {
		return e
	}
	if e = brain.ValidateAnswer(result.Content, input, brain.MaxAnswerBytes); e != nil {
		return e
	}
	ref, e := h.access.Save(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]}, result.Content)
	if e != nil {
		return e
	}
	if e = h.generation.Settle(ctx, q, tasks.GenerationUsage{Requests: 1, Tokens: 70}); e != nil {
		return e
	}
	c := tasks.WorkChange{Kind: "complete", ChangeID: "original-publication", Qualification: q, Finished: true, Proposal: tasks.Proposal{Kind: "answer", BaseVersion: q.Version, Complete: true, Result: ref}}
	if point == "output-before-admission" {
		os.Exit(73)
	}
	if _, e = h.port.Commit(ctx, c); e != nil {
		return e
	}
	if point == "publication-reply-lost" {
		os.Exit(73)
	}
	return fmt.Errorf("unknown crash point")
}
func recovery(ctx context.Context, executable, point string) error {
	root, e := os.MkdirTemp("", "answer-crash-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	cmd := exec.CommandContext(ctx, executable, "answer-crash-probe", root, point)
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 73 {
		return fmt.Errorf("crash probe %s: %v", point, err)
	}
	raw, e := os.ReadFile(filepath.Join(root, "checkpoint.json"))
	if e != nil {
		return e
	}
	var saved checkpoint
	if e = json.Unmarshal(raw, &saved); e != nil {
		return e
	}
	h, e := open(ctx, root, saved.Token, "test-model-location", tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if e != nil {
		return e
	}
	defer h.close()
	s, e := h.core.Load(ctx, saved.Ref)
	if e != nil {
		return e
	}
	switch point {
	case "reserved-before-dispatch", "request-outcome-unknown":
		if s.Task.ModelReservedTokens != 700 || s.Task.ModelUsedTokens != 0 || len(s.Generations) != 1 {
			return fmt.Errorf("lost unknown reservation")
		}
		want := uint32(0)
		if point == "request-outcome-unknown" {
			want = 1
		}
		if s.Generations[0].Started != want {
			return fmt.Errorf("lost dispatch ordinal")
		}
		_, e = h.port.Recover(ctx, saved.Ref)
		return demand(e != nil)
	case "output-before-admission", "publication-reply-lost":
		got, e := h.port.Recover(ctx, saved.Ref)
		if e != nil {
			return e
		}
		again, e := h.port.Recover(ctx, saved.Ref)
		if e != nil {
			return e
		}
		return demand(got.Task.State == "COMPLETED" && got.Task.Result == again.Task.Result && got.Task.Version == again.Task.Version && got.Task.ModelUsedTokens == 70 && got.Task.ModelReservedTokens == 0)
	}
	return fmt.Errorf("unknown recovery point")
}
