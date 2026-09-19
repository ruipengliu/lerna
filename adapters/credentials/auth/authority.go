// Package auth maps credential management and use to Harness policy.
package auth

import (
	"context"
	"lerna/authorization"
	"lerna/credentials"
	wire "lerna/gen/harness/v1"
)

type Views interface {
	ViewActions(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)
}
type Authority struct {
	views    Views
	resource string
}

func New(views Views, resource string) (*Authority, error) {
	if views == nil || resource == "" || len(resource) > 256 {
		return nil, credentials.Invalid
	}
	return &Authority{views, resource}, nil
}
func (a *Authority) Check(ctx context.Context, token string, b credentials.Binding, verb string) error {
	if !b.Valid() || (verb != "manage" && verb != "use") {
		return credentials.Invalid
	}
	v, e := a.views.ViewActions(ctx, token, []*wire.AuthorizationAction{{Resource: a.resource, Action: "credential." + verb, Purpose: b.Purpose, Location: b.Location}})
	if e != nil || v.Identity.Namespace != b.Namespace || v.Identity.Subject != b.Subject || len(v.Allowed) != 1 || !v.Allowed[0] {
		return credentials.Denied
	}
	return nil
}
