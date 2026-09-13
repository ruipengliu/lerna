package authorization

import (
	wire "lerna/gen/harness/v1"
	"slices"
	"time"
)

// The grant scope and its original certificate remain immutable provenance.
// Only administrator-authorized rotation can map that provenance to a new key.
func grantCertificate(st *State, spec *wire.SignedGrantSpec, now time.Time) (string, error) {
	if st.Nodes == nil {
		return spec.CertificateSha256, nil
	}
	r, ok := st.Nodes.Records[spec.Presenter]
	if !ok || !slices.Contains(r.Previous, spec.CertificateSha256) && r.CertificateSHA256 != spec.CertificateSha256 {
		return "", fail(Denied)
	}
	if err := checkNode(st, now, st.Namespace, spec.Presenter, r.CertificateSHA256, spec.Subject); err != nil {
		return "", err
	}
	receiver, ok := st.Nodes.Records[spec.Audience]
	if !ok || receiver.Disabled || now.Unix() >= receiver.Expires {
		return "", fail(Denied)
	}
	return r.CertificateSHA256, nil
}
func permitMatches(st *State, now time.Time, permit UsePermit, p GrantPresentation) bool {
	if st.Nodes != nil {
		if st.Signed == nil {
			return false
		}
		entry, ok := st.Signed.Grants[permit.GrantID]
		if !ok {
			return false
		}
		cert, err := grantCertificate(st, entry.Record.Spec, now)
		if err != nil || cert != p.CertificateSHA256 {
			return false
		}
		p.CertificateSHA256 = permit.CertificateSHA256
	}
	return permit.matches(p)
}
