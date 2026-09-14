package tasks_test

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"lerna/tasks"
	"testing"
	"time"
)

type delegationModel struct {
	body  []byte
	calls int
}

func (m *delegationModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "deterministic-delegation", Version: "1", Location: "local", Text: true, Structured: true, HardBounds: true, InputUpper: 100, ContextTokens: 4096}
}
func (m *delegationModel) Generate(_ context.Context, r brain.Request) (brain.Result, error) {
	m.calls++
	if r.Contract != brain.DelegationContract {
		return brain.Result{}, context.Canceled
	}
	return brain.Result{Content: m.body, Finish: "stop", Usage: brain.Usage{Known: true, Input: 90, Output: 80}}, nil
}

type delegationContext struct{ data []byte }

func (c *delegationContext) Assemble(_ context.Context, t tasks.Task, _ string, _ int) (brain.Input, error) {
	return brain.Input{Goal: t.Goal, Blocks: []brain.Block{{Ref: "child-owner", Role: "internal-agent", Text: "synthetic child with object contract"}}}, nil
}
func (c *delegationContext) Validate(context.Context, tasks.Task, string) error { return nil }
func (c *delegationContext) Save(_ context.Context, _ tasks.DecisionInput, data []byte) (string, error) {
	c.data = append([]byte(nil), data...)
	return "controlled-delegation-proposal", nil
}
func TestDelegationBrainUsesExistingGenerationAccounting(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints = tasks.Constraints{MaxSteps: 8, ModelRequests: 1, ModelTokens: 2048, DeadlineUnix: c.now.Add(time.Minute).Unix()}
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	w := workerPort(t, s, token, "parent-worker")
	r, _ := s.Load(ctx, task.Ref)
	for _, kind := range []string{"claim", "start"} {
		r, e = w.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			t.Fatal(e)
		}
	}
	g, e := w.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 1024, OutputTokens: 1024})
	if e != nil {
		t.Fatal(e)
	}
	g = g.WithOutputIdentities(func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	r, e = g.ReserveDecision(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	schema := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:child:answer","type":"object"}`)
	body, _ := json.Marshal(brain.DelegationOutput{Children: []tasks.ChildSpec{{Key: "a", Agent: "child-owner", Goal: "independent answer", Acceptance: "verify artifacts", Input: []byte(`{}`), InputSchema: schema, ResultSchema: schema, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: in.Constraints.DeadlineUnix}}}})
	model := &delegationModel{body: body}
	environment := &delegationContext{}
	b, e := brain.NewDelegations(model, environment, environment, g, brain.Config{MaxInputBytes: 8192, MaxOutputBytes: 8192, SettlementTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	proposal, e := b.Decide(ctx, tasks.DecisionInput{Task: r.Task, Work: r.Work[0], Generation: r.Generations[0]})
	if e != nil {
		t.Fatal(e)
	}
	if proposal.Kind != "delegate" || proposal.Complete || model.calls != 1 {
		t.Fatal(proposal, model.calls)
	}
	r, e = s.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if r.Task.ModelUsedRequests != 1 || r.Task.ModelUsedTokens != 170 || r.Task.ModelReservedTokens != 0 {
		t.Fatal("unmetered delegation", r.Task)
	}
	input, e := environment.Assemble(ctx, r.Task, "local", 8192)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := brain.PrepareDelegation(ctx, environment.data, input, func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	if e != nil {
		t.Fatal(e)
	}
	port, e := w.Delegations(tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, publicDelegationData)
	if e != nil {
		t.Fatal(e)
	}
	r, e = port.Admit(ctx, tasks.QualificationOf(r), prepared)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Delegations.Children) != 1 || r.Task.DelegatedSteps != 2 {
		t.Fatal("proposal skipped Core admission")
	}
	t.Log("finite scripted Brain proposal: one real Core generation reservation, 170 known tokens, one atomic child reservation")
}
