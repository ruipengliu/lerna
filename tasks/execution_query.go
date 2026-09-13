package tasks

import (
	"context"
	"lerna/authorization"
)

// ChargeExecutionQuery charges an observation for an exact dispatched action.
// An expired invocation lease is usable only through the existing explicit
// WithActionRecovery binding; this method never acquires a replacement lease.
func (p *ActionPort) ChargeExecutionQuery(ctx context.Context, in ActionBinding, key string) error {
	if !name(key) {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, err := p.current(j, tx, in.Qualification, false)
		if err != nil {
			return err
		}
		if r.Actions == nil {
			return failure(authorization.Conflict)
		}
		var matched *Action
		for _, action := range r.Actions.Actions {
			if matchesActionBinding(action, in) {
				matched = &action
				break
			}
		}
		if matched == nil {
			return failure(authorization.IdentityConflict)
		}
		if err = p.guardAction(j, tx, &r, in.Qualification, in.OperationID, false); err != nil {
			return err
		}
		if err = appendActionQuery(r.Actions, key); err != nil {
			return err
		}
		j.Runs[in.Qualification.Ref.TaskID] = r
		return nil
	})
}

func appendActionQuery(a *ActionState, key string) error {
	for _, old := range a.Queries {
		if old == key {
			return failure(authorization.Conflict)
		}
	}
	if uint32(len(a.Queries)) >= a.Limits.MaxQueries {
		return generationError("QUERY_BUDGET_EXCEEDED")
	}
	a.Queries = append(a.Queries, key)
	return nil
}

// ChargeExecutionFactQuery permits a control-time observation of an original
// dispatched action's finite failure facts. It does not authorize execution or
// Content access. Consumers must not release a successful result or reference.
func (p *ActionPort) ChargeExecutionFactQuery(ctx context.Context, in ActionBinding, key string) error {
	if !name(key) {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, err := p.current(j, tx, in.Qualification, false)
		if err != nil {
			return err
		}
		intent := controlIntent(r.Task)
		if r.Actions == nil || (intent != "PAUSE" && intent != "CANCEL") || !p.binding.AllowEffectEvidence {
			return failure(authorization.Conflict)
		}
		matched := false
		for _, a := range r.Actions.Actions {
			if matchesActionBinding(a, in) && (a.Status == "DISPATCHED" || a.Status == "DONE") {
				matched = true
				break
			}
		}
		if !matched {
			return failure(authorization.IdentityConflict)
		}
		if err = appendActionQuery(r.Actions, key); err != nil {
			return err
		}
		j.Runs[in.Qualification.Ref.TaskID] = r
		return nil
	})
}
