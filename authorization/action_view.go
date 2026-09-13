package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	wire "lerna/gen/harness/v1"
	"sort"
	"time"
)

// ActionView evaluates a bounded batch against one current authorization view.
// Revision changes at policy/grant changes and at expiry boundaries; it is not
// an authorization token and must be rechecked before releasing cached results.
type ActionView struct {
	Identity Identity
	Revision string
	Allowed  []bool
}

func (s *Service) ViewActions(ctx context.Context, token string, actions []*wire.AuthorizationAction) (ActionView, error) {
	out := ActionView{}
	if len(actions) > 4096 {
		return out, fail(Invalid)
	}
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, e := authenticate(st, token, now)
		if e != nil {
			return e
		}
		out = ActionView{Identity: Identity{p.Subject, st.Namespace}, Allowed: make([]bool, len(actions))}
		boundaries := []int64{p.Expires}
		for _, r := range st.Rules {
			if r.Scope != nil {
				boundaries = append(boundaries, r.Scope.ExpiresUnix)
			}
		}
		for _, g := range st.Grants {
			if g.Scope != nil {
				boundaries = append(boundaries, g.Scope.ExpiresUnix)
			}
		}
		sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })
		expired := 0
		for _, at := range boundaries {
			if now.Unix() >= at {
				expired++
			}
		}
		b, _ := json.Marshal(struct {
			Namespace, Subject string
			Revision           uint64
			Expired            int
		}{st.Namespace, p.Subject, st.Revision, expired})
		out.Revision = fmt.Sprintf("%x", sha256.Sum256(b))
		for i, a := range actions {
			if e = ctx.Err(); e != nil {
				return e
			}
			if a == nil || !known(a) {
				return fail(Invalid)
			}
			d, e := s.evaluate(st, p, a, now)
			if e != nil {
				return e
			}
			out.Allowed[i] = d.Allowed
		}
		return nil
	})
	return out, err
}
