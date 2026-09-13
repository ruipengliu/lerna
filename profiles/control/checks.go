package control

import (
	"context"
	_ "embed"
	"fmt"
	"lerna/adapters/tasklocal"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"os"
	"slices"
	"sync"
	"time"
)

//go:embed testdata/04-runtime.gob
var legacyRuntime []byte

var checks = []string{"reconcile-without-execute", "cross-subject-control", "authorization-control-race", "disposition-timeout", "legacy-upgrade", "unknown-control-reply", "unknown-observation-reply", "script-not-effect", "sdk-control", "permission-boundary", "version-and-identity", "cross-domain-identity", "window-and-receipt-retention", "terminal-controls", "control-before-claim", "control-before-complete", "cooperative-cancel", "uncooperative-pause", "resume-keeps-waits", "unknown-effects", "prior-completion", "source-fencing", "finite-disposition-budget", "control-capacity", "storage-unavailable", "pending-discovery-and-resume"}

func check(ctx context.Context, name string) error {
	dir, err := os.MkdirTemp("", "lerna-control-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	defer h.db.Close()
	s, err := h.submit(ctx, 3)
	if err != nil {
		return err
	}
	controls, err := h.controls()
	if err != nil {
		return err
	}
	switch name {
	case "reconcile-without-execute":
		p, started, err := h.start(ctx, s, false)
		if err != nil {
			return err
		}
		if err = h.policy(ctx, false); err != nil {
			return err
		}
		if _, err = controls.PrepareDisposition(ctx, h.token, s.Task.Ref); err != nil {
			return err
		}
		stopped, err := p.Observe(ctx, tasks.ScriptObservation(started, "STOPPED"))
		if err != nil {
			return err
		}
		return require(!stopped.Work[0].InFlight && stopped.Task.State == "WAITING" && slices.Contains(stopped.Task.WaitingReasons, "authorization") && stopped.Task.Attempts == 1, "reconciliation required target execution authority")

	case "cross-subject-control":
		in, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		if _, err = controls.Request(ctx, h.token, in); err != nil {
			return err
		}
		other, err := authorization.NewCredential()
		if err != nil {
			return err
		}
		now, _ := h.clock.Now()
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "other", CredentialSha256: authorization.CredentialDigest(other), ExpiresUnix: now.Add(time.Hour).Unix()}}}); err != nil {
			return err
		}
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "other", Subject: "other", Scope: scope(now, true), Mode: "continuous"}}}); err != nil {
			return err
		}
		id, err := h.auth.NewOperation(ctx, other)
		if err != nil {
			return err
		}
		_, err = controls.Request(ctx, other, tasks.ControlRequest{OperationID: id, Ref: s.Task.Ref, Intent: "CANCEL", ExpectedVersion: 2})
		if err = expect(err, authorization.Denied); err != nil {
			return err
		}
		_, err = controls.Lookup(ctx, other, "local", in.OperationID)
		if err = expect(err, authorization.Denied); err != nil {
			return err
		}
		_, err = controls.Request(ctx, "operator", in)
		return expect(err, authorization.Unauthenticated)
	case "authorization-control-race":
		return authorizationRace(ctx, h, controls, s)
	case "disposition-timeout":
		p, started, err := h.start(ctx, s, false)
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, started.Task, "CANCEL"); err != nil {
			return err
		}
		result, err := tasks.ReconcileDisposition(ctx, p, timeoutSource{}, s.Task.Ref, "timed-out-source", controlLimits())
		if err != nil {
			return err
		}
		return require(result.Task.State == "WAITING" && result.Task.Control.Progress == "ACCEPTED" && result.DispositionChecks == 1, "disposition timeout fabricated stop")

	case "legacy-upgrade":
		if err = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { tx.SetData(legacyRuntime); return nil }); err != nil {
			return err
		}
		waiting, err := h.service.Get(ctx, h.token, tasks.Ref{Namespace: "local", TaskID: "v04-waiting"})
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, waiting, "PAUSE"); err != nil {
			return err
		}
		paused, err := h.service.Get(ctx, h.token, waiting.Ref)
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, paused, "RUN"); err != nil {
			return err
		}
		got, err := h.service.Get(ctx, h.token, waiting.Ref)
		if err != nil {
			return err
		}
		if err = require(got.State == "WAITING" && got.Attempts == 1 && slices.Contains(got.WaitingReasons, "budget"), "legacy StopReason was cleared"); err != nil {
			return err
		}
		running, err := h.service.Load(ctx, tasks.Ref{Namespace: "local", TaskID: "v04-running"})
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, running.Task, "CANCEL"); err != nil {
			return err
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		reopened, err := other.service.Get(ctx, h.token, running.Task.Ref)
		if err != nil {
			return err
		}
		return require(reopened.Control.Intent == "CANCEL" && reopened.Control.Progress == "ACCEPTED" && slices.Contains(reopened.WaitingReasons, "reconciliation"), "legacy running work falsely stopped during upgrade")
	case "unknown-control-reply", "unknown-observation-reply":
		return unknownCheck(ctx, h, controls, s, name)
	case "script-not-effect":
		p, started, err := h.start(ctx, s, false)
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, started.Task, "CANCEL"); err != nil {
			return err
		}
		now, _ := h.clock.Now()
		_, err = p.Observe(ctx, tasks.Observation{ChangeID: "forged-fact", Qualification: tasks.QualificationOf(started), Status: "COMPLETED", Result: "proposal", Evidence: "unproven", CompletedAt: now.UnixNano()})
		return expect(err, authorization.Denied)

	case "sdk-control":
		binding := tasklocal.BindManaged(h.service, controls, h.token)
		client := sdk.NewTaskClient(binding, "local")
		control := sdk.NewControlClient(binding, "local")
		pause, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		receipt, err := control.Request(ctx, pause)
		if err != nil {
			return err
		}
		got, err := client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		if err = require(got.State == "WAITING" && got.Control.Progress == "APPLIED" && slices.Contains(got.WaitingReasons, "pause"), "pause not applied"); err != nil {
			return err
		}
		resume, err := h.request(ctx, got, "RUN")
		if err != nil {
			return err
		}
		if _, err = control.Request(ctx, resume); err != nil {
			return err
		}
		got, err = client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		if err = require(got.State == "QUEUED" && got.Attempts == 0, "resume modified budget"); err != nil {
			return err
		}
		cancel, err := h.request(ctx, got, "CANCEL")
		if err != nil {
			return err
		}
		if _, err = control.Request(ctx, cancel); err != nil {
			return err
		}
		got, err = client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		if err = require(got.State == "CANCELLED" && got.Control.Progress == "APPLIED", "cancel not applied"); err != nil {
			return err
		}
		old, err := control.Lookup(ctx, pause.OperationID)
		if err != nil {
			return err
		}
		return require(old == receipt, "later control rewrote old receipt")
	case "permission-boundary":
		now, _ := h.clock.Now()
		allowed := scope(now, true)
		allowed.Actions = []string{"task.submit", "task.read", "task.execute"}
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "no-control", Scope: allowed}}}}}); err != nil {
			return err
		}
		for _, intent := range []string{"PAUSE", "CANCEL", "RUN"} {
			in, err := h.request(ctx, s.Task, intent)
			if err != nil {
				return err
			}
			_, err = controls.Request(ctx, h.token, in)
			if err = expect(err, authorization.Denied); err != nil {
				return err
			}
		}
		got, err := h.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.Version == 1 && got.State == "QUEUED", "administrator or execution bypassed control permission")
	case "version-and-identity":
		in, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		receipt, err := controls.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		old, err := controls.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		if old != receipt {
			return fmt.Errorf("duplicate changed receipt")
		}
		for _, field := range []string{"intent", "reason", "version", "target"} {
			different := in
			switch field {
			case "intent":
				different.Intent = "CANCEL"
			case "reason":
				different.Reason = "different"
			case "version":
				different.ExpectedVersion++
			case "target":
				different.Ref.TaskID = "other"
			}
			_, err = controls.Request(ctx, h.token, different)
			if err = expect(err, authorization.IdentityConflict); err != nil {
				return err
			}
		}
		stale, err := h.request(ctx, s.Task, "CANCEL")
		if err != nil {
			return err
		}
		_, err = controls.Request(ctx, h.token, stale)
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		stale.Ref.Namespace = "other"
		_, err = controls.Request(ctx, h.token, stale)
		return expect(err, authorization.Denied)
	case "cross-domain-identity":
		id, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		now, _ := h.clock.Now()
		submission := tasks.Submission{Namespace: "local", OperationID: id, Goal: "another", Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: now.Add(time.Minute).Unix()}}
		if _, err = h.service.Submit(ctx, h.token, submission); err != nil {
			return err
		}
		_, err = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: id, Ref: s.Task.Ref, ExpectedVersion: 1, Intent: "PAUSE"})
		if err = expect(err, authorization.IdentityConflict); err != nil {
			return err
		}
		control, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		if _, err = controls.Request(ctx, h.token, control); err != nil {
			return err
		}
		submission.OperationID = control.OperationID
		_, err = h.service.Submit(ctx, h.token, submission)
		if err = expect(err, authorization.IdentityConflict); err != nil {
			return err
		}
		_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: control.OperationID, Command: &wire.AuthorizationCommand{ExpectedRevision: h.revision, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
		if err = expect(err, authorization.IdentityConflict); err != nil {
			return err
		}
		authID, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: authID, Command: &wire.AuthorizationCommand{ExpectedRevision: h.revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope(now, true)}}}}}})
		if err != nil {
			return err
		}
		_, err = controls.Request(ctx, h.token, tasks.ControlRequest{OperationID: authID, Ref: s.Task.Ref, ExpectedVersion: 2, Intent: "CANCEL"})
		return expect(err, authorization.IdentityConflict)
	case "window-and-receipt-retention":
		in, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		first, err := controls.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		h.clock.advance(3 * time.Minute)
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}); err != nil {
			return err
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		c, err := other.controls()
		if err != nil {
			return err
		}
		again, err := c.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		lookup, err := c.Lookup(ctx, h.token, "local", in.OperationID)
		if err != nil {
			return err
		}
		return require(first == again && lookup == first, "window cleanup revived control")
	case "terminal-controls":
		p, err := h.port("worker-a")
		if err != nil {
			return err
		}
		r, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
		if err != nil {
			return err
		}
		done, err := r.Run(ctx, s)
		if err != nil {
			return err
		}
		for _, intent := range []string{"CANCEL", "RUN", "PAUSE"} {
			receipt, err := h.control(ctx, done.Task, intent)
			if err != nil {
				return err
			}
			if receipt.Outcome != "already_COMPLETED" {
				return fmt.Errorf("terminal updated")
			}
		}
		got, err := h.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.Version == done.Task.Version && got.Result == done.Task.Result, "late control erased actual result")
	case "control-before-claim", "control-before-complete":
		return raceCheck(ctx, h, controls, s, name)
	case "cooperative-cancel", "uncooperative-pause":
		return runnerControlCheck(ctx, h, controls, s, name)
	case "resume-keeps-waits":
		task, err := h.submit(ctx, 1)
		if err != nil {
			return err
		}
		p, err := h.port("worker-a")
		if err != nil {
			return err
		}
		runner, err := tasks.NewRunner(p, tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
			return tasks.Proposal{}, &authorization.Error{Code: authorization.Unavailable}
		}), limits(), h.clock)
		if err != nil {
			return err
		}
		waiting, err := runner.Run(ctx, task)
		if err != nil {
			return err
		}
		if _, err = h.control(ctx, waiting.Task, "PAUSE"); err != nil {
			return err
		}
		paused, err := h.service.Get(ctx, h.token, task.Task.Ref)
		if err != nil {
			return err
		}
		if err = h.policy(ctx, false); err != nil {
			return err
		}
		if _, err = h.control(ctx, paused, "RUN"); err != nil {
			return err
		}
		got, err := h.service.Get(ctx, h.token, task.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.State == "WAITING" && got.Attempts == 1 && slices.Contains(got.WaitingReasons, "budget") && slices.Contains(got.WaitingReasons, "authorization") && !slices.Contains(got.WaitingReasons, "pause"), "resume cleared unrelated waits")
	case "unknown-effects", "prior-completion", "source-fencing", "finite-disposition-budget", "pending-discovery-and-resume":
		return dispositionCheck(ctx, h, controls, s, name)
	case "control-capacity":
		cfg := controlLimits()
		cfg.MaxOperations = 1
		c, err := h.service.Controls(cfg)
		if err != nil {
			return err
		}
		in, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		first, err := c.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		again, err := c.Request(ctx, h.token, in)
		if err != nil || again != first {
			return fmt.Errorf("capacity rejected original receipt: %v", err)
		}
		got, err := h.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		next, err := h.request(ctx, got, "RUN")
		if err != nil {
			return err
		}
		_, err = c.Request(ctx, h.token, next)
		return expect(err, authorization.Unavailable)
	case "storage-unavailable":
		in, err := h.request(ctx, s.Task, "CANCEL")
		if err != nil {
			return err
		}
		h.db.Close()
		if _, err = controls.Request(ctx, h.token, in); err == nil {
			return fmt.Errorf("closed storage accepted control")
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		got, err := other.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.State == "QUEUED" && got.Version == 1, "storage failure invented control")
	}
	return fmt.Errorf("unknown check %s", name)
}

type fixtureSource struct {
	mu             sync.Mutex
	status, result string
	completedAt    int64
	requests       int
}

func (f *fixtureSource) RequestStop(ctx context.Context, w tasks.Work) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	return nil
}
func (f *fixtureSource) Inspect(ctx context.Context, w tasks.Work) (tasks.Disposition, error) {
	if err := ctx.Err(); err != nil {
		return tasks.Disposition{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return tasks.Disposition{Status: f.status, Result: f.result, CompletedAt: f.completedAt, Evidence: "controlled-source"}, nil
}
func dispositionCheck(ctx context.Context, h *harness, c *tasks.ControlService, s tasks.RunSnapshot, name string) error {
	p, started, err := h.start(ctx, s, true)
	if err != nil {
		return err
	}
	intent := "CANCEL"
	if name == "pending-discovery-and-resume" {
		intent = "PAUSE"
	}
	if _, err = h.control(ctx, started.Task, intent); err != nil {
		return err
	}
	source := &fixtureSource{status: "UNKNOWN"}
	if name == "prior-completion" {
		now, _ := h.clock.Now()
		source.status = "COMPLETED"
		source.result = "confirmed result"
		source.completedAt = now.UnixNano()
	}
	if name == "source-fencing" {
		base := tasks.Observation{ChangeID: "fact", Qualification: tasks.QualificationOf(started), Status: "STOPPED"}
		wrong, err := h.port("other-source")
		if err != nil {
			return err
		}
		_, err = wrong.Observe(ctx, base)
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		base.Qualification.Generation++
		_, err = p.Observe(ctx, base)
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		base.Qualification = tasks.QualificationOf(started)
		base.Status = "COMPLETED"
		base.Result = "unproven"
		base.Evidence = "fixture"
		now, _ := h.clock.Now()
		base.CompletedAt = now.Add(time.Second).UnixNano()
		_, err = p.Observe(ctx, base)
		return expect(err, authorization.Denied)
	}
	first, err := tasks.ReconcileDisposition(ctx, p, source, s.Task.Ref, "check-1", controlLimits())
	if err != nil {
		return err
	}
	if name == "prior-completion" {
		return require(first.Task.State == "COMPLETED" && first.Task.Result == "confirmed result" && first.Task.Control.Progress == "NOT_PREVENTED", "confirmed prior result discarded")
	}
	if err = require(first.Task.State == "WAITING" && slices.Contains(first.Task.WaitingReasons, "reconciliation") && first.Task.Control.Progress != "APPLIED", "unknown effect manufactured terminal"); err != nil {
		return err
	}
	if name == "finite-disposition-budget" {
		if _, err = tasks.ReconcileDisposition(ctx, p, source, s.Task.Ref, "check-1", controlLimits()); err != nil {
			return err
		}
		if source.requests != 1 {
			return fmt.Errorf("replayed reservation re-executed disposition")
		}
		for i := 2; i <= 4; i++ {
			if _, err = tasks.ReconcileDisposition(ctx, p, source, s.Task.Ref, fmt.Sprintf("check-%d", i), controlLimits()); err != nil {
				return err
			}
		}
		_, err = tasks.ReconcileDisposition(ctx, p, source, s.Task.Ref, "check-5", controlLimits())
		if err = expect(err, authorization.Unavailable); err != nil {
			return err
		}
		got, err := h.service.Load(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.Task.State == "WAITING" && got.Task.Attempts == 1 && got.DispositionChecks == 4, "disposition budget became target budget or fake terminal")
	}
	if name == "pending-discovery-and-resume" {
		if _, err = h.control(ctx, first.Task, "RUN"); err != nil {
			return err
		}
		pending, err := c.ListPending(ctx, h.token, tasks.RecoveryQuery{Namespace: "local", Limit: 1})
		if err != nil {
			return err
		}
		if len(pending.Runs) != 1 {
			return fmt.Errorf("lost resumed pending work")
		}
	}
	source.status = "STOPPED"
	done, err := tasks.ReconcileDisposition(ctx, p, source, s.Task.Ref, "check-2", controlLimits())
	if err != nil {
		return err
	}
	expected := "CANCELLED"
	if name == "pending-discovery-and-resume" {
		expected = "QUEUED"
	}
	return require(done.Task.State == expected && done.Task.Control.Progress == "APPLIED" && done.Task.Attempts == 1, "actual stop not applied")
}

type timeoutSource struct{}

func (timeoutSource) RequestStop(ctx context.Context, _ tasks.Work) error {
	<-ctx.Done()
	return ctx.Err()
}
func (timeoutSource) Inspect(context.Context, tasks.Work) (tasks.Disposition, error) {
	return tasks.Disposition{Status: "UNKNOWN"}, nil
}
