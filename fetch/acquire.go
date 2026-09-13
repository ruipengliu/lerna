package fetch

import (
	"context"
	"errors"
	"time"
)

// Acquirer coordinates a single qualified execution attempt. It is an internal
// execution seam, not task admission: the host must bind the original request,
// subject, input fingerprint, limits and authorization-issued evidence operation.
// It never retries network dispatch after the durable reservation exists.
type Acquirer struct {
	fetcher  Fetcher
	store    OutcomeStore
	evidence Evidence
}

func NewAcquirer(f Fetcher, s OutcomeStore, e Evidence) (*Acquirer, error) {
	if f == nil || s == nil || e == nil {
		return nil, Invalid
	}
	return &Acquirer{f, s, e}, nil
}

// Acquire reports known=false when no terminal acquisition facts are committed.
// An error after reservation does not refund usage or authorize a new attempt.
func (a *Acquirer) Acquire(ctx context.Context, in AttemptIntent, request Request) (Outcome, bool, error) {
	if in.EvidenceOperation == "" || request.MaxRequests != int(in.MaxRequests) {
		return Outcome{}, false, Invalid
	}
	fresh, err := a.store.Begin(ctx, in)
	if err != nil {
		return Outcome{}, false, err
	}
	if !fresh {
		return a.Recover(ctx, in)
	}
	result, err := a.fetcher.Fetch(ctx, request)
	if err != nil {
		status := failureStatus(err)
		if result.Requests < 0 || result.Requests > int(in.MaxRequests) {
			return Outcome{}, false, Unavailable
		}
		outcome := Outcome{Mode: result.Mode, Status: status, Requests: uint32(result.Requests)}
		// Cancellation stops acquisition, not retention of its already observed
		// finite facts. No body, new request or budget refund occurs in this tail.
		record, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if err = a.store.Complete(record, in, outcome); err != nil {
			return Outcome{}, false, err
		}
		return outcome, true, nil
	}
	if !validAcquiredCount(result, in.MaxRequests) {
		return Outcome{}, false, Unavailable
	}
	ref, err := a.evidence.Save(ctx, in.EvidenceOperation, result)
	if err != nil {
		return Outcome{}, false, err
	}
	outcome := Outcome{Mode: result.Mode, Status: "acquired", Requests: uint32(result.Requests), Reference: ref}
	if err = a.store.Complete(ctx, in, outcome); err != nil {
		return Outcome{}, false, err
	}
	return outcome, true, nil
}

// Recover uses original durable identities only. A missing or inaccessible
// Content operation remains unresolved; it cannot be turned into a new Fetch.
func (a *Acquirer) Recover(ctx context.Context, in AttemptIntent) (Outcome, bool, error) {
	original, err := a.store.Inspect(ctx, in.Task.Namespace, in.OperationID)
	if err != nil {
		return Outcome{}, false, err
	}
	if original != in {
		return Outcome{}, false, IdentityConflict
	}
	outcome, known, err := a.store.Outcome(ctx, in.Task.Namespace, in.OperationID)
	if err != nil {
		return Outcome{}, false, err
	}
	if known && outcome.Status != "acquired" {
		return outcome, true, nil
	}
	if in.EvidenceOperation == "" {
		return Outcome{}, false, Unavailable
	}
	ref, err := a.evidence.Lookup(ctx, in.EvidenceOperation)
	if err != nil {
		return Outcome{}, false, err
	}
	result, err := a.evidence.Read(ctx, ref)
	if err != nil {
		return Outcome{}, false, err
	}
	if !validAcquiredCount(result, in.MaxRequests) {
		return Outcome{}, false, Unavailable
	}
	recovered := Outcome{Mode: result.Mode, Status: "acquired", Requests: uint32(result.Requests), Reference: ref}
	if known && outcome != recovered {
		return Outcome{}, false, IdentityConflict
	}
	if !known {
		if err = a.store.Complete(ctx, in, recovered); err != nil {
			return Outcome{}, false, err
		}
	}
	return recovered, true, nil
}
func failureStatus(err error) string {
	for _, v := range []struct {
		err  error
		name string
	}{
		{Invalid, "invalid"}, {Denied, "denied"}, {TooLarge, "too_large"}, {Unsupported, "unsupported"},
		{LimitExceeded, "limit_exceeded"}, {TimedOut, "timed_out"}, {Cancelled, "cancelled"}, {Expired, "expired"},
	} {
		if errors.Is(err, v.err) {
			return v.name
		}
	}
	return "unavailable"
}

func validAcquiredCount(result Result, max uint32) bool {
	if result.Mode == "fixed-replay" {
		return result.Requests == 0
	}
	return (result.Mode == "" || result.Mode == "http") && result.Requests >= 1 && result.Requests <= int(max)
}
