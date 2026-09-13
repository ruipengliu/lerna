package authorization

import (
	"encoding/base64"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
)

func target(v *wire.SignedGrantSpec) *wire.GrantDelegateTarget {
	return &wire.GrantDelegateTarget{Subject: v.Subject, Audience: v.Audience, Presenter: v.Presenter, CertificateSha256: v.CertificateSha256}
}
func validTarget(t *wire.GrantDelegateTarget) bool {
	if t == nil || !known(t) || !validName(t.Subject) || !validName(t.Audience) || !validName(t.Presenter) {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(t.CertificateSha256)
	return err == nil && len(b) == 32
}
func targetsContain(parent, child *wire.SignedGrantSpec) bool {
	allowed := func(t *wire.GrantDelegateTarget) bool {
		if proto.Equal(target(parent), t) {
			return true
		}
		for _, a := range parent.DelegateTargets {
			if proto.Equal(a, t) {
				return true
			}
		}
		return false
	}
	if !allowed(target(child)) {
		return false
	}
	for _, t := range child.DelegateTargets {
		if !allowed(t) {
			return false
		}
	}
	return true
}

// attenuates compares immutable scope; allocation admission separately checks
// remaining balance so existing children are never charged a second time.
func (g *GrantAuthority) attenuates(st *State, parent, child *wire.SignedGrantSpec, b *budget) error {
	if !targetsContain(parent, child) || parent.Mode != "continuous" || parent.DelegationDepth <= child.DelegationDepth || child.NotBefore < parent.NotBefore || child.Units > parent.Units {
		return fail(Denied)
	}
	contained, err := g.service.scopeContains(st, parent.Scope, child.Scope, b)
	if err != nil {
		return err
	}
	if !contained {
		return fail(Denied)
	}
	return nil
}
