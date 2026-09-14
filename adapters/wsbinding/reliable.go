package wsbinding

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/randomid"
	"slices"
	"time"
)

func (p *Peer) reliableJournal(ctx context.Context) (*Binding, error) {
	if !slices.Contains(p.capabilities, "reliable.v1") {
		return nil, failure(authorization.Unsupported)
	}
	b, e := p.current(ctx)
	if e != nil {
		return nil, e
	}
	if b.Journal == nil || b.Retain == nil {
		return nil, failure(authorization.Unsupported)
	}
	return b, nil
}
func (p *Peer) receiveReliable(e *wire.Envelope) error {
	ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
	defer cancel()
	b, err := p.reliableJournal(ctx)
	if err != nil {
		return err
	}
	if c := e.GetCursor(); c != nil {
		if e.ReplyTo != nil {
			return failure(authorization.Invalid)
		}
		generation, _, err := b.Journal.Sending(ctx)
		if err != nil {
			return err
		}
		switch c.Kind {
		case "CLOSE":
			if err = b.Journal.CloseReceiving(ctx, c.Generation); err != nil {
				return err
			}
			cursor, err := b.Journal.Cursor(ctx)
			if err != nil {
				return err
			}
			return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: cursor}})
		case "EXPIRED":
			if c.Generation < generation {
				return nil
			}
			if c.Generation != generation {
				return failure(authorization.Invalid)
			}
			return p.closeReliable(ctx, b.Journal)
		case "RESUME":
			if c.Generation < generation {
				return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "CLOSE", Generation: generation}}})
			}
			if err = b.Journal.Acknowledge(ctx, c.Generation, c.Prefix); err != nil {
				return err
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.reliableReady {
				return nil
			}
			p.reliableReady = true
			p.reliableSent = c.Prefix
			return nil
		case "PERSISTED":
			if c.Generation < generation {
				return nil
			}
			return b.Journal.Acknowledge(ctx, c.Generation, c.Prefix)
		default:
			return failure(authorization.Invalid)
		}
	}
	m := e.GetReliable()
	if m == nil || m.MessageId != e.MessageId || e.ReplyTo != nil {
		return failure(authorization.Invalid)
	}
	if len(m.OmittedSha256) > 0 {
		prefix, err := b.Journal.Receive(ctx, m)
		if err != nil {
			return err
		}
		return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "PERSISTED", Generation: m.Generation, Prefix: prefix}}})
	}
	var r *wire.CapabilityRequest
	if m.Request != nil {
		r = m.Request
		if r.Namespace != p.identity.Namespace || r.GetInvoke() == nil {
			return failure(authorization.Unsupported)
		}
	} else if m.Response != nil {
		r, err = b.Journal.Request(ctx, m.Response.ReplyTo)
		if err != nil {
			return err
		}
		env := &wire.Envelope{MessageId: m.MessageId, ReplyTo: &m.Response.ReplyTo}
		if !validAnswer(r, m.Response, env) {
			return failure(authorization.Invalid)
		}
	} else {
		return failure(authorization.Invalid)
	}
	if m.Response == nil || m.Response.GetFailure() == nil {
		if err = b.Disclose(ctx, p.identity, r); err == nil {
			err = b.Retain(ctx, p.identity, r)
		}
		if err != nil {
			if !authorization.Is(err, authorization.Denied) && !authorization.Is(err, authorization.Expired) {
				return err
			}
			id, e := randomid.New()
			if e != nil {
				return e
			}
			prefix, e := b.Journal.Reject(ctx, m, id, authorization.Code(code(err)))
			if e != nil {
				return e
			}
			return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "PERSISTED", Generation: m.Generation, Prefix: prefix}}})
		}
	}
	prefix, err := b.Journal.Receive(ctx, m)
	if err != nil {
		return err
	}
	return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "PERSISTED", Generation: m.Generation, Prefix: prefix}}})
}
func (p *Peer) recoverReliable() error {
	if !slices.Contains(p.capabilities, "reliable.v1") {
		return nil
	}
	ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
	defer cancel()
	b, e := p.reliableJournal(ctx)
	if e != nil {
		return e
	}
	expired, e := b.Journal.expired(ctx)
	if e != nil {
		return e
	}
	if expired {
		return p.closeReliable(ctx, b.Journal)
	}
	pending, e := b.Journal.Unconsumed(ctx)
	if e != nil {
		return e
	}
	for _, m := range pending {
		if m.Response != nil {
			r, e := b.Journal.Request(ctx, m.Response.ReplyTo)
			if e != nil {
				return e
			}
			if m.Response.GetFailure() == nil {
				if e = b.Disclose(ctx, p.identity, r); e != nil {
					continue
				}
			}
			if e = b.Journal.Complete(ctx, m, nil, nil); e != nil {
				return e
			}
			p.mu.Lock()
			wait, ok := p.pending[m.Response.ReplyTo]
			p.mu.Unlock()
			if ok {
				select {
				case wait.response <- m.Response:
				default:
				}
			}
			continue
		}
		if e = b.Journal.canConsume(ctx, m); e != nil {
			if authorization.Is(e, authorization.Expired) {
				return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "EXPIRED", Generation: m.Generation}}})
			}
			return e
		}
		r := m.Request
		if e = b.Retain(ctx, p.identity, r); e == nil {
			e = b.Disclose(ctx, p.identity, r)
		}
		if e != nil {
			id, err := randomid.New()
			if err != nil {
				return err
			}
			denied := &wire.CapabilityResponse{MessageId: id, ReplyTo: r.MessageId, Namespace: r.Namespace, Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: code(e)}}}
			if err = b.Journal.Complete(ctx, m, denied, nil); err != nil {
				return err
			}
			continue
		}
		if b.Execution == nil {
			return failure(authorization.Unsupported)
		}
		if e = b.Execution.MatchPeer(p.identity); e != nil {
			return e
		}
		receipt, err := b.Execution.Invoke(ctx, executionwire.Decode(r.GetInvoke().GetInvocation()), r.GetInvoke().GetGrantMaterial())
		// Unknown acceptance is rechecked by the original operation on the next pass.
		if err != nil && (authorization.Is(err, authorization.Unavailable) || authorization.Is(err, authorization.OutcomeUnknown) || ctx.Err() != nil) {
			continue
		}
		out := &wire.CapabilityResponse{ReplyTo: r.MessageId, Namespace: r.Namespace, Evidence: "durable_capability_execution"}
		out.MessageId, e = randomid.New()
		if e != nil {
			return e
		}
		if err == nil {
			_, err = b.Execution.ReadRemoteInvocation(ctx, receipt.OperationID)
			if err == nil {
				out.Body = &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: receipt.OperationID, Revision: receipt.Revision}}
			}
		}
		if err != nil {
			out.Body = &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: code(err)}}
		}
		if e = b.Disclose(ctx, p.identity, r); e != nil {
			return e
		}
		if e = b.Journal.Complete(ctx, m, out, nil); e != nil {
			return e
		}
	}
	p.mu.Lock()
	ready, sent := p.reliableReady, p.reliableSent
	p.mu.Unlock()
	if !ready {
		return nil
	}
	generation, next, e := b.Journal.Sending(ctx)
	if e != nil {
		return e
	}
	if sent == next {
		return nil
	}
	batch, e := b.Journal.Replay(ctx, generation, sent)
	if authorization.Is(e, authorization.Expired) || authorization.Is(e, authorization.Unavailable) {
		return p.closeReliable(ctx, b.Journal)
	}
	if e != nil {
		return e
	}
	for _, m := range batch {
		r := m.Request
		if r == nil && len(m.OmittedSha256) == 0 {
			r, e = b.Journal.replyGuard(ctx, m.MessageId)
			if e != nil {
				return e
			}
		}
		if r != nil {
			e = b.Disclose(ctx, p.identity, r)
			if e == nil && m.Response != nil && m.Response.GetFailure() == nil {
				_, e = b.Execution.ReadRemoteInvocation(ctx, r.GetInvoke().GetInvocation().GetOperationId())
			}
			if e != nil {
				if !authorization.Is(e, authorization.Denied) && !authorization.Is(e, authorization.Expired) {
					return e
				}
				m, e = b.Journal.Omit(ctx, m)
				if e != nil {
					return e
				}
				r = nil
			}
		}
		if e = b.Journal.Attempt(ctx, m); e != nil {
			if authorization.Is(e, authorization.Unavailable) {
				return p.closeReliable(ctx, b.Journal)
			}
			return e
		}
		if e = p.sendChecked(&wire.Envelope{MessageId: m.MessageId, Body: &wire.Envelope_Reliable{Reliable: m}}, r); e != nil {
			// A partial application assembly needs a new connection. Preserve
			// the refusal first so reconnect does not encounter the same block.
			if authorization.Is(e, authorization.Denied) || authorization.Is(e, authorization.Expired) {
				_, _ = b.Journal.Omit(ctx, m)
			}
			return e
		}
		p.mu.Lock()
		p.reliableSent = m.Position
		p.mu.Unlock()
	}
	return nil
}

// Result retrieves a durable reply after the original caller or connection exits.
func (p *Peer) Result(ctx context.Context, message string) (*wire.CapabilityResponse, error) {
	b, e := p.reliableJournal(ctx)
	if e != nil {
		return nil, e
	}
	r, e := b.Journal.Request(ctx, message)
	if e != nil {
		return nil, e
	}
	if e = b.Disclose(ctx, p.identity, r); e != nil {
		return nil, e
	}
	out, e := b.Journal.Result(ctx, message)
	if e != nil {
		return nil, e
	}
	if out.GetReceipt() != nil {
		id, e := randomid.New()
		if e != nil {
			return nil, e
		}
		query := &wire.CapabilityRequest{MessageId: id, Namespace: r.Namespace, Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: out.GetReceipt().OperationId}}
		raw, e := proto.Marshal(query)
		if e != nil {
			return nil, e
		}
		raw, e = p.Exchange(ctx, raw)
		if e != nil {
			return nil, e
		}
		current := new(wire.CapabilityResponse)
		if proto.Unmarshal(raw, current) != nil {
			return nil, failure(authorization.Invalid)
		}
		if current.GetFailure() != nil {
			return nil, failure(authorization.Code(current.GetFailure().Code))
		}
	}
	return out, nil
}

func (p *Peer) closeReliable(ctx context.Context, j *Journal) error {
	generation, e := j.CloseSending(ctx)
	if e != nil {
		return e
	}
	p.mu.Lock()
	p.reliableReady = false
	p.reliableSent = 0
	p.mu.Unlock()
	return p.enqueue(p.control, &wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: &wire.WSCursor{Kind: "CLOSE", Generation: generation}}})
}

// Recover returns original-operation snapshots after a replay window expires.
// Missing evidence remains UNKNOWN; this method never invokes or starts work.
func (p *Peer) Recover(ctx context.Context) ([]RecoveryRecord, error) {
	b, e := p.reliableJournal(ctx)
	if e != nil {
		return nil, e
	}
	records, e := b.Journal.Recovery(ctx)
	if e != nil {
		return nil, e
	}
	visible := []RecoveryRecord{}
	for _, r := range records {
		id, e := randomid.New()
		if e != nil {
			return nil, e
		}
		req := &wire.CapabilityRequest{MessageId: id, Namespace: r.Namespace, Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: r.OperationID}}
		if e = b.Disclose(ctx, p.identity, req); e != nil {
			continue
		}
		// A saved snapshot is evidence, never a lasting disclosure permission.
		r.Snapshot = nil
		if e = b.Journal.beginRecovery(ctx, r.MessageID, r.Incoming); e != nil {
			if authorization.Is(e, authorization.Unavailable) {
				r.State = "UNKNOWN"
				visible = append(visible, r)
				continue
			}
			return nil, e
		}
		var snapshot *wire.InvocationSnapshot
		if r.Incoming {
			if e = b.Execution.MatchPeer(p.identity); e != nil {
				continue
			}
			value, e := b.Execution.ReadRemoteInvocation(ctx, r.OperationID)
			if e != nil {
				continue
			}
			snapshot = executionwire.Snapshot(value)
		} else {
			raw, e := proto.Marshal(req)
			if e != nil {
				return nil, e
			}
			raw, e = p.Exchange(ctx, raw)
			if e != nil {
				r.State = "UNKNOWN"
				visible = append(visible, r)
				continue
			}
			out := new(wire.CapabilityResponse)
			if proto.Unmarshal(raw, out) != nil || out.GetSnapshot() == nil {
				continue
			}
			snapshot = out.GetSnapshot()
		}
		if e = b.Disclose(ctx, p.identity, req); e != nil {
			continue
		}
		if e = b.Journal.saveRecovery(ctx, r, snapshot); e != nil {
			return nil, e
		}
		r.Snapshot, e = proto.Marshal(snapshot)
		if e != nil {
			return nil, e
		}
		r.State = "KNOWN"
		visible = append(visible, r)
	}
	return visible, nil
}
func (p *Peer) recoveryLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-tick.C:
			ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
			_, _ = p.Recover(ctx)
			cancel()
		}
	}
}
