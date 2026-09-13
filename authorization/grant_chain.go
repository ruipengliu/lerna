package authorization

import "time"

// chain rechecks every ancestor against current policy and trusted registration.
func (g *GrantAuthority) chain(st *State, id string, now time.Time) error {
	seen := map[string]bool{}
	b := g.service.newBudget()
	for depth := 0; id != ""; depth++ {
		if depth > g.config.MaxDepth || seen[id] {
			return fail(Denied)
		}
		seen[id] = true
		e, ok := st.Signed.Grants[id]
		if !ok || e.Record.Revoked {
			return fail(Denied)
		}
		spec := e.Record.Spec
		if _, err := grantCertificate(st, spec, now); err != nil {
			return err
		}
		active := false
		for _, p := range st.Principals {
			if p.Subject == spec.Subject && !p.Disabled && p.Expires > now.Unix() {
				active = true
			}
		}
		if !active {
			return fail(Denied)
		}
		if now.Unix() < spec.NotBefore || now.Unix() >= spec.Scope.ExpiresUnix {
			return fail(Denied)
		}
		if err := g.service.policyCovers(st, spec.Scope, now, b); err != nil {
			return err
		}
		if parent := e.Record.Parent; parent != "" {
			p, ok := st.Signed.Grants[parent]
			if !ok {
				return fail(Denied)
			}
			if err := g.attenuates(st, p.Record.Spec, spec, b); err != nil {
				return err
			}
		}
		id = e.Record.Parent
	}
	return nil
}
func (g *GrantAuthority) descends(st *State, id, ancestor string) bool {
	for depth := 0; id != "" && depth <= g.config.MaxDepth; depth++ {
		if id == ancestor {
			return true
		}
		e, ok := st.Signed.Grants[id]
		if !ok {
			return false
		}
		id = e.Record.Parent
	}
	return false
}
