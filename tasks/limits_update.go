package tasks

import (
	"context"
	"lerna/authorization"
	"math"
)

// Presence is explicit in both gob and wire encodings, including zero values.
type LimitValue struct {
	Present bool
	Value   uint64
}
type LimitPatch struct{ Steps, Requests, Tokens, Deadline LimitValue }
type AdjustRequest struct {
	OperationID     string
	Ref             Ref
	ExpectedVersion uint64
	Patch           LimitPatch
}

func (p LimitPatch) valid() bool {
	count := 0
	for _, v := range []LimitValue{p.Steps, p.Requests, p.Tokens, p.Deadline} {
		if v.Present {
			count++
		} else if v.Value != 0 {
			return false
		}
	}
	return count > 0 && p.Steps.Value <= 10000 && p.Requests.Value <= 64 && p.Tokens.Value <= 1048576 && p.Deadline.Value <= math.MaxInt64
}
func (u *UpdateService) AdjustLimits(ctx context.Context, token string, in AdjustRequest) (InputReceipt, error) {
	return u.apply(ctx, token, inputChange{Adjust: in})
}
func adjusted(t Task, p LimitPatch) (Constraints, error) {
	out := t.Constraints
	if p.Steps.Present {
		out.MaxSteps = uint32(p.Steps.Value)
	}
	if p.Requests.Present {
		out.ModelRequests = uint32(p.Requests.Value)
	}
	if p.Tokens.Present {
		out.ModelTokens = p.Tokens.Value
	}
	if p.Deadline.Present {
		out.DeadlineUnix = int64(p.Deadline.Value)
	}
	// This slice does not convert an existing scripted task into a model task or
	// erase a model ledger by disabling its budget mode.
	if (out.ModelRequests == 0) != (t.Constraints.ModelRequests == 0) || (out.ModelRequests == 0) != (out.ModelTokens == 0) || out.MaxSteps == 0 || out.DeadlineUnix <= 0 || out.MaxSteps < t.Attempts || out.ModelRequests < t.ModelUsedRequests+t.ModelReservedRequests || out.ModelTokens < t.ModelUsedTokens+t.ModelReservedTokens {
		return Constraints{}, failure(authorization.Invalid)
	}
	return out, nil
}
func applyLimits(r *RunSnapshot, c inputChange) error {
	if c.Adjust.OperationID == "" {
		return nil
	}
	out, e := adjusted(r.Task, c.Adjust.Patch)
	if e != nil {
		return e
	}
	r.Task.Constraints = out
	if r.Task.Attempts < out.MaxSteps {
		removeWait(&r.Task, "budget")
	}
	if out.ModelRequests > r.Task.ModelUsedRequests+r.Task.ModelReservedRequests && out.ModelTokens > r.Task.ModelUsedTokens+r.Task.ModelReservedTokens {
		removeWait(&r.Task, "GENERATION_BUDGET_EXCEEDED")
	}
	// Deadline wait is recomputed using authoritative time by settleControl after
	// the old generation actually stops, never by altering the old reservation.
	removeWait(&r.Task, "deadline")
	return nil
}
