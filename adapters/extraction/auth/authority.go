// Package auth checks independent extraction permissions in the
// current Harness authority. It does not replace source liveness/residency,
// task execution qualification, Memory admission, or retained-use tracking.
package auth

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"time"
)

type Views interface {
	ViewActions(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)
}

// Scope is fixed by the trusted host for one source resource. Neither source
// contents nor extractor outputs may choose the policy resource or purpose.
type Scope struct{ Namespace, Resource, Purpose, Location string }
type Authority struct {
	views Views
	scope Scope
}

func New(views Views, scope Scope) (*Authority, error) {
	if views == nil {
		return nil, memory.Invalid
	}
	for _, s := range []string{scope.Namespace, scope.Resource, scope.Purpose, scope.Location} {
		if len(s) == 0 || len(s) > 256 {
			return nil, memory.Invalid
		}
	}
	return &Authority{views: views, scope: scope}, nil
}

// Check observes current permission for one effect; callers recheck at the
// actual effect and release boundaries. Success is not a reusable permit.
// Save still requires Memory's original operation admission and source policy.
func (a *Authority) Check(ctx context.Context, b memory.Binding, phase string) error {
	if b.Namespace != a.scope.Namespace || b.Location != a.scope.Location || b.Recipient != a.scope.Location || b.Subject == "" || b.Token == "" {
		return memory.Denied
	}
	var names []string
	switch phase {
	case "read":
		names = []string{"memory.source.read"}
	case "extract":
		names = []string{"memory.source.read", "memory.extract"}
	case "retain":
		names = []string{"memory.candidate.retain"}
	case "save":
		names = []string{"memory.put", "memory.store"}
	case "scan":
		names = []string{"memory.source.read", "memory.extract", "memory.scan"}
	case "cancel-scan":
		names = []string{"memory.scan.cancel"}
	case "disclose":
		names = []string{"memory.candidate.disclose"}
	default:
		return memory.Denied
	}
	actions := make([]*wire.AuthorizationAction, 0, len(names))
	for _, name := range names {
		actions = append(actions, &wire.AuthorizationAction{Resource: a.scope.Resource, Action: name, Purpose: a.scope.Purpose, Location: a.scope.Location})
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	v, err := a.views.ViewActions(bounded, b.Token, actions)
	if err != nil || v.Identity.Namespace != b.Namespace || v.Identity.Subject != b.Subject || len(v.Allowed) != len(actions) {
		return memory.Denied
	}
	for _, allowed := range v.Allowed {
		if !allowed {
			return memory.Denied
		}
	}
	return nil
}
