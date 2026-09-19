package tasks_test

import (
	"context"
	sqliteauth "lerna/adapters/authorization/sqlite"
	tasklocal "lerna/adapters/tasks/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"strings"
	"testing"
	"time"
)

func enableExecution(t *testing.T, a *authorization.Service, token string, c *clock) {
	t.Helper()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope}}}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "execution", Subject: "admin", Scope: scope, Mode: "continuous"}}})
}
func limits() tasks.RunLimits {
	return tasks.RunLimits{Lease: time.Second, RenewEvery: 200 * time.Millisecond, DecisionTimeout: time.Second, IOTimeout: time.Second, MaxAttempts: 3, MaxConcurrent: 1}
}
func workerPort(t *testing.T, s *tasks.Service, token, id string) *tasks.WorkPort {
	t.Helper()
	p, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: id}, limits())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestWorkerCompletesThroughSDK(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	client := sdk.NewTaskClient(tasklocal.Bind(s, token), "local")
	task, err := client.Submit(ctx, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	brain := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		if in.Task.State != "RUNNING" || in.Task.Version != 2 || in.Task.Attempts != 1 || in.RemainingSteps != 1 {
			t.Errorf("unreserved input: %+v", in)
		}
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "scripted answer"}, nil
	})
	runner, err := tasks.NewRunner(workerPort(t, s, token, "worker-a"), brain, limits(), c)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.Run(ctx, snap); err != nil {
		t.Fatal(err)
	}
	got, err := client.Get(ctx, task.Ref)
	if err != nil || got.State != "COMPLETED" || got.Result != "scripted answer" || got.Version != 3 || got.Attempts != 1 {
		t.Fatalf("complete: %+v %v", got, err)
	}
	page, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
	if err != nil || len(page.Runs) != 0 {
		t.Fatalf("terminal candidate: %+v %v", page, err)
	}
}

func TestRunnerTimeoutConsumesBudgetAndStops(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.MaxSteps = 1
	task, err := s.Submit(ctx, token, in)
	if err != nil {
		t.Fatal(err)
	}
	cfg := limits()
	cfg.DecisionTimeout = 10 * time.Millisecond
	port, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "slow"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	brain := tasks.BrainFunc(func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		close(started)
		<-release
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "late"}, nil
	})
	runner, err := tasks.NewRunner(port, brain, cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, snap); done <- err }()
	<-started
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("unbounded Brain timeout")
	}
	got, err := s.Get(ctx, token, task.Ref)
	if err != nil || got.State != "WAITING" || got.StopReason != "budget" || got.Attempts != 1 {
		t.Fatalf("lost budget: %+v %v", got, err)
	}
	close(release)
}

func TestLeaseReclaimFencesOldWorkerAndRenewPreservesVersion(t *testing.T) {
	s, a, token, c, path := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a2, err := authorization.New(db, c, authConfig())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := tasks.New(a2, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	p1, p2 := workerPort(t, s, token, "a"), workerPort(t, s2, token, "b")
	candidate, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	claim := tasks.WorkChange{ChangeID: "claim-a", Kind: "claim", Qualification: tasks.QualificationOf(candidate)}
	first, err := p1.Commit(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p2.Commit(ctx, tasks.WorkChange{ChangeID: "claim-b", Kind: "claim", Qualification: claim.Qualification}); !authorization.Is(err, authorization.Conflict) {
		t.Fatalf("two claims: %v", err)
	}
	renewed, err := p1.Commit(ctx, tasks.WorkChange{ChangeID: "renew-a", Kind: "renew", Qualification: tasks.QualificationOf(first)})
	if err != nil || renewed.Task.Version != 2 || renewed.Work[0].Generation != 1 {
		t.Fatalf("renewal: %+v %v", renewed, err)
	}
	c.now = c.now.Add(2 * time.Second)
	second, err := p2.Commit(ctx, tasks.WorkChange{ChangeID: "reclaim-b", Kind: "claim", Qualification: tasks.QualificationOf(renewed)})
	if err != nil {
		t.Fatal(err)
	}
	if second.Work[0].ID != first.Work[0].ID || second.Work[0].Generation != 2 || second.Task.Attempts != 2 || second.Task.Version != 3 {
		t.Fatalf("reclaim: %+v", second)
	}
	for _, kind := range []string{"renew", "complete"} {
		ch := tasks.WorkChange{ChangeID: "old-" + kind, Kind: kind, Qualification: tasks.QualificationOf(first)}
		if kind == "complete" {
			ch.Proposal = tasks.Proposal{Kind: "result", BaseVersion: 2, Complete: true, Result: "old"}
		}
		if _, err = p1.Commit(ctx, ch); !authorization.Is(err, authorization.Conflict) {
			t.Fatalf("old %s accepted: %v", kind, err)
		}
	}
	done := tasks.WorkChange{ChangeID: "finish", Kind: "complete", Qualification: tasks.QualificationOf(second), Proposal: tasks.Proposal{Kind: "result", BaseVersion: 3, Complete: true, Result: "current"}}
	completed, err := p2.Commit(ctx, done)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p2.Commit(ctx, done)
	if err != nil || again.Task.Version != completed.Task.Version {
		t.Fatalf("completion replay: %+v %v", again, err)
	}
	done.Proposal.Result = "changed"
	if _, err = p2.Commit(ctx, done); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	if _, err = p2.Commit(ctx, tasks.WorkChange{ChangeID: "resurrect", Kind: "claim", Qualification: tasks.QualificationOf(completed)}); !authorization.Is(err, authorization.Conflict) {
		t.Fatalf("terminal reopened: %v", err)
	}
}

func TestInternalCreationCannotForgeExecution(t *testing.T) {
	s, _, _, c, _ := setup(t)
	_, err := s.Commit(context.Background(), tasks.RunChange{ChangeID: "forge", MustNotExist: true, Task: tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "forge"}, Goal: "answer", Subject: "admin", Resource: "root", Owner: "local-owner", OwnerEpoch: 1, Version: 1, State: "QUEUED", Attempts: 1, Result: "forged", Constraints: tasks.Constraints{MaxSteps: 1, DeadlineUnix: c.now.Add(time.Minute).Unix()}}, Work: []tasks.Work{{ID: "forge/initial", Kind: "decide", Worker: "forged", Generation: 9}}, Records: []tasks.Record{{Kind: "submitted", Version: 1}}})
	if err == nil {
		t.Fatal("forged execution admitted")
	}
}

type delayedClaimStore struct {
	authorization.Store
	clock *clock
	armed bool
}

func (s *delayedClaimStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	err := s.Store.Commit(ctx, v, state)
	if err == nil && s.armed {
		s.armed = false
		s.clock.now = s.clock.now.Add(2 * time.Second)
	}
	return err
}
func TestDelayedClaimReplyCannotStartExpiredDecision(t *testing.T) {
	s, a, token, c, path := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	delayed := &delayedClaimStore{Store: db, clock: c, armed: true}
	auth, err := authorization.New(delayed, c, authConfig())
	if err != nil {
		t.Fatal(err)
	}
	service, err := tasks.New(auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	brain := tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
		t.Error("expired qualification reached Brain")
		return tasks.Proposal{}, nil
	})
	runner, err := tasks.NewRunner(workerPort(t, service, token, "delayed"), brain, limits(), c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(ctx, snap)
	if !authorization.Is(err, authorization.Conflict) {
		t.Fatalf("expired qualification: %v", err)
	}
}

func TestOversizedProposalCanStopWithoutPersistingItsPayload(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	port := workerPort(t, s, token, "worker-a")
	claimed, err := port.Commit(ctx, tasks.WorkChange{ChangeID: "claim", Kind: "claim", Qualification: tasks.QualificationOf(snap)})
	if err != nil {
		t.Fatal(err)
	}
	request := tasks.WorkChange{ChangeID: "oversized", Kind: "complete", Qualification: tasks.QualificationOf(claimed), Proposal: tasks.Proposal{Kind: "result", BaseVersion: claimed.Task.Version, Complete: true, Result: strings.Repeat("x", 9<<20)}}
	stopped, err := port.Commit(ctx, request)
	if err != nil || stopped.Task.State != "FAILED" || stopped.Task.StopReason != "invalid_proposal" {
		t.Fatalf("oversized proposal prevented durable stop: %+v %v", stopped, err)
	}
	replay, err := port.Commit(ctx, request)
	if err != nil || replay.LastCommit != stopped.LastCommit {
		t.Fatalf("rejected proposal replay: %+v %v", replay, err)
	}
	request.Proposal.Result += "different"
	if _, err = port.Commit(ctx, request); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("changed invalid proposal reused identity: %v", err)
	}
}
