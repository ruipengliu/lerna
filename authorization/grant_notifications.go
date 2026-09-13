package authorization

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"time"
)

// ConfirmApplied is a trusted recipient-adapter seam. The adapter must first
// atomically apply the referenced authority state and its local cursor. A
// transport receipt is insufficient. Confirmation does not mutate authority rev.
func (g *GrantAuthority) ConfirmApplied(ctx context.Context, p GrantPresentation, id string, revision uint64) error {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	return g.service.update(ctx, func(st *State, now time.Time) error {
		if st.Namespace != p.Namespace {
			return fail(Denied)
		}
		if err := g.journal(st); err != nil {
			return err
		}
		e, ok := st.Signed.Grants[id]
		if !ok {
			return fail(NotFound)
		}
		if revision != e.Record.Revision {
			return fail(Conflict)
		}
		bound := false
		for target, entry := range st.Signed.Grants {
			v := entry.Record.Spec
			certificate, err := grantCertificate(st, v, now)
			if err == nil && g.descends(st, target, id) && v.Subject == p.Subject && v.Audience == p.Audience && v.Presenter == p.Presenter && certificate == p.CertificateSHA256 {
				bound = true
			}
		}
		if !bound {
			return fail(Denied)
		}
		for _, n := range e.Record.Notices {
			if n.Recipient == p.Audience && n.Revision == revision {
				n.Applied = true
				return nil
			}
		}
		return fail(NotFound)
	})
}
func (g *GrantAuthority) LookupOperation(ctx context.Context, token, id string) (*wire.GrantReceipt, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	var out *wire.GrantReceipt
	err := g.service.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if err = g.journal(st); err != nil {
			return err
		}
		if _, err = windowOf(st, id); err != nil {
			return err
		}
		op, ok := st.Signed.Operations[id]
		if !ok {
			return fail(NotFound)
		}
		if op.Subject != p.Subject {
			return fail(Denied)
		}
		if err = g.manage(st, p, op.Request, now); err != nil {
			return err
		}
		out = proto.Clone(op.Receipt).(*wire.GrantReceipt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
