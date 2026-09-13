package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"lerna/tasks"
	"testing"
	"time"
)

func TestResearchActionContextRejectsRevokedGoalWithReadableInputs(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx, []string{"https://source.example/start", "https://source.example/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	raw, _ := json.Marshal(map[string]any{"url": h.urls[0], "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	ref, err := h.put(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Acquire authorized evidence", InputRefs: []string{ref}, Constraints: tasks.Constraints{MaxSteps: 2, ModelRequests: 2, ModelTokens: 16384, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claim", "start"} {
		run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
		if err != nil {
			t.Fatal(err)
		}
	}
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: 2, MaxQueries: 32, InputTokens: 8192, OutputTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
		t.Fatal(err)
	}
	host := &fetchActionHost{h: h, queries: port}
	if _, err = host.Assemble(ctx, run.Task, "local", brain.MaxInputBytes); err != nil {
		t.Fatal(err)
	}
	state, err := h.policyStore.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rules := state.Rules[:0]
	for _, rule := range state.Rules {
		if rule.Kind != "task-goal" {
			rules = append(rules, rule)
		}
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	// The original content remains readable; only the inline goal was revoked.
	if _, err = h.access.Read(ctx, h.token, ref, h.cap); err != nil {
		t.Fatal(err)
	}
	input, assembleErr := host.Assemble(ctx, run.Task, "local", brain.MaxInputBytes)
	if assembleErr == nil || input.Goal != "" || len(input.Blocks) != 0 {
		t.Error("revoked goal entered action-model input")
	}
	if err = host.Validate(ctx, run.Task, "local"); err == nil {
		t.Error("action boundary accepted revoked goal")
	}
}
