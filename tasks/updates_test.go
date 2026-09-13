package tasks_test

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"testing"
	"time"
)

func enableUpdates(t *testing.T, a *authorization.Service, token string, c *clock) {
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.input", "task.append", "task.revise", "task.adjust", "task.pause", "task.cancel", "task.resume"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "updates", Scope: scope}}}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "updates", Subject: "admin", Mode: "continuous", Scope: scope}}})
}

type inputFixture struct{}

func (inputFixture) ValidateInput(context.Context, string, tasks.Task, []string, *tasks.AnswerConstraint) error {
	return nil
}
func updateLimits() tasks.UpdateLimits {
	return tasks.UpdateLimits{MaxOperations: 32, MaxInteractions: 16, MaxRefs: 16, IOTimeout: time.Second, Control: tasks.ControlLimits{MaxOperations: 32, MaxObservations: 32, MaxChecks: 4, PollInterval: 100 * time.Millisecond, StopTimeout: time.Second, IOTimeout: time.Second}}
}
func TestAppendInputIsDurableAndReplayable(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Updates(inputFixture{}, updateLimits())
	if e != nil {
		t.Fatal(e)
	}
	op, e := a.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	in := tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: task.Version, Intent: "append", InputRefs: []string{"evidence-1"}}
	receipt, e := u.SubmitUpdate(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, token, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if got.Version != 2 || len(got.InputRefs) != 1 {
		t.Fatalf("update lost: %+v", got)
	}
	replay, e := u.SubmitUpdate(ctx, token, in)
	if e != nil || replay != receipt {
		t.Fatalf("replay %v", e)
	}
	snap, e := s.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Task.InputFacts) != 1 || snap.Task.InputFacts[0].Subject != "admin" {
		t.Fatal("missing provenance")
	}
	in.InputRefs = []string{"changed"}
	if _, e = u.SubmitUpdate(ctx, token, in); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("changed identity: %v", e)
	}
}

func TestWaitingResponseConsumesOnlyItsInteraction(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	u, _ := s.Updates(inputFixture{}, updateLimits())
	snap, _ := s.Load(ctx, task.Ref)
	created, e := u.CreateWait(ctx, token, tasks.WaitRequest{ChangeID: "wait", Qualification: tasks.QualificationOf(snap), Questions: []tasks.Question{{QuestionRef: "question", Constraint: tasks.AnswerConstraint{Kind: "text", MaxBytes: 100}, Dependency: "goal", Responder: "admin", ExpiresUnix: c.now.Add(time.Minute).Unix()}}})
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	in := tasks.InputRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: created.Version, InteractionID: created.Interactions[0].ID, AnswerRef: "answer"}
	r, e := u.ProvideInput(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := u.ProvideInput(ctx, token, in)
	if e != nil || again != r {
		t.Fatalf("response replay %v", e)
	}
	view, e := u.Interactions(ctx, token, task.Ref)
	if e != nil || view.Interactions[0].State != "answered" {
		t.Fatalf("waiting state %v", e)
	}
	other, _ := a.NewOperation(ctx, token)
	in.OperationID = other
	in.ExpectedVersion = view.Version
	if _, e = u.ProvideInput(ctx, token, in); e == nil {
		t.Fatal("interaction consumed twice")
	}
	got, _ := s.Get(ctx, token, task.Ref)
	if got.State != "QUEUED" || len(got.InputRefs) != 2 {
		t.Fatalf("not resumed %+v", got)
	}
}

func TestLimitAdjustmentCannotEraseReservedGeneration(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 2
	in.Constraints.ModelTokens = 200
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	p := workerPort(t, s, token, "worker")
	snap, _ := s.Load(ctx, task.Ref)
	for _, kind := range []string{"claim", "start"} {
		snap, e = p.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(snap)})
		if e != nil {
			t.Fatal(e)
		}
	}
	g, _ := p.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	snap, e = g.ReserveDecision(ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := s.Updates(inputFixture{}, updateLimits())
	op, _ := a.NewOperation(ctx, token)
	req := tasks.AdjustRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: snap.Task.Version, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 99}}}
	if _, e = u.AdjustLimits(ctx, token, req); !authorization.Is(e, authorization.Invalid) {
		t.Fatalf("below reserved floor: %v", e)
	}
	got, _ := s.Get(ctx, token, task.Ref)
	if got.Constraints.ModelTokens != 200 || got.ModelReservedTokens != 100 || got.Version != snap.Task.Version {
		t.Fatal("partial limit update")
	}
	req.Patch.Tokens.Value = 100
	r, e := u.AdjustLimits(ctx, token, req)
	if e != nil {
		t.Fatal(e)
	}
	again, e := u.AdjustLimits(ctx, token, req)
	if e != nil || again != r {
		t.Fatal("limit replay")
	}
	got, _ = s.Get(ctx, token, task.Ref)
	if got.Constraints.ModelTokens != 100 || got.ModelReservedTokens != 100 || got.ModelReservedRequests != 1 {
		t.Fatal("reservation erased")
	}
}

func TestUpdateCancelsOldDecisionAndQueuesCurrentFacts(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	u, _ := s.Updates(inputFixture{}, updateLimits())
	p := workerPort(t, s, token, "worker")
	started := make(chan struct{})
	stopped := make(chan struct{})
	b := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "stale"}, nil
	})
	r, _ := tasks.NewRunner(p, b, limits(), c)
	snap, _ := s.Load(ctx, task.Ref)
	done := make(chan error, 1)
	go func() { _, e := r.Run(ctx, snap); done <- e }()
	<-started
	current, _ := s.Get(ctx, token, task.Ref)
	op, _ := a.NewOperation(ctx, token)
	_, e = u.SubmitUpdate(ctx, token, tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: current.Version, Intent: "append", InputRefs: []string{"new-fact"}})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old decision not interrupted")
	}
	<-stopped
	next, e := s.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if next.Task.State != "QUEUED" || next.Work[0].InFlight || next.Task.Result != "" {
		t.Fatalf("bad recovery %+v", next.Task)
	}
	nextBrain := tasks.BrainFunc(func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		if len(in.Task.InputRefs) != 1 || in.Task.InputRefs[0] != "new-fact" {
			return tasks.Proposal{}, context.Canceled
		}
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "current"}, nil
	})
	runner, _ := tasks.NewRunner(p, nextBrain, limits(), c)
	got, e := runner.Run(ctx, next)
	if e != nil || got.Task.Result != "current" || got.Task.Attempts != 2 {
		t.Fatalf("next decision %+v %v", got.Task, e)
	}
}

func TestAppendPermissionCannotReviseOrAdjust(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.append", "task.read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 4, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "append-only", Scope: scope}}}}})
	u, _ := s.Updates(inputFixture{}, updateLimits())
	op, _ := a.NewOperation(ctx, token)
	_, e = u.SubmitUpdate(ctx, token, tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: task.Version, Intent: "revise", GoalRef: "goal"})
	if !authorization.Is(e, authorization.Denied) {
		t.Fatalf("revise permission: %v", e)
	}
	_, e = u.AdjustLimits(ctx, token, tasks.AdjustRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: task.Version, Patch: tasks.LimitPatch{Steps: tasks.LimitValue{Present: true, Value: 5}}})
	if !authorization.Is(e, authorization.Denied) {
		t.Fatalf("adjust permission: %v", e)
	}
	_, e = u.SubmitUpdate(ctx, token, tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: task.Version, Intent: "append", InputRefs: []string{"fact"}})
	if e != nil {
		t.Fatal(e)
	}
	got, _ := s.Get(ctx, token, task.Ref)
	if got.Goal != task.Goal || got.Constraints != task.Constraints || len(got.InputFacts) != 1 || got.InputFacts[0].Subject != "admin" {
		t.Fatal("append escalated")
	}
}

func TestPriorEffectDoesNotCompleteUpdatedGoal(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "source", AllowEffectEvidence: true}, limits())
	if e != nil {
		t.Fatal(e)
	}
	run, _ := s.Load(ctx, task.Ref)
	for _, kind := range []string{"claim", "start"} {
		run, e = p.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
		if e != nil {
			t.Fatal(e)
		}
	}
	u, _ := s.Updates(inputFixture{}, updateLimits())
	op, _ := a.NewOperation(ctx, token)
	_, e = u.SubmitUpdate(ctx, token, tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: run.Task.Version, Intent: "revise", GoalRef: "new-goal"})
	if e != nil {
		t.Fatal(e)
	}
	obs := tasks.Observation{ChangeID: "prior-effect", Qualification: tasks.QualificationOf(run), Status: "COMPLETED", Result: "old effect", Evidence: "fixture", CompletedAt: c.now.UnixNano()}
	got, e := p.Observe(ctx, obs)
	if e != nil {
		t.Fatal(e)
	}
	if got.Task.State != "QUEUED" || got.Task.Result != "" || got.Work[0].InFlight || got.Work[0].Done {
		t.Fatal("old effect completed new goal")
	}
	replay, e := p.Observe(ctx, obs)
	if e != nil || replay.LastCommit != got.LastCommit {
		t.Fatal("effect evidence not replayable")
	}
}

type beforeReserve struct {
	*tasks.GenerationPort
	before func(tasks.RunSnapshot)
}

func (p beforeReserve) ReserveDecision(ctx context.Context, s tasks.RunSnapshot) (tasks.RunSnapshot, error) {
	p.before(s)
	return p.GenerationPort.ReserveDecision(ctx, s)
}
func TestUpdateBetweenStartAndReserveStopsUnsentWork(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 1
	in.Constraints.ModelTokens = 100
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	p := workerPort(t, s, token, "worker")
	g, _ := p.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 60, OutputTokens: 40})
	u, _ := s.Updates(inputFixture{}, updateLimits())
	op, _ := a.NewOperation(ctx, token)
	port := beforeReserve{g, func(run tasks.RunSnapshot) {
		_, err := u.SubmitUpdate(ctx, token, tasks.UpdateRequest{OperationID: op, Ref: task.Ref, ExpectedVersion: run.Task.Version, Intent: "append", InputRefs: []string{"new-fact"}})
		if err != nil {
			t.Fatal(err)
		}
	}}
	calls := 0
	r, _ := tasks.NewRunner(port, tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
		calls++
		return tasks.Proposal{}, nil
	}), limits(), c)
	initial, _ := s.Load(ctx, task.Ref)
	out, e := r.Run(ctx, initial)
	if e != nil || out.Task.State != "QUEUED" || out.Work[0].InFlight || calls != 0 || out.Task.ModelReservedTokens != 0 {
		t.Fatalf("unsent work stranded: state=%s err=%v calls=%d", out.Task.State, e, calls)
	}
}
