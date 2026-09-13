package tasks

import (
	"context"
	"errors"
	"fmt"
	"lerna/authorization"
	"lerna/internal/randomid"
	"time"
)

type DecisionInput struct {
	Generation     GenerationReservation
	Task           Task
	Work           Work
	RemainingSteps uint32
}
type Brain interface {
	Decide(context.Context, DecisionInput) (Proposal, error)
}
type BrainFunc func(context.Context, DecisionInput) (Proposal, error)

func (f BrainFunc) Decide(ctx context.Context, in DecisionInput) (Proposal, error) { return f(ctx, in) }

type WorkStore interface {
	Commit(context.Context, WorkChange) (RunSnapshot, error)
	Lookup(context.Context, Ref, string) (RunSnapshot, error)
}

// PendingCommit preserves the exact request when a bounded reconciliation could
// not determine its outcome. Never replace it with a new change ID or a failure.
type PendingCommit struct {
	Change WorkChange
	Cause  error
}

func (e *PendingCommit) Error() string {
	return fmt.Sprintf("commit %s requires reconciliation: %v", e.Change.ChangeID, e.Cause)
}
func (e *PendingCommit) Unwrap() error { return e.Cause }

type Runner struct {
	store  WorkStore
	brain  Brain
	limits RunLimits
	slots  chan struct{}
	clock  authorization.Clock
}

func NewRunner(store WorkStore, brain Brain, c RunLimits, clock authorization.Clock) (*Runner, error) {
	if store == nil || brain == nil || clock == nil || !c.valid() {
		return nil, failure(authorization.Invalid)
	}
	return &Runner{store: store, brain: brain, limits: c, slots: make(chan struct{}, c.MaxConcurrent), clock: clock}, nil
}

// Reconcile looks up the original commit without executing Brain or claiming
// again. NotFound may race an outstanding commit and remains an unknown outcome.
func (r *Runner) Reconcile(ctx context.Context, c WorkChange) (RunSnapshot, error) {
	bounded, cancel := context.WithTimeout(ctx, r.limits.IOTimeout)
	defer cancel()
	out, err := r.store.Lookup(bounded, c.Qualification.Ref, c.ChangeID)
	if err != nil {
		return RunSnapshot{}, &PendingCommit{c, err}
	}
	return out, nil
}
func (r *Runner) commit(ctx context.Context, c WorkChange) (RunSnapshot, error) {
	bounded, cancel := context.WithTimeout(ctx, r.limits.IOTimeout)
	defer cancel()
	out, err := r.store.Commit(bounded, c)
	if authorization.Is(err, authorization.OutcomeUnknown) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return r.Reconcile(ctx, c)
	}
	return out, err
}
func change(kind string, s RunSnapshot) (WorkChange, error) {
	id, err := randomid.New()
	return WorkChange{ChangeID: id, Kind: kind, Qualification: QualificationOf(s)}, err
}

// Run processes one finite attempt. A subsequent scheduler sweep may select a
// QUEUED retry, but WAITING and terminal tasks need no automatic re-execution.
func (r *Runner) Run(ctx context.Context, candidate RunSnapshot) (result RunSnapshot, runErr error) {
	if err := ctx.Err(); err != nil {
		return RunSnapshot{}, err
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return RunSnapshot{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-r.slots
		}
	}()
	c, err := change("claim", candidate)
	if err != nil {
		return RunSnapshot{}, err
	}
	claimed, err := r.commit(ctx, c)
	if err != nil {
		return RunSnapshot{}, err
	}
	if claimed.Task.State != "RUNNING" {
		return claimed, nil
	}
	// The store freezes limits; a mismatched runner cannot relax them.
	if claimed.Limits != r.limits {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	// Revalidate after a delayed or reconciled claim response before calling Brain.
	c, err = change("start", claimed)
	if err != nil {
		return RunSnapshot{}, err
	}
	claimed, err = r.commit(ctx, c)
	if err != nil {
		return RunSnapshot{}, err
	}
	if claimed.Task.State != "RUNNING" {
		return claimed, nil
	}
	if preparer, ok := r.store.(interface {
		ReserveDecision(context.Context, RunSnapshot) (RunSnapshot, error)
	}); ok {
		bounded, stop := context.WithTimeout(ctx, r.limits.IOTimeout)
		reserved, e := preparer.ReserveDecision(bounded, claimed)
		stop()
		if e != nil {
			if authorization.Is(e, authorization.Conflict) {
				return r.controlConflict(ctx, claimed, e, nil, cancelNoop, true)
			}
			if domain, ok := e.(interface{ DecisionReason() string }); ok && decisionReason(domain.DecisionReason()) {
				return r.finishDecision(ctx, claimed, domain.DecisionReason(), nil, cancelNoop, true)
			}
			if authorization.Is(e, authorization.Unavailable) {
				return r.finishDecision(ctx, claimed, "unavailable", nil, cancelNoop, true)
			}
			return RunSnapshot{}, e
		}
		claimed = reserved
	} else if claimed.Task.Constraints.ModelRequests > 0 {
		return r.finishDecision(ctx, claimed, "brain_failure", nil, cancelNoop, true)
	}
	now, err := r.clock.Now()
	if err != nil || now.UnixNano() < claimed.CheckedAt {
		return RunSnapshot{}, failure(authorization.TimeUntrusted)
	}
	if now.UnixNano() >= claimed.Work[0].LeaseUntil {
		return RunSnapshot{}, failure(authorization.Conflict)
	}
	timeout := r.limits.DecisionTimeout
	if left := time.Unix(claimed.Task.Constraints.DeadlineUnix, 0).Sub(now); left < timeout {
		timeout = left
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	input := DecisionInput{Task: claimed.Task, Work: claimed.Work[0], RemainingSteps: claimed.Task.Constraints.MaxSteps - claimed.Task.Attempts}
	if len(claimed.Generations) > 0 {
		input.Generation = claimed.Generations[len(claimed.Generations)-1]
	}
	input.Task.InputRefs = append([]string(nil), input.Task.InputRefs...)
	answers := make(chan decisionAnswer, 1)
	returned := make(chan struct{})
	// Every exit (including timeout, lease loss and storage failure) retains
	// a bounded observer of this actual script return. It cannot attest effects.
	if store, ok := r.store.(ControlWorkStore); ok && !claimed.Work[0].EffectAware {
		original := claimed
		defer func() {
			if runErr == nil && len(result.Work) == 1 && !result.Work[0].InFlight {
				return
			}
			go func() { <-returned; _, _ = r.observe(context.WithoutCancel(ctx), store, original, "STOPPED") }()
		}()
	}
	// The slot remains occupied until even a non-cooperative Brain returns. Go
	// cannot kill an in-process goroutine; this prevents unbounded abandoned calls.
	release = false
	go func() {
		defer func() { <-r.slots; close(returned) }()
		result := decisionAnswer{}
		defer func() {
			if recover() != nil {
				result = decisionAnswer{err: failure(authorization.Invalid)}
			}
			answers <- result
		}()
		result.proposal, result.err = r.brain.Decide(callCtx, input)
	}()
	pollInterval := 10 * time.Millisecond
	if claimed.Task.Constraints.ModelRequests > 0 {
		pollInterval = 100 * time.Millisecond
	}
	if claimed.ControlLimits.valid() {
		pollInterval = claimed.ControlLimits.PollInterval
	}
	controlTicker := time.NewTicker(min(r.limits.RenewEvery, pollInterval))
	defer controlTicker.Stop()
	ticker := time.NewTicker(r.limits.RenewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-controlTicker.C:
			if store, ok := r.store.(ControlWorkStore); ok {
				checkCtx, checkCancel := context.WithTimeout(ctx, r.limits.IOTimeout)
				current, e := store.PollControl(checkCtx, claimed.Task.Ref)
				checkCancel()
				if e != nil {
					return RunSnapshot{}, e
				}
				if controlChanged(current, claimed) {
					return r.handleControl(ctx, store, claimed, current, answers, cancel, false)
				}
			}
		case <-callCtx.Done():
			reason := "timeout"
			if ctx.Err() != nil {
				reason = "interrupted"
			}
			return r.finishDecision(ctx, claimed, reason, answers, cancel, false)
		case <-ticker.C:
			c, err = change("renew", claimed)
			if err != nil {
				return RunSnapshot{}, err
			}
			renewed, err := r.commit(callCtx, c)
			if err != nil {
				return r.controlConflict(ctx, claimed, err, answers, cancel, false)
			}
			if renewed.Task.State != "RUNNING" {
				return renewed, nil
			}
			claimed = renewed
		case a := <-answers:
			if callCtx.Err() != nil {
				reason := "timeout"
				if ctx.Err() != nil {
					reason = "interrupted"
				}
				return r.finishDecision(ctx, claimed, reason, answers, cancel, true)
			}
			if a.err != nil {
				reason := "brain_failure"
				if domain, ok := a.err.(interface{ DecisionReason() string }); ok && decisionReason(domain.DecisionReason()) {
					reason = domain.DecisionReason()
				}
				if authorization.Is(a.err, authorization.Unavailable) {
					reason = "unavailable"
				}
				return r.finishDecision(ctx, claimed, reason, answers, cancel, true)
			}
			c, err = change("complete", claimed)
			if err != nil {
				return RunSnapshot{}, err
			}
			c.Proposal = a.proposal
			c.Finished = true
			out, e := r.commit(ctx, c)
			if e != nil {
				return r.controlConflict(ctx, claimed, e, answers, cancel, true)
			}
			return out, nil
		}
	}
}
func (r *Runner) stop(ctx context.Context, s RunSnapshot, reason string, finished bool) (RunSnapshot, error) {
	c, err := change("stop", s)
	if err != nil {
		return RunSnapshot{}, err
	}
	c.Reason = reason
	c.Finished = finished
	// A cancelled caller must not prevent the bounded durable stop attempt.
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.limits.IOTimeout)
	defer cancel()
	return r.commit(bounded, c)
}

func cancelNoop() {}
