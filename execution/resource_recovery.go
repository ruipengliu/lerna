package execution

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
	"sort"
)

func (s *Service) AdvanceResourceControl(ctx context.Context, ref ResourceRef) (ResourceControl, error) {
	if s.resource == nil {
		return ResourceControl{}, failure(authorization.Unsupported)
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return ResourceControl{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-s.slots
		}
	}()
	var state controlState
	var key string
	claimed := false
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		claimed = false
		var e error
		key, state, e = s.resourceState(j, ref)
		if e != nil {
			return e
		}
		if state.Scope.Ref != s.resource.scope.Ref {
			return failure(authorization.Unsupported)
		}
		if e = s.authorizeResource(tx, state.Scope, "resource.reconcile"); e != nil {
			return e
		}
		if resourceView(state, j.Shared).Progress == "APPLIED" {
			return nil
		}
		now := tx.Now().UnixNano()
		if now < state.Next || now < state.LeaseUntil {
			return failure(authorization.Conflict)
		}
		if now >= state.Until || state.View.Checks >= uint32(state.Scope.MaxChecks) {
			return failure(authorization.Unavailable)
		}
		state.View.Checks++
		state.Generation++
		state.LeaseUntil = tx.Now().Add(state.Scope.Lease).UnixNano()
		state.Next = tx.Now().Add(state.Scope.PollInterval).UnixNano()
		j.Shared.Scopes[key] = state
		claimed = true
		return nil
	})
	if e != nil {
		return ResourceControl{}, e
	}
	if !claimed {
		return state.View, nil
	}
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	type result struct {
		view ResourceControl
		err  error
	}
	done := make(chan result, 1)
	release = false
	go func() {
		defer func() { <-s.slots; cancel() }()
		view := state.View
		var err error
		defer func() {
			if recover() != nil {
				err = failure(authorization.Unavailable)
			}
			done <- result{view, err}
		}()
		observation, e := s.resource.driver.ObserveResource(bounded, state.Scope.Ref)
		if e != nil {
			err = e
			return
		}
		if !validResourceObservation(state, observation) {
			err = failure(authorization.Denied)
			return
		}
		matches := observation.ControlVersion == state.View.Version && observation.Intent == state.View.Intent
		if !matches && !state.Sent && state.OperationID != "" {
			send := false
			err = s.transaction(bounded, func(j *journal, tx authorization.ExecutionTransaction) error {
				send = false
				current := j.Shared.Scopes[key]
				if current.View.Version != state.View.Version || current.Generation != state.Generation {
					return failure(authorization.Conflict)
				}
				if e := s.authorizeResource(tx, current.Scope, "resource.reconcile"); e != nil {
					return e
				}
				action := "resource.takeover"
				if current.View.Intent == "RESUME" {
					action = "resource.resume"
				}
				if e := s.authorizeResource(tx, current.Scope, action); e != nil {
					return e
				}
				if current.View.Intent == "RESUME" && (resourceView(current, j.Shared).Blocking > 0 || !observation.Quiescent || observation.Intent != "TAKEOVER") {
					return failure(authorization.Conflict)
				}
				current.Sent = true
				current.Observation = observation
				j.Shared.Scopes[key] = current
				send = true
				return nil
			})
			if err != nil {
				return
			}
			if send {
				observation, err = s.resource.driver.ApplyResourceControl(bounded, ResourceCommand{state.OperationID, state.Scope.Ref, state.Scope.Authority, state.View.Version, state.View.Intent, observation.ResourceVersion})
				if err != nil {
					return
				}
			}
		}
		if !validResourceObservation(state, observation) {
			err = failure(authorization.Denied)
			return
		}
		save, stop := context.WithTimeout(context.WithoutCancel(ctx), s.config.IOTimeout)
		defer stop()
		err = s.transaction(save, func(j *journal, tx authorization.ExecutionTransaction) error {
			if e := s.authorizeResource(tx, state.Scope, "resource.reconcile"); e != nil {
				return e
			}
			current := j.Shared.Scopes[key]
			if current.View.Version != state.View.Version || current.Generation != state.Generation {
				return failure(authorization.Conflict)
			}
			current.Observation = observation
			current.View.ResourceVersion = observation.ResourceVersion
			current.LeaseUntil = 0
			if observation.ControlVersion == current.View.Version && observation.Intent == current.View.Intent && observation.Quiescent && resourceView(current, j.Shared).Blocking == 0 {
				current.View.Progress = "APPLIED"
			}
			j.Shared.Scopes[key] = current
			view = resourceView(current, j.Shared)
			return nil
		})
	}()
	select {
	case out := <-done:
		return out.view, out.err
	case <-bounded.Done():
		return state.View, nil
	}
}
func validResourceObservation(state controlState, o ResourceObservation) bool {
	return o.Resource == state.Scope.Ref && o.Authority == state.Scope.Authority && o.ControlVersion > 0 && o.ResourceVersion > 0 && (o.Intent == "TAKEOVER" || o.Intent == "RESUME")
}

// ReconcileResource handles this bound participant's original work. Other
// participants use their own identities; final control settlement counts all.
func (s *Service) ReconcileResource(ctx context.Context, ref ResourceRef, limit int) error {
	if s.resource == nil || limit < 1 || limit > 16 {
		return failure(authorization.Invalid)
	}
	records := []Record{}
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		_, state, e := s.resourceState(j, ref)
		if e != nil {
			return e
		}
		if state.Scope.Ref != s.resource.scope.Ref {
			return failure(authorization.Unsupported)
		}
		if e = s.authorizeResource(tx, state.Scope, "resource.reconcile"); e != nil {
			return e
		}
		records = nil
		keys := []string{}
		for op, r := range j.Records {
			if s.owned(r) && r.Effect == "UNKNOWN" {
				keys = append(keys, op)
			}
		}
		sort.Strings(keys)
		cursor := j.Shared.Cursors[s.cap.Digest()]
		start := sort.SearchStrings(keys, cursor)
		for start < len(keys) && keys[start] <= cursor {
			start++
		}
		if start == len(keys) {
			start = 0
		}
		for i := 0; i < min(limit, len(keys)); i++ {
			op := keys[(start+i)%len(keys)]
			j.Shared.Cursors[s.cap.Digest()] = op
			r := j.Records[op]
			if !r.Started && r.Request.ControlVersion != state.View.Version {
				if len(r.Reports) >= s.config.MaxReports || pending(j) >= s.config.MaxOutbox {
					return failure(authorization.Unavailable)
				}
				r.Phase = "NOT_STARTED"
				r.Result = "FAILURE"
				r.Effect = "NOT_OCCURRED"
				r.Revision++
				r.Reports = append(r.Reports, tasks.ExecutionReport{OperationID: op, Qualification: r.Request.Qualification, Revision: r.Revision, Phase: r.Phase, Result: r.Result, Effect: r.Effect, PreStart: true})
				j.Records[op] = r
			} else if r.Started {
				records = append(records, r)
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, r := range records {
		if _, e = s.Reconcile(ctx, r.Request.OperationID); e != nil && !authorization.Is(e, authorization.Conflict) && !authorization.Is(e, authorization.Unavailable) {
			return e
		}
	}
	return s.Drain(ctx, limit)
}

// ResourceRecovery is the consumer seam used by a host's bounded recovery pass.
// Each participant keeps its own authenticated identity and durable cursor.
type ResourceRecovery interface {
	AdvanceResourceControl(context.Context, ResourceRef) (ResourceControl, error)
	ReconcileResource(context.Context, ResourceRef, int) error
}
type ResourceParticipant struct {
	Resource ResourceRef
	Recovery ResourceRecovery
}
type ResourceRecoveryResult struct {
	Resource ResourceRef
	Control  ResourceControl
	Err      error
}

// RecoverResources visits every configured participant, even if another range is
// unavailable. The caller invokes another pass after the configured poll interval.
func RecoverResources(ctx context.Context, participants []ResourceParticipant, limit int) ([]ResourceRecoveryResult, error) {
	if len(participants) < 1 || len(participants) > 32 || limit < 1 || limit > 16 {
		return nil, failure(authorization.Invalid)
	}
	for _, p := range participants {
		if p.Recovery == nil || !validResource(p.Resource) {
			return nil, failure(authorization.Invalid)
		}
	}
	results := make([]ResourceRecoveryResult, 0, len(participants))
	for _, p := range participants {
		v, e := p.Recovery.AdvanceResourceControl(ctx, p.Resource)
		if err := p.Recovery.ReconcileResource(ctx, p.Resource, limit); err != nil && e == nil {
			e = err
		}
		results = append(results, ResourceRecoveryResult{p.Resource, v, e})
	}
	return results, nil
}
