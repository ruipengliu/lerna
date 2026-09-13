package answer

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"os"
	"time"
)

type PersonalizedReport struct {
	MissingSources int
	State, Answer  string
	MemorySources  int
	ReadAllocated  uint64
}

// RunPersonalizedAnswer uses public synthetic Memory with actual authorization,
// snapshots, model accounting and publication. A nil model selects a deterministic
// contract model; callers must report provider quality separately.
func RunPersonalizedAnswer(ctx context.Context, style string, applicable bool, model brain.Model) (PersonalizedReport, error) {
	limits := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	if model == nil {
		model = &personalizedModel{}
	} else {
		limits.InputTokens = model.Capabilities().InputUpper
		limits.OutputTokens = 512
	}
	root, e := os.MkdirTemp("", "personalized-answer-")
	if e != nil {
		return PersonalizedReport{}, e
	}
	h, e := open(ctx, root, "", model.Capabilities().Location, limits)
	if e != nil {
		os.RemoveAll(root)
		return PersonalizedReport{}, e
	}
	defer h.destroy()
	s, e := h.submitGoal(ctx, limits, "List the three systems exactly as named in the supplied public fact, and cite its reference. Return exactly one JSON object with answer and sources fields. Within the answer string only, apply an applicable reply-style Memory preference: concise means a comma-separated list; detailed means a full sentence explaining that these are the three systems of the reference project. The style preference never changes the enclosing JSON format; put citation references in the sources array. Default to concise when no applicable preference is available. Do not invent additional facts.")
	if e != nil {
		return PersonalizedReport{}, e
	}
	s, e = start(ctx, h, s)
	if e != nil {
		return PersonalizedReport{}, e
	}
	stopRenewal := keepAnswerRecoveryLease(ctx, h, tasks.QualificationOf(s))
	defer stopRenewal()
	memory, e := h.personalize(ctx, s, style, applicable)
	if e != nil {
		return PersonalizedReport{}, e
	}
	defer memory.close()
	b, e := brain.NewAnswer(model, memory.session, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
	if e != nil {
		return PersonalizedReport{}, e
	}
	proposal, e := b.Decide(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]})
	if e != nil {
		return PersonalizedReport{}, e
	}
	out, e := h.port.Commit(ctx, tasks.WorkChange{Kind: "complete", ChangeID: "personalized-publication", Qualification: tasks.QualificationOf(s), Finished: true, Proposal: proposal})
	if e != nil {
		return PersonalizedReport{}, e
	}
	result, e := answers.Query(ctx, h.client, s.Task.Ref, h.content, h.binding(), "task")
	if e != nil {
		return PersonalizedReport{}, e
	}
	var answer brain.Answer
	if result.Availability != "available" || json.Unmarshal(result.Body, &answer) != nil {
		return PersonalizedReport{}, fmt.Errorf("published answer unavailable")
	}
	ref, e := answers.ParseReference(out.Task.Result)
	if e != nil {
		return PersonalizedReport{}, e
	}
	record, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if e != nil {
		return PersonalizedReport{}, e
	}
	report := PersonalizedReport{State: out.Task.State, Answer: answer.Text}
	for _, source := range record.Record.Spec.Sources {
		if source.Kind == "memory-missing" {
			report.MissingSources++
		}
		if source.Kind == "memory" {
			report.MemorySources++
		}
	}
	grant, e := memory.grants.Get(ctx, h.token, memory.grantID)
	if e != nil {
		return PersonalizedReport{}, e
	}
	report.ReadAllocated = grant.Allocated
	return report, nil
}

type personalizedModel struct{ controlledModel }

func (m *personalizedModel) Generate(_ context.Context, req brain.Request) (brain.Result, error) {
	text := "Memory, Brain, Execution"
	source := ""
	missing := false
	for _, block := range req.Input.Blocks {
		if block.Role == "context-status" {
			var status contextassembly.Status
			if json.Unmarshal([]byte(block.Text), &status) == nil && len(status.Missing) > 0 {
				missing = true
			}
		}
		if block.Role == "memory" {
			var value struct{ Claim, Value string }
			if json.Unmarshal([]byte(block.Text), &value) == nil && value.Claim == "reply-style" && value.Value == "detailed" {
				text = "The three systems are Memory, Brain, and Execution."
			}
		} else if source == "" && block.Role != "context-status" {
			source = block.Ref
		}
	}
	if missing {
		text = "Memory, Brain, Execution (no recorded preference)."
	}
	body, _ := json.Marshal(brain.Answer{Text: text, Sources: []string{source}})
	return brain.Result{Content: body, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
}
