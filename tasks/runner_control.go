package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"lerna/authorization"
	"time"
)

type decisionAnswer struct {
	proposal Proposal
	err      error
}

func controlChanged(current, claimed RunSnapshot) bool {
	return current.UpdateVersion > claimed.UpdateVersion || (current.Task.Control.OperationID != "" && (current.Task.Control.OperationID != claimed.Task.Control.OperationID || controlIntent(current.Task) != "RUN"))
}

type PendingObservation struct {
	Observation Observation
	Cause       error
}

func (e *PendingObservation) Error() string {
	return fmt.Sprintf("disposition %s requires reconciliation: %v", e.Observation.ChangeID, e.Cause)
}
func (e *PendingObservation) Unwrap() error { return e.Cause }
func (r *Runner) observe(ctx context.Context, store ControlWorkStore, claimed RunSnapshot, status string) (RunSnapshot, error) {
	in := ScriptObservation(claimed, status)
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.limits.IOTimeout)
	defer cancel()
	out, err := store.Observe(bounded, in)
	return observationResult(in, out, err)
}
func observationResult(in Observation, out RunSnapshot, err error) (RunSnapshot, error) {
	if authorization.Is(err, authorization.OutcomeUnknown) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return RunSnapshot{}, &PendingObservation{in, err}
	}
	return out, err
}
func (r *Runner) handleControl(ctx context.Context, store ControlWorkStore, claimed, current RunSnapshot, answers <-chan decisionAnswer, cancel context.CancelFunc, finished bool) (RunSnapshot, error) {
	cancel()
	if terminal(current.Task) {
		return current, nil
	}
	if !current.ControlLimits.valid() {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	if finished {
		return r.observe(ctx, store, claimed, "STOPPED")
	}
	timer := time.NewTimer(current.ControlLimits.StopTimeout)
	defer timer.Stop()
	select {
	case <-answers:
		return r.observe(ctx, store, claimed, "STOPPED")
	case <-timer.C:
		out, err := r.observe(ctx, store, claimed, "UNKNOWN")
		if err != nil {
			return RunSnapshot{}, err
		}

		return out, nil
	}
}
func (r *Runner) controlConflict(ctx context.Context, claimed RunSnapshot, cause error, answers <-chan decisionAnswer, cancel context.CancelFunc, finished bool) (RunSnapshot, error) {
	if !authorization.Is(cause, authorization.Conflict) {
		return RunSnapshot{}, cause
	}
	store, ok := r.store.(ControlWorkStore)
	if !ok {
		return RunSnapshot{}, cause
	}
	bounded, stop := context.WithTimeout(context.WithoutCancel(ctx), r.limits.IOTimeout)
	defer stop()
	current, err := store.PollControl(bounded, claimed.Task.Ref)
	if err != nil {
		return RunSnapshot{}, err
	}
	if controlChanged(current, claimed) {
		return r.handleControl(ctx, store, claimed, current, answers, cancel, finished)
	}
	return RunSnapshot{}, cause
}
func (r *Runner) finishDecision(ctx context.Context, claimed RunSnapshot, reason string, answers <-chan decisionAnswer, cancel context.CancelFunc, finished bool) (RunSnapshot, error) {
	out, err := r.stop(ctx, claimed, reason, finished)
	if err != nil {
		return r.controlConflict(ctx, claimed, err, answers, cancel, finished)
	}
	return out, nil
}

// ScriptObservation has a reproducible per-attempt identity, so a late observer
// whose acknowledgement is lost remains queryable after the runner exits.
// Constructing this value does not attest that the call actually stopped.
func ScriptObservation(claimed RunSnapshot, status string) Observation {
	q := observationQualification(claimed)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%q/%q/%q/%d/%d/%d/%s", q.Ref.Namespace, q.WorkID, q.Owner, q.Epoch, q.Generation, q.Version, status)))
	return Observation{ChangeID: fmt.Sprintf("%x", digest), Qualification: q, Status: status}
}
