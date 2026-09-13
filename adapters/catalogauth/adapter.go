package catalogauth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/catalog"
	wire "lerna/gen/harness/v1"
	"time"
)

type Authority interface {
	ViewActions(context.Context, string, []*wire.AuthorizationAction) (authorization.ActionView, error)
}
type SourcePolicy interface {
	Check(context.Context, *wire.ContentSource, string, string, string, int64) error
	Revision(context.Context, int64) (string, error)
}
type Clock interface{ Now() (time.Time, error) }
type Adapter struct {
	Authority   Authority
	Policy      SourcePolicy
	Clock       Clock
	QuerySource catalog.Source
}

func (a Adapter) View(ctx context.Context, q catalog.QueryContext, policies []catalog.Policy) (catalog.Access, error) {
	denied := func() (catalog.Access, error) {
		return catalog.Access{}, &authorization.Error{Code: authorization.Denied}
	}
	if a.Authority == nil || a.Policy == nil || a.Clock == nil || q.Location != "local" || a.QuerySource.Kind == "" || a.QuerySource.Key == "" || a.QuerySource.Revision == 0 {
		return denied()
	}
	now, e := a.Clock.Now()
	if e != nil {
		return catalog.Access{}, e
	}
	before, e := a.Policy.Revision(ctx, now.Unix())
	if e != nil {
		return catalog.Access{}, e
	}
	source := func(s catalog.Source) *wire.ContentSource {
		return &wire.ContentSource{Kind: s.Kind, Key: s.Key, Revision: s.Revision}
	}
	if e = a.Policy.Check(ctx, source(a.QuerySource), "process", q.Purpose, q.Location, now.Unix()); e != nil {
		return denied()
	}
	unique := map[catalog.Policy]int{}
	actions := []*wire.AuthorizationAction{}
	positions := make([]int, len(policies))
	sourceAllowed := map[catalog.Source]bool{}
	for i, p := range policies {
		positions[i] = -1
		if p.Location != q.Location || p.Purpose != q.Purpose {
			continue
		}
		allowed, checked := sourceAllowed[p.Source]
		if !checked {
			allowed = true
			for _, action := range []string{"process", "discover", "disclose"} {
				if e = a.Policy.Check(ctx, source(p.Source), action, q.Purpose, q.Location, now.Unix()); e != nil {
					if e != artifacts.Error("PERMISSION_DENIED") {
						return catalog.Access{}, e
					}
					allowed = false
					break
				}
			}
			sourceAllowed[p.Source] = allowed
		}
		if !allowed {
			continue
		}
		index, ok := unique[p]
		if !ok {
			index = len(actions)
			unique[p] = index
			actions = append(actions, &wire.AuthorizationAction{Resource: p.Resource, Action: q.Action, Purpose: p.Purpose, Location: p.Location})
		}
		positions[i] = index
	}
	v, e := a.Authority.ViewActions(ctx, q.Token, actions)
	if e != nil {
		return catalog.Access{}, e
	}
	if len(v.Allowed) != len(actions) {
		return denied()
	}
	afterTime, e := a.Clock.Now()
	if e != nil {
		return catalog.Access{}, e
	}
	after, e := a.Policy.Revision(ctx, afterTime.Unix())
	if e != nil {
		return catalog.Access{}, e
	}
	if before != after {
		return catalog.Access{}, &authorization.Error{Code: authorization.Conflict}
	}
	out := catalog.Access{Namespace: v.Identity.Namespace, Subject: v.Identity.Subject, Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(v.Revision+":"+after))), Allowed: make([]bool, len(policies))}
	for i, p := range positions {
		if p >= 0 {
			out.Allowed[i] = v.Allowed[p]
		}
	}
	return out, nil
}
