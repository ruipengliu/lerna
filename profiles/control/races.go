package control

import (
	"context"
	"fmt"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"sync"
	"sync/atomic"
	"time"
)

type gateStore struct {
	authorization.Store
	armed          atomic.Bool
	ready, release chan struct{}
}

func (g *gateStore) Commit(ctx context.Context, v uint64, s authorization.State) error {
	if g.armed.Swap(false) {
		close(g.ready)
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return g.Store.Commit(ctx, v, s)
}
func raceCheck(ctx context.Context, h *harness, c *tasks.ControlService, s tasks.RunSnapshot, name string) error {
	gate := &gateStore{Store: h.db, ready: make(chan struct{}), release: make(chan struct{})}
	controlled, err := assemble(gate, h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	p, err := controlled.port("worker-a")
	if err != nil {
		return err
	}
	change := mutation("race-claim", "claim", s)
	if name == "control-before-complete" {
		_, started, err := h.start(ctx, s, false)
		if err != nil {
			return err
		}
		s = started
		change = mutation("race-complete", "complete", s)
		change.Proposal = proposal(s)
	}
	other, err := open(h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	defer other.db.Close()
	other.revision = h.revision
	request, err := other.request(ctx, s.Task, "CANCEL")
	if err != nil {
		return err
	}
	otherControl, err := other.controls()
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	gate.armed.Store(true)
	go func() { _, err := p.Commit(bounded, change); done <- err }()
	select {
	case <-gate.ready:
	case <-bounded.Done():
		return bounded.Err()
	}
	_, err = otherControl.Request(ctx, h.token, request)
	close(gate.release)
	if err != nil {
		return err
	}
	if err = expect(<-done, authorization.Conflict); err != nil {
		return err
	}
	got, err := h.service.Get(ctx, h.token, s.Task.Ref)
	if err != nil {
		return err
	}
	if name == "control-before-claim" {
		return require(got.State == "CANCELLED" && got.Attempts == 0, "control lost claim race")
	}
	return require(got.State == "WAITING" && got.Result == "" && got.Control.Intent == "CANCEL", "control lost completion race")
}
func runnerControlCheck(ctx context.Context, h *harness, c *tasks.ControlService, s tasks.RunSnapshot, name string) error {
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	brain := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		close(entered)
		defer close(exited)
		if name == "cooperative-cancel" {
			<-ctx.Done()
			return tasks.Proposal{}, ctx.Err()
		}
		<-release
		return script(ctx, in)
	})
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	runner, err := tasks.NewRunner(p, brain, limits(), h.clock)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, s); done <- err }()
	var once sync.Once
	closeRelease := func() { once.Do(func() { close(release) }) }
	defer closeRelease()
	select {
	case <-entered:
	case <-time.After(time.Second):
		return fmt.Errorf("decision did not start")
	}
	current, err := h.service.Get(ctx, h.token, s.Task.Ref)
	if err != nil {
		return err
	}
	intent := "CANCEL"
	if name == "uncooperative-pause" {
		intent = "PAUSE"
	}
	if _, err = h.control(ctx, current, intent); err != nil {
		return err
	}
	select {
	case err = <-done:
		if err != nil {
			return err
		}
	case <-time.After(2 * time.Second):
		return fmt.Errorf("unbounded control")
	}
	current, err = h.service.Get(ctx, h.token, s.Task.Ref)
	if err != nil {
		return err
	}
	if name == "cooperative-cancel" {
		return require(current.State == "CANCELLED" && current.Control.Progress == "APPLIED", "cooperative call not stopped")
	}
	if err = require(current.State == "WAITING" && current.Control.Progress == "ACCEPTED" && current.Result == "", "uncooperative call falsely stopped"); err != nil {
		return err
	}
	closeRelease()
	<-exited
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		current, err = h.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		if current.Control.Progress == "APPLIED" {
			return require(current.Result == "" && current.State == "WAITING", "late proposal was published")
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("late stop was not recorded")
}

type lostStore struct {
	authorization.Store
	armed atomic.Bool
}

func (s *lostStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	armed := s.armed.Swap(false)
	err := s.Store.Commit(ctx, v, state)
	if err == nil && armed {
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return err
}
func unknownCheck(ctx context.Context, h *harness, c *tasks.ControlService, s tasks.RunSnapshot, name string) error {
	lost := &lostStore{Store: h.db}
	bound, err := assemble(lost, h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	if name == "unknown-control-reply" {
		controls, err := bound.controls()
		if err != nil {
			return err
		}
		in, err := h.request(ctx, s.Task, "PAUSE")
		if err != nil {
			return err
		}
		lost.armed.Store(true)
		_, err = controls.Request(ctx, h.token, in)
		if err = expect(err, authorization.OutcomeUnknown); err != nil {
			return err
		}
		receipt, err := controls.Lookup(ctx, h.token, "local", in.OperationID)
		if err != nil {
			return err
		}
		again, err := controls.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
		got, err := h.service.Get(ctx, h.token, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(receipt == again && got.Version == 2 && got.Control.Progress == "APPLIED", "unknown control duplicated transition")
	}
	_, started, err := h.start(ctx, s, false)
	if err != nil {
		return err
	}
	if _, err = h.control(ctx, started.Task, "CANCEL"); err != nil {
		return err
	}
	p, err := bound.port("worker-a")
	if err != nil {
		return err
	}
	in := tasks.Observation{ChangeID: "stopped", Qualification: tasks.QualificationOf(started), Status: "STOPPED"}
	lost.armed.Store(true)
	_, err = p.Observe(ctx, in)
	if err = expect(err, authorization.OutcomeUnknown); err != nil {
		return err
	}
	receipt, err := h.service.LookupCommit(ctx, s.Task.Ref, in.ChangeID)
	if err != nil {
		return err
	}
	again, err := p.Observe(ctx, in)
	if err != nil {
		return err
	}
	return require(again.LastCommit == receipt && again.Task.State == "CANCELLED", "unknown disposition reopened work")
}

func authorizationRace(ctx context.Context, h *harness, c *tasks.ControlService, s tasks.RunSnapshot) error {
	gate := &gateStore{Store: h.db, ready: make(chan struct{}), release: make(chan struct{})}
	controlled, err := assemble(gate, h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	controls, err := controlled.controls()
	if err != nil {
		return err
	}
	request, err := h.request(ctx, s.Task, "PAUSE")
	if err != nil {
		return err
	}
	other, err := open(h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	defer other.db.Close()
	other.revision = h.revision
	gate.armed.Store(true)
	done := make(chan error, 1)
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go func() { _, err := controls.Request(bounded, h.token, request); done <- err }()
	select {
	case <-gate.ready:
	case <-bounded.Done():
		return bounded.Err()
	}
	now, _ := h.clock.Now()
	policy := scope(now, true)
	policy.Actions = []string{"task.read", "task.submit", "task.execute"}
	err = other.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "no-control", Scope: policy}}}}})
	close(gate.release)
	if err != nil {
		return err
	}
	if err = expect(<-done, authorization.Denied); err != nil {
		return err
	}
	got, err := h.service.Get(ctx, h.token, s.Task.Ref)
	if err != nil {
		return err
	}
	return require(got.Version == 1 && got.Control.OperationID == "", "stale authorization accepted control")
}
