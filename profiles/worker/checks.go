package worker

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed testdata/03-runtime.gob
var legacyRuntime []byte

var checks = []string{"v03-storage-upgrade", "authorization-expiry", "deadline-during-decision", "sdk-complete-reopen", "two-workers", "lease-reclaim-and-renew", "version-and-owner-fencing", "invalid-proposals", "budget-and-retry", "deadline", "authorization-boundary", "authorization-commit-race", "clock-untrusted", "storage-unavailable", "unknown-claim", "unknown-completion", "unknown-renewal", "unresolved-commit", "timeout-and-concurrency", "configuration-frozen", "replaceable-scheduling", "temporary-runtime-common-contract"}

func check(ctx context.Context, name string) error {
	if name == "temporary-runtime-common-contract" {
		return memoryCheck(ctx)
	}
	dir, err := os.MkdirTemp("", "lerna-worker-check-")
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
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	switch name {
	case "v03-storage-upgrade":
		if err = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { tx.SetData(legacyRuntime); return nil }); err != nil {
			return err
		}
		old, err := h.service.Load(ctx, tasks.Ref{Namespace: "local", TaskID: "v03-task"})
		if err != nil {
			return err
		}
		if err = require(old.Task.ModelUsedTokens == 0 && old.Task.ModelReservedTokens == 0 && old.Task.ModelUsedRequests == 0 && old.Task.ModelReservedRequests == 0 && old.Task.Constraints.ModelRequests == 0 && old.Task.Constraints.ModelTokens == 0 && len(old.Generations) == 0 && old.Task.Attempts == 0 && old.Work[0].Generation == 0 && old.Limits == (tasks.RunLimits{}), "old fields have nonzero defaults"); err != nil {
			return err
		}
		runner, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
		if err != nil {
			return err
		}
		done, err := runner.Run(ctx, old)
		if err != nil {
			return err
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		got, err := other.client.Get(ctx, old.Task.Ref)
		if err != nil {
			return err
		}
		original, err := other.service.LookupCommit(ctx, old.Task.Ref, "v03-create")
		if err != nil {
			return err
		}
		return require(got.State == "COMPLETED" && done.Work[0].ID == "v03-task/initial" && original.Version == 1, "upgrade lost original task/work/receipt")
	case "authorization-expiry":
		id, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		now, _ := h.clock.Now()
		task, err := h.client.Submit(ctx, tasks.Submission{Namespace: "local", OperationID: id, Goal: "long deadline", Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: now.Add(2 * time.Hour).Unix()}})
		if err != nil {
			return err
		}
		run, err := h.service.Load(ctx, task.Ref)
		if err != nil {
			return err
		}
		h.clock.advance(time.Hour)
		_, err = p.Commit(ctx, mutation("expired-grant", "claim", run))
		if err = expect(err, authorization.Denied); err != nil {
			return err
		}
		got, err := h.service.Load(ctx, task.Ref)
		if err != nil {
			return err
		}
		return require(got.Task.State == "WAITING" && got.Task.StopReason == "authorization" && got.Task.Attempts == 0, "grant expiry confused with task deadline")
	case "deadline-during-decision":
		brain := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
			h.clock.advance(time.Minute)
			return script(ctx, in)
		})
		runner, err := tasks.NewRunner(p, brain, limits(), h.clock)
		if err != nil {
			return err
		}
		got, err := runner.Run(ctx, s)
		if err != nil {
			return err
		}
		return require(got.Task.State == "FAILED" && got.Task.StopReason == "deadline" && got.Task.Result == "", "deadline accepted late output")
	case "sdk-complete-reopen":
		r, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
		if err != nil {
			return err
		}
		done, err := r.Run(ctx, s)
		if err != nil {
			return err
		}
		if err = h.db.Close(); err != nil {
			return err
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		got, err := other.client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		if err = require(got.State == "COMPLETED" && got.Result == "scripted answer" && got.Attempts == 1 && got.Version == 3, "SDK lost formal result"); err != nil {
			return err
		}
		run, err := other.service.Load(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		receipt, err := other.service.LookupCommit(ctx, s.Task.Ref, done.LastCommit.ChangeID)
		if err != nil {
			return err
		}
		if err = require(len(run.Work) == 1 && run.Work[0].Done && run.Work[0].ID == s.Work[0].ID && len(run.Records) == 4 && receipt == run.LastCommit, "partial completion"); err != nil {
			return err
		}
		page, err := other.service.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
		if err != nil {
			return err
		}
		return require(len(page.Runs) == 0, "completed work was scheduled")
	case "two-workers":
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		p2, err := other.port("worker-b")
		if err != nil {
			return err
		}
		type result struct {
			s   tasks.RunSnapshot
			err error
			p   *tasks.WorkPort
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for i, port := range []*tasks.WorkPort{p, p2} {
			go func() {
				<-start
				claimed, err := port.Commit(ctx, mutation(fmt.Sprintf("claim-%d", i), "claim", s))
				results <- result{claimed, err, port}
			}()
		}
		close(start)
		a, b := <-results, <-results
		if a.err != nil {
			a, b = b, a
		}
		if a.err != nil {
			return a.err
		}
		if err = expect(b.err, authorization.Conflict); err != nil {
			return err
		}
		done, err := complete(ctx, a.p, "complete", a.s)
		if err != nil {
			return err
		}
		return require(done.Task.Attempts == 1 && done.Work[0].Generation == 1 && done.Work[0].Done, "race admitted duplicate work")
	case "lease-reclaim-and-renew":
		first, err := p.Commit(ctx, mutation("claim", "claim", s))
		if err != nil {
			return err
		}
		h.clock.advance(200 * time.Millisecond)
		renew := mutation("renew", "renew", first)
		renewed, err := p.Commit(ctx, renew)
		if err != nil {
			return err
		}
		if err = require(renewed.Task.Version == first.Task.Version && renewed.Work[0].Generation == 1 && renewed.Work[0].LeaseUntil > first.Work[0].LeaseUntil, "renew changed decision version or failed to extend lease"); err != nil {
			return err
		}
		replay, err := p.Commit(ctx, renew)
		if err != nil {
			return err
		}
		if err = require(replay.Work[0] == renewed.Work[0], "renew replay changed lease"); err != nil {
			return err
		}
		h.clock.advance(2 * time.Second)
		_, err = p.Commit(ctx, mutation("expired-renew", "renew", renewed))
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		p2, err := other.port("worker-b")
		if err != nil {
			return err
		}
		next, err := p2.Commit(ctx, mutation("reclaim", "claim", renewed))
		if err != nil {
			return err
		}
		if err = require(next.Task.Attempts == 2 && next.Work[0].ID == s.Work[0].ID && next.Work[0].Generation == 2, "reclaim reset budget/work"); err != nil {
			return err
		}
		_, err = complete(ctx, p, "late-complete", first)
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		// Renewal must not invalidate the unchanged proposal baseline.
		renewed, err = p2.Commit(ctx, mutation("renew-b", "renew", next))
		if err != nil {
			return err
		}
		done, err := complete(ctx, p2, "finish", next)
		if err != nil {
			return err
		}
		_, err = p2.Commit(ctx, mutation("late-renew", "renew", renewed))
		if err = expect(err, authorization.Conflict); err != nil {
			return err
		}
		return require(done.Task.State == "COMPLETED" && done.Task.Version == 4, "current proposal failed after renewal")
	case "version-and-owner-fencing":
		first, err := p.Commit(ctx, mutation("claim", "claim", s))
		if err != nil {
			return err
		}
		for _, which := range []string{"version", "epoch", "owner", "generation", "proposal", "malformed-proposal"} {
			c := mutation(which, "complete", first)
			c.Proposal = proposal(first)
			code := authorization.Conflict
			switch which {
			case "version":
				c.Qualification.Version--
			case "epoch":
				c.Qualification.Epoch++
				code = authorization.Denied
			case "owner":
				c.Qualification.Owner = "other"
				code = authorization.Denied
			case "generation":
				c.Qualification.Generation++
			case "proposal":
				c.Proposal.BaseVersion--
			case "malformed-proposal":
				c.Proposal.BaseVersion--
				c.Proposal.Kind = "unknown"
			}
			_, err = p.Commit(ctx, c)
			if err = expect(err, code); err != nil {
				return err
			}
		}
		c := mutation("done", "complete", first)
		c.Proposal = proposal(first)
		done, err := p.Commit(ctx, c)
		if err != nil {
			return err
		}
		again, err := p.Commit(ctx, c)
		if err != nil {
			return err
		}
		if err = require(again.LastCommit == done.LastCommit, "lost completion identity"); err != nil {
			return err
		}
		c.Proposal.Result = "different"
		_, err = p.Commit(ctx, c)
		return expect(err, authorization.IdentityConflict)
	case "invalid-proposals":
		for i, v := range []tasks.Proposal{{Kind: "action", Complete: true, Result: "call API"}, {Kind: "unknown", Complete: true, Result: "x"}, {Kind: "result", Result: "partial"}, {Kind: "result", Complete: true}, {Kind: "result", Complete: true, Result: strings.Repeat("x", 65537)}, {Kind: "result", Complete: true, Result: string([]byte{255})}} {
			current := s
			if i > 0 {
				current, err = h.submit(ctx, 3)
				if err != nil {
					return err
				}
			}
			claimed, err := p.Commit(ctx, mutation("claim", "claim", current))
			if err != nil {
				return err
			}
			v.BaseVersion = claimed.Task.Version
			c := mutation("invalid", "complete", claimed)
			c.Proposal = v
			got, err := p.Commit(ctx, c)
			if err != nil {
				return err
			}
			if err = require(got.Task.State == "FAILED" && got.Task.StopReason == "invalid_proposal" && got.Task.Result == "", "unsupported proposal became success"); err != nil {
				return err
			}
		}
		return nil
	case "budget-and-retry":
		for _, steps := range []uint32{1, 8} {
			current, err := h.submit(ctx, steps)
			if err != nil {
				return err
			}
			r, err := tasks.NewRunner(p, tasks.BrainFunc(func(context.Context, tasks.DecisionInput) (tasks.Proposal, error) {
				return tasks.Proposal{}, &authorization.Error{Code: authorization.Unavailable}
			}), limits(), h.clock)
			if err != nil {
				return err
			}
			for i := 0; i < 4 && current.Task.State == "QUEUED"; i++ {
				current, err = r.Run(ctx, current)
				if err != nil {
					return err
				}
			}
			want, reason := uint32(1), "budget"
			if steps == 8 {
				want = 3
				reason = "retry_limit"
			}
			if err = require(current.Task.State == "WAITING" && current.Task.StopReason == reason && current.Task.Attempts == want, "retry escaped persistent budget"); err != nil {
				return err
			}
		}
		return nil
	case "deadline":
		h.clock.advance(time.Minute)
		got, err := p.Commit(ctx, mutation("expired", "claim", s))
		if err != nil {
			return err
		}
		return require(got.Task.State == "FAILED" && got.Task.StopReason == "deadline" && got.Task.Attempts == 0, "deadline launched decision")
	case "authorization-boundary":
		if err = h.policy(ctx, false); err != nil {
			return err
		}
		_, err = p.Commit(ctx, mutation("denied", "claim", s))
		if err = expect(err, authorization.Denied); err != nil {
			return err
		}
		got, err := h.client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.State == "WAITING" && got.StopReason == "authorization" && got.Attempts == 0, "submit/read or administrator bypassed execution authorization")
	case "authorization-commit-race":
		return authorizationRace(ctx, h, p, s)
	case "clock-untrusted":
		h.clock.mu.Lock()
		h.clock.broken = true
		h.clock.mu.Unlock()
		_, err = p.Commit(ctx, mutation("clock", "claim", s))
		if err = expect(err, authorization.TimeUntrusted); err != nil {
			return err
		}
		h.clock.mu.Lock()
		h.clock.broken = false
		h.clock.mu.Unlock()
		got, err := h.client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.State == "QUEUED" && got.Attempts == 0, "untrusted time admitted work")
	case "storage-unavailable":
		h.db.Close()
		r, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
		if err != nil {
			return err
		}
		_, err = r.Run(ctx, s)
		if err == nil {
			return fmt.Errorf("closed storage reported running")
		}
		other, err := open(dir, h.token, h.clock)
		if err != nil {
			return err
		}
		defer other.db.Close()
		got, err := other.client.Get(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		return require(got.State == "QUEUED" && got.Attempts == 0, "storage failure consumed task")
	case "unknown-claim", "unknown-completion", "unknown-renewal", "unresolved-commit":
		return unknownCheck(ctx, h, s, name)
	case "timeout-and-concurrency":
		return timeoutCheck(ctx, h, s)
	case "configuration-frozen":
		first, err := p.Commit(ctx, mutation("claim", "claim", s))
		if err != nil {
			return err
		}
		cfg := limits()
		cfg.MaxAttempts = 2
		changed, err := h.service.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "worker-a"}, cfg)
		if err != nil {
			return err
		}
		_, err = changed.Commit(ctx, mutation("renew", "renew", first))
		return expect(err, authorization.Invalid)
	case "replaceable-scheduling":
		for _, reverse := range []bool{false, true} {
			if reverse {
				if _, err = h.submit(ctx, 3); err != nil {
					return err
				}
			}
			brain := tasks.BrainFunc(script)
			if reverse {
				brain = func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
					v, _ := script(ctx, in)
					v.Result = "alternate strategy"
					return v, nil
				}
			}
			runner, err := tasks.NewRunner(p, brain, limits(), h.clock)
			if err != nil {
				return err
			}
			wake := make(chan struct{}, 1)
			wake <- struct{}{}
			result, err := runner.Dispatch(ctx, h.service, tasks.SelectorFunc(func(refs []tasks.Ref) ([]tasks.Ref, error) {
				if reverse {
					for i, j := 0, len(refs)-1; i < j; i, j = i+1, j-1 {
						refs[i], refs[j] = refs[j], refs[i]
					}
				}
				return refs, nil
			}), tasks.WakeupFunc(func(ctx context.Context) error {
				select {
				case <-wake:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}), tasks.RecoveryQuery{Namespace: "local", Limit: 10})
			if err != nil {
				return err
			}
			if err = require(len(result.Runs) == 1 && result.Runs[0].Task.State == "COMPLETED", "replacement failed to execute selected page"); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown worker check %s", name)
}

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
func authorizationRace(ctx context.Context, h *harness, p *tasks.WorkPort, s tasks.RunSnapshot) error {
	first, err := p.Commit(ctx, mutation("claim", "claim", s))
	if err != nil {
		return err
	}
	gate := &gateStore{Store: h.db, ready: make(chan struct{}), release: make(chan struct{})}
	controlled, err := assemble(gate, h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	port, err := controlled.port("worker-a")
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
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() { _, err := complete(bounded, port, "finish", first); done <- err }()
	select {
	case <-gate.ready:
	case <-bounded.Done():
		return bounded.Err()
	}
	err = other.policy(ctx, false)
	close(gate.release)
	if err != nil {
		return err
	}
	if err = expect(<-done, authorization.Denied); err != nil {
		return err
	}
	got, err := h.client.Get(ctx, s.Task.Ref)
	if err != nil {
		return err
	}
	return require(got.State == "WAITING" && got.StopReason == "authorization" && got.Result == "", "authorization CAS race published forbidden result")
}

type lostStore struct {
	authorization.Store
	mode atomic.Int32
}

func (s *lostStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	mode := s.mode.Swap(0)
	if mode == 2 {
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	err := s.Store.Commit(ctx, v, state)
	if err == nil && mode == 1 {
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return err
}
func unknownCheck(ctx context.Context, h *harness, s tasks.RunSnapshot, name string) error {
	fault := &lostStore{Store: h.db}
	controlled, err := assemble(fault, h.dir, h.token, h.clock)
	if err != nil {
		return err
	}
	p, err := controlled.port("worker-a")
	if err != nil {
		return err
	}
	brain := tasks.BrainFunc(script)
	if name == "unknown-completion" {
		brain = func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
			fault.mode.Store(1)
			return script(ctx, in)
		}
	}
	r, err := tasks.NewRunner(p, brain, limits(), h.clock)
	if err != nil {
		return err
	}
	if name == "unknown-renewal" {
		claimed, err := p.Commit(ctx, mutation("claim", "claim", s))
		if err != nil {
			return err
		}
		c := mutation("renew", "renew", claimed)
		fault.mode.Store(1)
		_, err = p.Commit(ctx, c)
		if err = expect(err, authorization.OutcomeUnknown); err != nil {
			return err
		}
		recovered, err := r.Reconcile(ctx, c)
		if err != nil {
			return err
		}
		again, err := p.Commit(ctx, c)
		if err != nil {
			return err
		}
		return require(recovered.LastCommit == again.LastCommit && again.Work[0].Generation == 1 && again.Task.Attempts == 1, "renew reconciliation changed qualification")
	}
	if name == "unknown-claim" {
		fault.mode.Store(1)
	}
	if name == "unresolved-commit" {
		fault.mode.Store(2)
	}
	result, err := r.Run(ctx, s)
	if name == "unresolved-commit" {
		var pending *tasks.PendingCommit
		if !errors.As(err, &pending) {
			return fmt.Errorf("unknown commit discarded: %v", err)
		}
		if pending.Change.Kind != "claim" {
			return fmt.Errorf("wrong pending identity")
		}
		current, err := h.service.Load(ctx, s.Task.Ref)
		if err != nil {
			return err
		}
		if err = require(current.Task.Attempts == 0 && current.Task.State == "QUEUED", "unknown claim created speculative work"); err != nil {
			return err
		}
		committed, err := p.Commit(ctx, pending.Change)
		if err != nil {
			return err
		}
		recovered, err := r.Reconcile(ctx, pending.Change)
		if err != nil {
			return err
		}
		return require(recovered.LastCommit == committed.LastCommit && recovered.Task.Attempts == 1, "original commit could not be reconciled")
	}
	if err != nil {
		return err
	}
	return require(result.Task.State == "COMPLETED" && result.Task.Attempts == 1, "unknown reply duplicated decision")
}
func timeoutCheck(ctx context.Context, h *harness, s tasks.RunSnapshot) error {
	cfg := limits()
	cfg.DecisionTimeout = 20 * time.Millisecond
	p, err := h.service.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "worker-a"}, cfg)
	if err != nil {
		return err
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	brain := tasks.BrainFunc(func(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
		close(entered)
		<-release
		defer close(exited)
		return script(ctx, in)
	})
	runner, err := tasks.NewRunner(p, brain, cfg, h.clock)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, s); result <- err }()
	<-entered
	defer func() { close(release); <-exited }()
	select {
	case err = <-result:
	case <-time.After(time.Second):
		return fmt.Errorf("unbounded call")
	}
	if err != nil {
		return err
	}
	current, err := h.service.Load(ctx, s.Task.Ref)
	if err != nil {
		return err
	}
	if err = require(current.Task.State == "QUEUED" && current.Task.Attempts == 1 && current.Task.StopReason == "timeout", "timeout not charged"); err != nil {
		return err
	}
	_, err = runner.Run(ctx, current)
	return expect(err, authorization.Unavailable)
}
