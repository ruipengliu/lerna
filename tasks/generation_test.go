package tasks_test

import (
	"context"
	"lerna/tasks"
	"testing"
)

func TestGenerationReservationPrecedesDecisionVersion(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 2
	in.Constraints.ModelTokens = 200
	task, err := s.Submit(ctx, token, in)
	if err != nil {
		t.Fatal(err)
	}
	p := workerPort(t, s, token, "model-worker")
	snap, _ := s.Load(ctx, task.Ref)
	claimed, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "model-claim", Kind: "claim", Qualification: tasks.QualificationOf(snap)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "model-start", Kind: "start", Qualification: tasks.QualificationOf(claimed)})
	if err != nil {
		t.Fatal(err)
	}
	g, err := p.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := g.ReserveDecision(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Task.Version != started.Task.Version+1 || reserved.Task.ModelReservedTokens != 100 {
		t.Fatalf("reservation not durable: %+v", reserved.Task)
	}
	again, err := g.ReserveDecision(ctx, started)
	if err != nil || again.Task.ModelReservedTokens != 100 {
		t.Fatalf("double reserved: %+v %v", again, err)
	}
	if err = g.BeginRequest(ctx, tasks.QualificationOf(reserved), 0); err != nil {
		t.Fatal(err)
	}
	if err = g.Settle(ctx, tasks.QualificationOf(reserved), tasks.GenerationUsage{Requests: 1, Tokens: 30}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, token, task.Ref)
	if err != nil || got.ModelUsedTokens != 30 || got.ModelReservedTokens != 0 {
		t.Fatalf("settlement: %+v %v", got, err)
	}
}

// A completed request with unknown usage must be reconciled against its original
// ordinal; replay cannot release the reservation twice or start another request.
func TestLateGenerationUsage(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 1
	in.Constraints.ModelTokens = 100
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	p := workerPort(t, s, token, "model-worker")
	snap, _ := s.Load(ctx, task.Ref)
	snap, e = p.Commit(ctx, tasks.WorkChange{ChangeID: "claim", Kind: "claim", Qualification: tasks.QualificationOf(snap)})
	if e != nil {
		t.Fatal(e)
	}
	snap, e = p.Commit(ctx, tasks.WorkChange{ChangeID: "start", Kind: "start", Qualification: tasks.QualificationOf(snap)})
	if e != nil {
		t.Fatal(e)
	}
	g, _ := p.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	snap, e = g.ReserveDecision(ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	q := tasks.QualificationOf(snap)
	if e = g.BeginRequest(ctx, q, 0); e != nil {
		t.Fatal(e)
	}
	if e = g.BeginRequest(ctx, q, 0); e == nil {
		t.Fatal("a replay must not authorize duplicate dispatch")
	}
	if e = g.Settle(ctx, q, tasks.GenerationUsage{Requests: 1, UnknownRequests: 1}); e != nil {
		t.Fatal(e)
	}
	if e = g.Settle(ctx, q, tasks.GenerationUsage{Requests: 1, Tokens: 30}); e != nil {
		t.Fatal(e)
	}
	if e = g.Settle(ctx, q, tasks.GenerationUsage{Requests: 1, Tokens: 30}); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Get(ctx, token, task.Ref)
	if got.ModelUsedTokens != 30 || got.ModelReservedTokens != 0 || got.ModelUsedRequests != 1 {
		t.Fatalf("late usage %+v", got)
	}
}

func TestConcurrentGenerationReservationAndWorkerFence(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 1
	in.Constraints.ModelTokens = 100
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	p := workerPort(t, s, token, "model-worker")
	initial, _ := s.Load(ctx, task.Ref)
	claimed, e := p.Commit(ctx, tasks.WorkChange{Kind: "claim", ChangeID: "claim", Qualification: tasks.QualificationOf(initial)})
	if e != nil {
		t.Fatal(e)
	}
	started, e := p.Commit(ctx, tasks.WorkChange{Kind: "start", ChangeID: "start", Qualification: tasks.QualificationOf(claimed)})
	if e != nil {
		t.Fatal(e)
	}
	g, _ := p.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, e := g.ReserveDecision(ctx, started); results <- e }()
	}
	for i := 0; i < 4; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	got, e := s.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Generations) != 1 || got.Task.ModelReservedTokens != 100 || got.Task.ModelReservedRequests != 1 {
		t.Fatal("concurrent reservation enlarged budget")
	}
	other, _ := workerPort(t, s, token, "other-worker").Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	if e := other.Settle(ctx, tasks.QualificationOf(got), tasks.GenerationUsage{}); e == nil {
		t.Fatal("wrong worker settled reservation")
	}
	if e := g.BeginRequest(ctx, tasks.QualificationOf(started), 0); e == nil {
		t.Fatal("old proposal version dispatched")
	}
}
