package tasks

import (
	"context"
	"lerna/authorization"
)

func (p *HandoffPort) Aborted(ctx context.Context, ref Ref, op string) (HandoffEnvelope, error) {
	h, e := p.Status(ctx, ref, op)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if h.Phase != "ABORTED" || len(h.Digest) != 64 {
		return HandoffEnvelope{}, failure(authorization.Conflict)
	}
	return p.sign(handoffMessageOf(h, "abort"))
}
func (p *HandoffPort) DiscardPrepared(ctx context.Context, in HandoffEnvelope) error {
	m, e := p.verify(in, "abort")
	if e != nil {
		return e
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return e
	}
	if !matchesHandoff(h, m) || m.Request.Target != p.service.config.Owner {
		return failure(authorization.IdentityConflict)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		old := j.Handoffs[m.Request.Ref.TaskID]
		if !matchesHandoff(old, m) {
			return failure(authorization.IdentityConflict)
		}
		if old.Phase != "PREPARED" && old.Phase != "ABORTED" {
			return failure(authorization.Conflict)
		}
		old.Phase = "ABORTED"
		j.Handoffs[m.Request.Ref.TaskID] = old
		return nil
	})
}
