package tasks_test

import (
	"context"
	"errors"
	sqliteauth "lerna/adapters/authorization/sqlite"
	tasklocal "lerna/adapters/tasks/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"slices"
	"testing"
	"time"
)

func controlLimits() tasks.ControlLimits {
	return tasks.ControlLimits{MaxOperations: 32, MaxObservations: 32, MaxChecks: 8, PollInterval: 10 * time.Millisecond, StopTimeout: 50 * time.Millisecond, IOTimeout: time.Second}
}
func enableControls(t *testing.T, a *authorization.Service, token string, c *clock) {
	t.Helper()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.pause", "task.cancel", "task.resume", "task.reconcile"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "controls", Scope: scope}}}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "controls", Subject: "admin", Mode: "continuous", Scope: scope}}})
}
func controlRequest(t *testing.T, a *authorization.Service, token string, task tasks.Task, intent string) tasks.ControlRequest {
	t.Helper()
	id, err := a.NewOperation(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return tasks.ControlRequest{OperationID: id, Ref: task.Ref, ExpectedVersion: task.Version, Intent: intent}
}
func TestPauseResumeAndCancelBeforeClaim(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	pause := controlRequest(t, a, token, task, "PAUSE")
	receipt, err := controls.Request(ctx, token, pause)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("pause: %+v %v", receipt, err)
	}
	got, err := s.Get(ctx, token, task.Ref)
	if err != nil || got.State != "WAITING" || got.Control.Intent != "PAUSE" || got.Control.Progress != "APPLIED" {
		t.Fatalf("paused: %+v %v", got, err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, got, "RUN")); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, token, task.Ref)
	if err != nil || got.State != "QUEUED" || got.Attempts != 0 {
		t.Fatalf("resume: %+v %v", got, err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, got, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, token, task.Ref)
	if err != nil || got.State != "CANCELLED" || got.Control.Progress != "APPLIED" {
		t.Fatalf("cancel: %+v %v", got, err)
	}
	replay, err := controls.Request(ctx, token, pause)
	if err != nil || replay != receipt {
		t.Fatalf("historical receipt changed: %+v %v", replay, err)
	}
}

func TestControlSDKShowsReceiptAndCurrentProgress(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	transport := tasklocal.BindManaged(s, controls, token)
	client := sdk.NewTaskClient(transport, "local")
	controlClient := sdk.NewControlClient(transport, "local")
	task, err := client.Submit(ctx, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	req := controlRequest(t, a, token, task, "CANCEL")
	receipt, err := controlClient.Request(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Get(ctx, task.Ref)
	if err != nil || got.State != "CANCELLED" || got.Control.OperationID != req.OperationID {
		t.Fatalf("SDK control state: %+v %v", got, err)
	}
	old, err := controlClient.Lookup(ctx, req.OperationID)
	if err != nil || old != receipt {
		t.Fatalf("SDK control receipt: %+v %v", old, err)
	}
}

func TestRunningPauseWaitsForActualStop(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	brain := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		close(started)
		<-release
		defer close(exited)
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "late proposal"}, nil
	})
	runner, err := tasks.NewRunner(workerPort(t, s, token, "worker-a"), brain, limits(), c)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, candidate); done <- err }()
	<-started
	current, err := s.Get(ctx, token, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, current, "PAUSE")); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("control not bounded")
	}
	current, err = s.Get(ctx, token, task.Ref)
	if err != nil || current.Control.Progress == "APPLIED" || !slices.Contains(current.WaitingReasons, "reconciliation") {
		close(release)
		t.Fatalf("claimed stopped early: %+v %v", current, err)
	}
	close(release)
	<-exited
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		current, err = s.Get(ctx, token, task.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if current.Control.Progress == "APPLIED" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if current.Control.Progress != "APPLIED" || current.State != "WAITING" || current.Control.Intent != "PAUSE" || current.Result != "" {
		t.Fatalf("late result or lost stop: %+v", current)
	}
}

func TestUnknownDispositionAndConfirmedPriorCompletion(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "source-a", AllowEffectEvidence: true}, limits())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "claim", Kind: "claim", Qualification: tasks.QualificationOf(initial)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "start", Kind: "start", Qualification: tasks.QualificationOf(claimed)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, started.Task, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	obs := tasks.Observation{ChangeID: "unknown", Qualification: tasks.QualificationOf(started), Status: "UNKNOWN"}
	unknown, err := p.Observe(ctx, obs)
	if err != nil || unknown.Task.State != "WAITING" {
		t.Fatalf("unknown: %+v %v", unknown, err)
	}
	obs.ChangeID = "actual-completion"
	obs.Status = "COMPLETED"
	obs.Result = "confirmed prior result"
	obs.Evidence = "fixture:completed-before-cancel"
	obs.CompletedAt = c.now.UnixNano()
	done, err := p.Observe(ctx, obs)
	if err != nil || done.Task.State != "COMPLETED" || done.Task.Control.Progress != "NOT_PREVENTED" {
		t.Fatalf("prior completion: %+v %v", done, err)
	}
	again, err := p.Observe(ctx, obs)
	if err != nil || again.LastCommit != done.LastCommit {
		t.Fatalf("observation replay: %+v %v", again, err)
	}
}
func TestResumePreservesBudgetWait(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	in := submission(t, a, token, c)
	in.Constraints.MaxSteps = 1
	task, err := s.Submit(ctx, token, in)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := tasks.NewRunner(workerPort(t, s, token, "worker"), tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
		return tasks.Proposal{}, &authorization.Error{Code: authorization.Unavailable}
	}), limits(), c)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := runner.Run(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, stopped.Task, "PAUSE")); err != nil {
		t.Fatal(err)
	}
	paused, err := s.Get(ctx, token, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, paused, "RUN")); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Get(ctx, token, task.Ref)
	if err != nil || resumed.State != "WAITING" || !slices.Contains(resumed.WaitingReasons, "budget") || slices.Contains(resumed.WaitingReasons, "pause") || resumed.Attempts != 1 {
		t.Fatalf("lost independent wait: %+v %v", resumed, err)
	}
}

func TestRepeatedCancelDoesNotMoveCompletionCutoff(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "source", AllowEffectEvidence: true}, limits())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "claim", Kind: "claim", Qualification: tasks.QualificationOf(candidate)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "start", Kind: "start", Qualification: tasks.QualificationOf(claimed)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, started.Task, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	first, err := s.Get(ctx, token, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(time.Second)
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, first, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	_, err = p.Observe(ctx, tasks.Observation{ChangeID: "after-first-cancel", Qualification: tasks.QualificationOf(started), Status: "COMPLETED", Result: "too late", Evidence: "fixture", CompletedAt: c.now.Add(-500 * time.Millisecond).UnixNano()})
	if !authorization.Is(err, authorization.Denied) {
		t.Fatalf("second cancel moved evidence cutoff: %v", err)
	}
}

type armDispositionSource struct{ arm func() }

func (s armDispositionSource) RequestStop(context.Context, tasks.Work) error { return nil }
func (s armDispositionSource) Inspect(context.Context, tasks.Work) (tasks.Disposition, error) {
	s.arm()
	return tasks.Disposition{Status: "STOPPED"}, nil
}

type lostObservationStore struct {
	authorization.Store
	armed bool
}

func (s *lostObservationStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	err := s.Store.Commit(ctx, v, state)
	if err == nil && s.armed {
		s.armed = false
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return err
}
func TestDispositionWorkflowPreservesUnknownObservationIdentity(t *testing.T) {
	s, a, token, c, path := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	p := workerPort(t, s, token, "worker")
	claimed, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "claim", Kind: "claim", Qualification: tasks.QualificationOf(initial)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "start", Kind: "start", Qualification: tasks.QualificationOf(claimed)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, token, controlRequest(t, a, token, started.Task, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fault := &lostObservationStore{Store: db}
	auth, err := authorization.New(fault, c, authConfig())
	if err != nil {
		t.Fatal(err)
	}
	service, err := tasks.New(auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	port := workerPort(t, service, token, "worker")
	_, err = tasks.ReconcileDisposition(ctx, port, armDispositionSource{arm: func() { fault.armed = true }}, task.Ref, "reserve", controlLimits())
	var pending *tasks.PendingObservation
	if !errors.As(err, &pending) {
		t.Fatalf("unknown observation lost its identity: %v", err)
	}
	recovered, err := port.Observe(ctx, pending.Observation)
	if err != nil || recovered.Task.State != "CANCELLED" {
		t.Fatalf("cannot recover observation: %+v %v", recovered, err)
	}
}
