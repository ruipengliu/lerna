package authorization

import (
	"context"
	"encoding/hex"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	wire "lerna/gen/harness/v1"
	"time"
)

type budget struct {
	left     int
	deadline time.Time
}

func (s *Service) newBudget() *budget {
	return &budget{s.config.MaxWork, time.Now().Add(s.config.EvaluationTimeout)}
}
func (b *budget) step() error {
	b.left--
	if b.left < 0 || time.Now().After(b.deadline) {
		return fail(Denied)
	}
	return nil
}
func known(message proto.Message) bool {
	if message == nil {
		return false
	}
	m := message.ProtoReflect()
	if !m.IsValid() || len(m.GetUnknown()) != 0 {
		return false
	}
	ok := true
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Kind() == protoreflect.MessageKind {
			if f.IsList() {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					if !known(l.Get(i).Message().Interface()) {
						ok = false
						return false
					}
				}
			} else {
				ok = known(v.Message().Interface())
			}
		}
		return ok
	})
	return ok
}
func resources(st *State, selector *wire.ResourceSelector, b *budget, maxDepth int) (map[string]bool, error) {
	out := make(map[string]bool)
	if selector == nil {
		return nil, fail(Invalid)
	}
	add := func(id string) error {
		if err := b.step(); err != nil {
			return err
		}
		if _, ok := st.Resources[id]; !ok {
			return fail(Invalid)
		}
		out[id] = true
		return nil
	}
	switch sel := selector.Selection.(type) {
	case *wire.ResourceSelector_Exact:
		if err := add(sel.Exact); err != nil {
			return nil, err
		}
	case *wire.ResourceSelector_Set:
		if sel.Set == nil || len(sel.Set.Ids) == 0 {
			return nil, fail(Invalid)
		}
		for _, id := range sel.Set.Ids {
			if err := add(id); err != nil {
				return nil, err
			}
		}
	case *wire.ResourceSelector_Subtree:
		if _, ok := st.Resources[sel.Subtree]; !ok {
			return nil, fail(Invalid)
		}
		for id := range st.Resources {
			cursor := id
			for depth := 0; cursor != ""; depth++ {
				if err := b.step(); err != nil {
					return nil, err
				}
				if depth >= maxDepth {
					return nil, fail(Denied)
				}
				if cursor == sel.Subtree {
					out[id] = true
					break
				}
				cursor = st.Resources[cursor]
			}
		}
	default:
		return nil, fail(Unsupported)
	}
	return out, nil
}
func validSet(values []string) bool {
	if len(values) == 0 || len(values) > 128 {
		return false
	}
	seen := make(map[string]bool)
	for _, v := range values {
		if !validName(v) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func subset(a, b []string) bool {
	for _, v := range a {
		if !contains(b, v) {
			return false
		}
	}
	return true
}
func (s *Service) validateScope(st *State, scope *wire.AuthorizationScope, now time.Time, b *budget) error {
	if scope == nil || !known(scope) {
		return fail(Invalid)
	}
	if len(scope.RequiredConstraints) > 0 {
		return fail(Unsupported)
	}
	if !validSet(scope.Actions) || !validSet(scope.Purposes) || !validSet(scope.Locations) || scope.ExpiresUnix <= now.Unix() || scope.ExpiresUnix > now.Add(s.config.GrantTTL).Unix() {
		return fail(Invalid)
	}
	_, err := resources(st, scope.Resources, b, s.config.MaxDepth)
	return err
}
func (s *Service) scopeContains(st *State, parent, child *wire.AuthorizationScope, b *budget) (bool, error) {
	if parent == nil || child == nil || len(parent.RequiredConstraints) > 0 || len(child.RequiredConstraints) > 0 {
		return false, fail(Unsupported)
	}
	if parent.ExpiresUnix < child.ExpiresUnix || !subset(child.Actions, parent.Actions) || !subset(child.Purposes, parent.Purposes) || !subset(child.Locations, parent.Locations) {
		return false, nil
	}
	// Finite selectors cannot authorize an open subtree, even when their current
	// expansions coincide. Registered descendants may be added in the future.
	if child.GetResources().GetSubtree() != "" && parent.GetResources().GetSubtree() == "" {
		return false, nil
	}
	p, err := resources(st, parent.Resources, b, s.config.MaxDepth)
	if err != nil {
		return false, err
	}
	c, err := resources(st, child.Resources, b, s.config.MaxDepth)
	if err != nil {
		return false, err
	}
	for id := range c {
		if !p[id] {
			return false, nil
		}
	}
	return true, nil
}
func (s *Service) matches(st *State, scope *wire.AuthorizationScope, action *wire.AuthorizationAction, now time.Time, b *budget) (bool, error) {
	if err := b.step(); err != nil {
		return false, err
	}
	if scope == nil || len(scope.RequiredConstraints) > 0 {
		return false, fail(Unsupported)
	}
	if now.Unix() >= scope.ExpiresUnix || !contains(scope.Actions, action.Action) || !contains(scope.Purposes, action.Purpose) || !contains(scope.Locations, action.Location) {
		return false, nil
	}
	set, err := resources(st, scope.Resources, b, s.config.MaxDepth)
	return set[action.Resource], err
}
func (s *Service) policyAllows(st *State, action *wire.AuthorizationAction, now time.Time, b *budget) (bool, int64, error) {
	allowed := false
	var until int64
	if len(st.Rules) > s.config.MaxRules {
		return false, 0, fail(Denied)
	}
	for _, rule := range st.Rules {
		match, err := s.matches(st, rule.Scope, action, now, b)
		if err != nil {
			return false, 0, err
		}
		if !match {
			continue
		}
		if rule.Deny {
			return false, 0, nil
		}
		allowed = true
		if rule.Scope.ExpiresUnix > until {
			until = rule.Scope.ExpiresUnix
		}
	}
	return allowed, until, nil
}
func (s *Service) policyCovers(st *State, scope *wire.AuthorizationScope, now time.Time, b *budget) error {
	set, err := resources(st, scope.Resources, b, s.config.MaxDepth)
	if err != nil {
		return err
	}
	for id := range set {
		for _, a := range scope.Actions {
			for _, purpose := range scope.Purposes {
				for _, location := range scope.Locations {
					allowed, until, err := s.policyAllows(st, &wire.AuthorizationAction{Resource: id, Action: a, Purpose: purpose, Location: location}, now, b)
					if err != nil {
						return err
					}
					if !allowed || until < scope.ExpiresUnix {
						return fail(Denied)
					}
				}
			}
		}
	}
	return nil
}
func (s *Service) Evaluate(ctx context.Context, token string, action *wire.AuthorizationAction) (*wire.AuthorizationDecision, error) {
	if action == nil {
		return nil, fail(Invalid)
	}
	action = proto.Clone(action).(*wire.AuthorizationAction)
	var out *wire.AuthorizationDecision
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		out, err = s.evaluate(st, p, action, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) evaluate(st *State, p Principal, action *wire.AuthorizationAction, now time.Time) (*wire.AuthorizationDecision, error) {
	out := &wire.AuthorizationDecision{Reason: "DENIED", Revision: st.Revision}
	if !known(action) || len(action.RequiredConstraints) > 0 {
		return nil, fail(Unsupported)
	}
	if !validName(action.Resource) || !validName(action.Action) || !validName(action.Purpose) || !validName(action.Location) {
		return nil, fail(Invalid)
	}
	if _, ok := st.Resources[action.Resource]; !ok {
		return out, nil
	}
	b := s.newBudget()
	allowed, policyUntil, err := s.policyAllows(st, action, now, b)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return out, nil
	}
	for _, grant := range st.Grants {
		if err := b.step(); err != nil {
			return nil, err
		}
		if grant.Subject != p.Subject {
			continue
		}
		match, err := s.matches(st, grant.Scope, action, now, b)
		if err != nil {
			return nil, err
		}
		if match && grant.Mode == "continuous" {
			until := min(grant.Scope.ExpiresUnix, policyUntil, p.Expires)
			out = &wire.AuthorizationDecision{Allowed: true, Reason: "ALLOWED", Revision: st.Revision, ValidUntilUnix: until, Limits: proto.Clone(action).(*wire.AuthorizationAction)}
			return out, nil
		}
	}
	return out, nil
}

func (s *Service) GetPolicy(ctx context.Context, token string) (*wire.PolicyView, error) {
	var out *wire.PolicyView
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if !p.Administrator {
			return fail(Denied)
		}
		out = &wire.PolicyView{Revision: st.Revision}
		for _, r := range st.Rules {
			out.Rules = append(out.Rules, proto.Clone(r).(*wire.PolicyRule))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) GetGrant(ctx context.Context, token, id string) (*wire.LocalGrant, error) {
	var out *wire.LocalGrant
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if !p.Administrator {
			return fail(Denied)
		}
		grant, ok := st.Grants[id]
		if !ok {
			return fail(NotFound)
		}
		out = proto.Clone(grant).(*wire.LocalGrant)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) registerPrincipal(st *State, r *wire.RegisterPrincipal, now time.Time) (string, error) {
	if r == nil || !validName(r.Subject) || len(r.CredentialSha256) != 32 || r.ExpiresUnix <= now.Unix() || r.ExpiresUnix > now.Add(s.config.CredentialTTL).Unix() {
		return "", fail(Invalid)
	}
	hash := hex.EncodeToString(r.CredentialSha256)
	if previous, exists := st.Principals[hash]; exists && previous.Subject != r.Subject {
		return "", fail(IdentityConflict)
	}
	admin := false
	for oldHash, p := range st.Principals {
		if p.Subject == r.Subject {
			admin = p.Administrator
			delete(st.Principals, oldHash)
		}
	}
	if len(st.Principals) >= s.config.MaxResources {
		return "", fail(Invalid)
	}
	st.Principals[hash] = Principal{Subject: r.Subject, Expires: r.ExpiresUnix, Disabled: r.Disabled, Administrator: admin}
	return r.Subject, nil
}
