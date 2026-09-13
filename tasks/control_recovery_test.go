package tasks_test

import (
	"context"
	"lerna/tasks"
	"slices"
	"testing"
	"time"
)

func TestTimeoutActualReturnAllowsRetry(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := s.Load(ctx, task.Ref)
	cfg := limits()
	cfg.DecisionTimeout = 20 * time.Millisecond
	p, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "worker"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	runner, _ := tasks.NewRunner(p, tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
		<-release
		return tasks.Proposal{}, nil
	}), cfg, c)
	stopped, err := runner.Run(ctx, initial)
	close(release)
	if err != nil || !stopped.Work[0].InFlight {
		t.Fatalf("timeout: %+v %v", stopped, err)
	}
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		stopped, err = s.Load(ctx, task.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if !stopped.Work[0].InFlight {
			break
		}
		time.Sleep(time.Millisecond)
	}
	next, _ := tasks.NewRunner(p, tasks.BrainFunc(func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		return tasks.Proposal{Kind: "result", Complete: true, BaseVersion: in.Task.Version, Result: "retry"}, nil
	}), cfg, c)
	done, err := next.Run(ctx, stopped)
	if err != nil || done.Task.State != "COMPLETED" || done.Task.Attempts != 2 {
		t.Fatalf("retry blocked: %+v %v", done, err)
	}
}

func TestRecoveryWithoutUserControlAndDeadlineEvidence(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "restart", true: "deadline"}[deadline], func(t *testing.T) {
			s, a, token, c, _ := setup(t)
			enableControls(t, a, token, c)
			ctx := context.Background()
			task, err := s.Submit(ctx, token, submission(t, a, token, c))
			if err != nil {
				t.Fatal(err)
			}
			initial, _ := s.Load(ctx, task.Ref)
			p, err := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "source", AllowEffectEvidence: true}, limits())
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
			if deadline {
				c.now = time.Unix(task.Constraints.DeadlineUnix, 0)
				stopped, e := p.Commit(ctx, tasks.WorkChange{ChangeID: "renew", Kind: "renew", Qualification: tasks.QualificationOf(started)})
				if e != nil || stopped.Task.State != "WAITING" || !slices.Contains(stopped.Task.WaitingReasons, "reconciliation") || !slices.Contains(stopped.Task.WaitingReasons, "deadline") {
					t.Fatalf("invented failure: %+v %v", stopped, e)
				}
			}
			controls, _ := s.Controls(controlLimits())
			page, err := controls.ListPending(ctx, token, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
			if err != nil || len(page.Runs) != 1 {
				t.Fatalf("undiscoverable: %+v %v", page, err)
			}
			if _, err = controls.PrepareDisposition(ctx, token, task.Ref); err != nil {
				t.Fatal(err)
			}
			obs := tasks.ScriptObservation(started, "STOPPED")
			if deadline {
				obs.Status = "COMPLETED"
				obs.Result = "actual"
				obs.Evidence = "fixture"
				obs.CompletedAt = c.now.Add(-time.Second).UnixNano()
			}
			done, err := p.Observe(ctx, obs)
			if err != nil {
				t.Fatal(err)
			}
			if deadline {
				if done.Task.State != "COMPLETED" {
					t.Fatalf("lost fact: %+v", done)
				}
			} else {
				next, err := p.Commit(ctx, tasks.WorkChange{ChangeID: "retry", Kind: "claim", Qualification: tasks.QualificationOf(done)})
				if err != nil || next.Task.Attempts != 2 {
					t.Fatalf("recovery: %+v %v", next, err)
				}
			}
			if !tasks.ValidSnapshot(done.Task) {
				t.Fatalf("invalid public snapshot: %+v", done.Task)
			}
		})
	}
}

func TestCompletionFirstReturnsReceiptForStaleCancel(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableControls(t, a, token, c)
	ctx := context.Background()
	task, err := s.Submit(ctx, token, submission(t, a, token, c))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	request := controlRequest(t, a, token, task, "CANCEL")
	runner, err := tasks.NewRunner(workerPort(t, s, token, "worker"), tasks.BrainFunc(func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "actual"}, nil
	}), limits(), c)
	if err != nil {
		t.Fatal(err)
	}
	done, err := runner.Run(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	controls, _ := s.Controls(controlLimits())
	receipt, err := controls.Request(ctx, token, request)
	if err != nil || receipt.Outcome != "already_COMPLETED" || receipt.Version != done.Task.Version {
		t.Fatalf("late cancel: %+v %v", receipt, err)
	}
	current, err := s.Get(ctx, token, task.Ref)
	if err != nil || current.Result != "actual" || current.Version != done.Task.Version {
		t.Fatalf("terminal changed: %+v %v", current, err)
	}
}
