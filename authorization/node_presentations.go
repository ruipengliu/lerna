package authorization

import (
	"context"
	"slices"
	"time"
)

// NodePresentation is original ingress evidence, not a reusable authentication
// token. It belongs only in the trusted operation store, never a request header.
type NodePresentation struct {
	Presentation GrantPresentation
	Certificate  []byte
}

// RememberPresentation binds the authenticated ingress to an already reserved
// original operation. The transport adapter supplies p and DER, not request data.
func (n *NodeAuthority) RememberPresentation(ctx context.Context, p GrantPresentation, der []byte) error {
	if len(der) == 0 || len(der) > 8192 || CertificateDigest(der) != p.CertificateSHA256 {
		return fail(Invalid)
	}
	der = slices.Clone(der)
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	return n.service.update(ctx, func(st *State, now time.Time) error {
		if err := n.journal(st); err != nil {
			return err
		}
		if st.Signed == nil {
			return fail(Denied)
		}
		permit, ok := st.Signed.Uses[p.OperationID]
		if !ok || !permitMatches(st, now, permit, p) {
			return fail(Denied)
		}
		g := GrantAuthority{service: n.service, config: st.Signed.Config}
		if err := g.chain(st, permit.GrantID, now); err != nil {
			return err
		}
		if err := checkNode(st, now, p.Namespace, p.Presenter, p.CertificateSHA256, p.Subject); err != nil {
			return err
		}
		if old, ok := st.Nodes.Presentations[p.OperationID]; ok {
			// Rotation does not overwrite the original proof. A caller with the newly
			// verified key may recover the same operation through its original permit.
			original := old.Presentation
			original.CertificateSHA256 = p.CertificateSHA256
			if original != p {
				return fail(IdentityConflict)
			}
			return nil
		}
		if len(st.Nodes.Presentations) >= n.config.MaxOperations {
			return fail(Unavailable)
		}
		if st.Nodes.Presentations == nil {
			st.Nodes.Presentations = map[string]NodePresentation{}
		}
		st.Nodes.Presentations[p.OperationID] = NodePresentation{p, der}
		return nil
	})
}

// LookupPresentation is a privileged local recovery port. Reading original
// evidence does not authorize disclosure or another action; RestorePresentation
// and the business authority must revalidate before either.
func (n *NodeAuthority) LookupPresentation(ctx context.Context, token, operation string) (NodePresentation, error) {
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	var out NodePresentation
	err := n.service.update(ctx, func(st *State, now time.Time) error {
		if _, err := nodeAdmin(st, token, now); err != nil {
			return err
		}
		if err := n.journal(st); err != nil {
			return err
		}
		saved, ok := st.Nodes.Presentations[operation]
		if !ok {
			return fail(NotFound)
		}
		out = saved
		out.Certificate = slices.Clone(saved.Certificate)
		return nil
	})
	if err != nil {
		return NodePresentation{}, err
	}
	return out, nil
}
