// Package fetchauth binds acquisition phases to the current Harness authority.
package fetchauth

import (
	"context"
	"lerna/authorization"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"maps"
	"time"
)

type Views interface {
	ViewActions(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)
}

// Scope is trusted host context, never selected by a webpage or request URL.
// Resources maps each exact URL to its registered authorization resource.
type Scope struct {
	Token, Namespace, Subject, Purpose, Location, Recipient string
	Resources                                               map[string]string
}
type Authority struct {
	views Views
	scope Scope
}

func New(v Views, s Scope) (*Authority, error) {
	if v == nil || s.Token == "" || len(s.Token) > 4096 || len(s.Resources) == 0 || len(s.Resources) > 128 {
		return nil, fetch.Invalid
	}
	for _, x := range []string{s.Namespace, s.Subject, s.Purpose, s.Location, s.Recipient} {
		if x == "" || len(x) > 256 {
			return nil, fetch.Invalid
		}
	}
	for url, resource := range s.Resources {
		if url == "" || len(url) > 4096 || resource == "" || len(resource) > 256 {
			return nil, fetch.Invalid
		}
	}
	s.Resources = maps.Clone(s.Resources)
	return &Authority{v, s}, nil
}
func (a *Authority) Check(ctx context.Context, url, phase string) error {
	resource, ok := a.scope.Resources[url]
	if !ok {
		return fetch.Denied
	}
	if phase != "request" && phase != "release" {
		return fetch.Denied
	}
	actions := []*wire.AuthorizationAction{
		{Resource: resource, Action: "fetch.read", Purpose: a.scope.Purpose, Location: a.scope.Location},
		{Resource: resource, Action: "fetch.process", Purpose: a.scope.Purpose, Location: a.scope.Location},
	}
	if phase == "release" {
		actions = append(actions, &wire.AuthorizationAction{Resource: resource, Action: "fetch.disclose", Purpose: a.scope.Purpose, Location: a.scope.Recipient})
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	view, err := a.views.ViewActions(ctx, a.scope.Token, actions)
	if err != nil || view.Identity.Subject != a.scope.Subject || view.Identity.Namespace != a.scope.Namespace || len(view.Allowed) != len(actions) {
		return fetch.Denied
	}
	for _, allowed := range view.Allowed {
		if !allowed {
			return fetch.Denied
		}
	}
	return nil
}
