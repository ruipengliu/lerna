package execution

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"reflect"
	"slices"
	"time"
)

func resourceKey(r ResourceRef) string { b, _ := json.Marshal(r); return string(b) }
func validResource(r ResourceRef) bool {
	return r.Namespace != "" && len(r.Namespace) <= 64 && r.Kind != "" && len(r.Kind) <= 64 && r.Key != "" && len(r.Key) <= 128
}
func (s *Service) WithResourceControl(scope ResourceScope, d ResourceDriver) (*Service, error) {
	if d == nil || !validResource(scope.Ref) || scope.Ref.Namespace != s.binding.Namespace || scope.Authority == "" || len(scope.Authority) > 128 || scope.AuthorizationResource != s.cap.Resource || scope.Purpose != s.cap.Purpose || scope.Location != s.cap.Location || len(scope.Participants) < 1 || len(scope.Participants) > 16 || len(scope.Aliases) > 16 || scope.MaxOperations < 1 || scope.MaxOperations > 64 || scope.MaxChecks < 1 || scope.MaxChecks > 16 || scope.PollInterval < time.Millisecond || scope.PollInterval > time.Minute || scope.Lease < s.config.IOTimeout || scope.Lease > time.Minute || scope.Window < scope.Lease || scope.Window > time.Hour {
		return nil, failure(authorization.Invalid)
	}
	scope.Participants = slices.Clone(scope.Participants)
	scope.Aliases = slices.Clone(scope.Aliases)
	slices.Sort(scope.Participants)
	if !slices.Contains(scope.Participants, s.cap.Digest()) {
		return nil, failure(authorization.Denied)
	}
	for i, p := range scope.Participants {
		if len(p) != 64 || (i > 0 && scope.Participants[i-1] == p) {
			return nil, failure(authorization.Invalid)
		}
	}
	for _, a := range scope.Aliases {
		if !validResource(a) || a.Namespace != scope.Ref.Namespace {
			return nil, failure(authorization.Invalid)
		}
	}
	out := *s
	out.resource = &controlBinding{scope, d}
	return &out, nil
}
func (s *Service) prepareResource(j *journal, tx authorization.ExecutionTransaction) error {
	if s.resource == nil {
		return nil
	}
	scope := s.resource.scope
	k := resourceKey(scope.Ref)
	if old, ok := j.Shared.Scopes[k]; ok {
		if !reflect.DeepEqual(old.Scope, scope) {
			return failure(authorization.IdentityConflict)
		}
		return nil
	}
	if len(j.Shared.Scopes) >= 8 {
		return failure(authorization.Unavailable)
	}
	for _, p := range scope.Participants {
		for _, invocation := range j.Shared.Invocations {
			if invocation.Descriptor == p {
				return failure(authorization.Unsupported)
			}
		}
		if old, ok := j.Shared.Participants[p]; ok && old != k {
			return failure(authorization.IdentityConflict)
		}
	}
	for _, ref := range append([]ResourceRef{scope.Ref}, scope.Aliases...) {
		key := resourceKey(ref)
		if old, ok := j.Shared.Aliases[key]; ok && old != k {
			return failure(authorization.IdentityConflict)
		}
		j.Shared.Aliases[key] = k
	}
	for _, p := range scope.Participants {
		j.Shared.Participants[p] = k
	}
	j.Shared.Scopes[k] = controlState{Scope: scope, View: ResourceControl{Resource: scope.Ref, Version: 1, Intent: "RESUME", Progress: "ACCEPTED"}, Until: tx.Now().Add(scope.Window).UnixNano()}
	return nil
}
func (s *Service) indexResource(j *journal) {
	for op, r := range j.Records {
		j.Shared.indexInvocation(s.cap.Digest(), op, r)
	}
}
func (j *resourceJournal) indexInvocation(descriptor, op string, r Record) {
	ref := ResourceRef{}
	if key, ok := j.Participants[descriptor]; ok {
		ref = j.Scopes[key].Scope.Ref
	}
	j.Invocations[op] = resourceInvocation{descriptor, op, ref, r.Started, r.Effect != "UNKNOWN", r.ConflictingEvidence}
}
func (s *Service) guardResource(j *journal, r Request) error {
	if s.resource == nil {
		if _, ok := j.Shared.Participants[s.cap.Digest()]; ok {
			return failure(authorization.Denied)
		}
		for _, v := range j.Shared.Scopes {
			if v.Scope.AuthorizationResource == s.cap.Resource {
				return failure(authorization.Unsupported)
			}
		}
		if r.ControlVersion != 0 {
			return failure(authorization.Unsupported)
		}
		return nil
	}
	state := j.Shared.Scopes[resourceKey(s.resource.scope.Ref)]
	if r.ControlVersion != state.View.Version || state.View.Intent != "RESUME" || state.View.Progress != "APPLIED" {
		return failure(authorization.Conflict)
	}
	for _, v := range j.Shared.Invocations {
		if v.Resource == state.Scope.Ref && (v.Conflict || v.Started && !v.Resolved) && v.OperationID != r.OperationID {
			return failure(authorization.Conflict)
		}
	}
	return nil
}
func (s *Service) controlQualification(r Request) *ControlQualification {
	if s.resource == nil {
		return nil
	}
	return &ControlQualification{s.resource.scope.Ref, s.resource.scope.Authority, r.ControlVersion}
}
func (s *Service) resourceState(j *journal, ref ResourceRef) (string, controlState, error) {
	if !validResource(ref) || ref.Namespace != s.binding.Namespace {
		return "", controlState{}, failure(authorization.Denied)
	}
	k, ok := j.Shared.Aliases[resourceKey(ref)]
	if !ok {
		return "", controlState{}, failure(authorization.Denied)
	}
	return k, j.Shared.Scopes[k], nil
}
func (s *Service) authorizeResource(tx authorization.ExecutionTransaction, scope ResourceScope, action string) error {
	id, e := tx.AuthorizeAction(s.binding.Token, &wire.AuthorizationAction{Resource: scope.AuthorizationResource, Action: action, Purpose: scope.Purpose, Location: scope.Location})
	if e != nil {
		return e
	}
	if id.Subject != s.binding.Subject || id.Namespace != scope.Ref.Namespace {
		return failure(authorization.Denied)
	}
	return nil
}
func resourceView(state controlState, j *resourceJournal) ResourceControl {
	v := state.View
	v.Blocking = 0
	for _, op := range j.Invocations {
		if op.Resource == v.Resource && (op.Started && !op.Resolved || op.Conflict) {
			v.Blocking++
		}
	}
	if v.Intent == "TAKEOVER" && v.Blocking > 0 {
		v.Progress = "ACCEPTED"
	}
	if v.Progress != "APPLIED" {
		v.Limitation = "TARGET_UNCONFIRMED"
		if v.Blocking > 0 {
			v.Limitation = "INVOCATIONS_UNRESOLVED"
		}
		if v.Checks >= uint32(state.Scope.MaxChecks) {
			v.Limitation = "BUDGET_EXHAUSTED"
		}
	}
	return v
}
func (s *Service) GetResourceControl(ctx context.Context, ref ResourceRef) (ResourceControl, error) {
	var out ResourceControl
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		_, state, e := s.resourceState(j, ref)
		if e != nil {
			return e
		}
		if e = s.authorizeResource(tx, state.Scope, "resource.control.read"); e != nil {
			return e
		}
		out = resourceView(state, j.Shared)
		if out.Progress != "APPLIED" && tx.Now().UnixNano() >= state.Until {
			out.Limitation = "BUDGET_EXHAUSTED"
		}
		return nil
	})
	return out, e
}
func (s *Service) RequestResourceControl(ctx context.Context, in ResourceControlRequest) (ResourceControlReceipt, error) {
	if in.Intent != "TAKEOVER" && in.Intent != "RESUME" || len(in.OperationID) < 1 || len(in.OperationID) > 512 || in.ExpectedVersion == 0 {
		return ResourceControlReceipt{}, failure(authorization.Invalid)
	}
	var out ResourceControlReceipt
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		key, state, e := s.resourceState(j, in.Resource)
		if e != nil {
			return e
		}
		action := "resource.takeover"
		if in.Intent == "RESUME" {
			action = "resource.resume"
		}
		if e = s.authorizeResource(tx, state.Scope, action); e != nil {
			return e
		}
		in.Resource = state.Scope.Ref
		if old, ok := j.Shared.Operations[in.OperationID]; ok {
			if old.Subject != s.binding.Subject {
				return failure(authorization.Denied)
			}
			if old.Request != in {
				return failure(authorization.IdentityConflict)
			}
			if e = s.authorizeResource(tx, state.Scope, "resource.control.read"); e != nil {
				return e
			}
			out = old.Receipt
			return nil
		}
		if _, ok := j.Shared.Invocations[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Cancels[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.ReservedCancels[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Shared.CancelOwners[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		count := 0
		for _, op := range j.Shared.Operations {
			if op.Request.Resource == state.Scope.Ref {
				count++
			}
		}
		if count >= state.Scope.MaxOperations {
			return failure(authorization.Unavailable)
		}
		if state.View.Version != in.ExpectedVersion {
			return failure(authorization.Conflict)
		}
		if in.Intent == "RESUME" && (state.View.Intent != "TAKEOVER" || state.View.Progress != "APPLIED" || resourceView(state, j.Shared).Blocking > 0) {
			return failure(authorization.Conflict)
		}
		if e = tx.ExecutionControlOperation(in.OperationID, s.binding.Subject); e != nil {
			return e
		}
		state.View.Version++
		state.View.Intent = in.Intent
		state.View.Progress = "ACCEPTED"
		state.View.Checks = 0
		state.OperationID = in.OperationID
		state.Sent = false
		state.LeaseUntil = 0
		state.Next = 0
		state.Until = tx.Now().Add(state.Scope.Window).UnixNano()
		state.Generation++
		j.Shared.Scopes[key] = state
		out = ResourceControlReceipt{in.OperationID, in.Resource, state.View.Version}
		j.Shared.Operations[in.OperationID] = controlOperation{in, s.binding.Subject, out}
		return nil
	})
	return out, e
}
func (s *Service) LookupResourceControl(ctx context.Context, op string) (ResourceControlReceipt, error) {
	var out ResourceControlReceipt
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		v, ok := j.Shared.Operations[op]
		if !ok || v.Subject != s.binding.Subject {
			return failure(authorization.Denied)
		}
		_, state, e := s.resourceState(j, v.Request.Resource)
		if e != nil {
			return e
		}
		if e = s.authorizeResource(tx, state.Scope, "resource.control.read"); e != nil {
			return e
		}
		out = v.Receipt
		return nil
	})
	return out, e
}
