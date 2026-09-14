package wsbinding

import (
	"context"
	"crypto/tls"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"slices"
	"sync"
	"time"
)

type pending struct {
	request  *wire.CapabilityRequest
	response chan *wire.CapabilityResponse
}
type assembly struct {
	data  []byte
	total uint32
	until time.Time
}
type subscription struct {
	op   string
	last *wire.CapabilityResponse
}

// Peer owns bounded queues and both network loops. Exchange implements the
// existing SDK transport. Closing it never calls a business mutation.
type Peer struct {
	conn                        *websocket.Conn
	host                        Host
	config                      Config
	state                       tls.ConnectionState
	server                      bool
	target                      string
	identity                    authorization.GrantPresentation
	capabilities                []string
	ctx                         context.Context
	cancel                      context.CancelFunc
	mu                          sync.Mutex
	err                         error
	pending                     map[string]pending
	receiving                   map[string]*Subscription
	publishing                  map[string]subscription
	control, outgoing, incoming chan *wire.Envelope
	journal                     *Journal
	reliableReady               bool
	reliableSent                uint64
	sendSeq                     [7]uint64
	recvSeq                     [7]uint64
}

func newPeer(c *websocket.Conn, h Host, cfg Config, state tls.ConnectionState, server bool, target string, p authorization.GrantPresentation, caps []string, journal *Journal) *Peer {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Lifetime)
	v := &Peer{journal: journal, conn: c, host: h, config: cfg, state: state, server: server, target: target, identity: p, capabilities: caps, ctx: ctx, cancel: cancel, pending: map[string]pending{}, receiving: map[string]*Subscription{}, publishing: map[string]subscription{}, control: make(chan *wire.Envelope, cfg.Window), outgoing: make(chan *wire.Envelope, cfg.Window), incoming: make(chan *wire.Envelope, cfg.Window)}
	// Control-frame handlers use the same finite write deadline as data writes.
	c.SetPingHandler(func(s string) error {
		return c.WriteControl(websocket.PongMessage, []byte(s), time.Now().Add(cfg.Timeout))
	})
	go func() { <-ctx.Done(); c.Close() }()
	go v.readLoop()
	go v.writeLoop()
	if journal != nil {
		go v.recoveryLoop()
	}
	return v
}
func (p *Peer) Close()                { p.stop(context.Canceled) }
func (p *Peer) Done() <-chan struct{} { return p.ctx.Done() }
func (p *Peer) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	return p.ctx.Err()
}
func (p *Peer) stop(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.mu.Unlock()
	p.cancel()
	p.conn.Close()
}
func (p *Peer) current(ctx context.Context) (*Binding, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := p.host.Endpoint.Authenticate(ctx, p.state, p.target, p.server); err != nil {
		return nil, err
	}
	v, err := p.host.Endpoint.Presentation(ctx, p.state, p.identity.Subject, "", "")
	if err != nil {
		return nil, err
	}
	if v != p.identity {
		return nil, failure(authorization.Denied)
	}
	b, err := p.host.Resolve(ctx, v)
	if err != nil {
		return nil, err
	}
	if b == nil || b.Peer != v || b.Disclose == nil {
		return nil, failure(authorization.Denied)
	}
	if p.journal != nil {
		if b.Journal == nil || b.Journal.authority != p.journal.authority || b.Journal.peer != p.journal.peer {
			return nil, failure(authorization.Denied)
		}
		if err := p.journal.checkSession(ctx); err != nil {
			return nil, err
		}
		copy := *b
		copy.Journal = p.journal
		b = &copy
	}
	return b, nil
}
func (p *Peer) enqueue(q chan *wire.Envelope, e *wire.Envelope) error {
	select {
	case <-p.ctx.Done():
		return p.Err()
	default:
	}
	select {
	case q <- e:
		return nil
	default:
		return failure(authorization.Unavailable)
	}
}
func (p *Peer) Exchange(ctx context.Context, raw []byte) ([]byte, error) {
	if len(raw) > p.config.MaxMessage {
		return nil, failure(authorization.Invalid)
	}
	r := new(wire.CapabilityRequest)
	if proto.Unmarshal(raw, r) != nil || !taskwire.Known(r.ProtoReflect()) || r.MessageId == "" || len(r.MessageId) > 128 || r.Namespace != p.identity.Namespace {
		return nil, failure(authorization.Invalid)
	}
	reliable := r.GetInvoke() != nil && slices.Contains(p.capabilities, "reliable.v1")
	if !reliable && (method(r) == "" || !slices.Contains(p.capabilities, method(r))) {
		return nil, failure(authorization.Unsupported)
	}
	ch := make(chan *wire.CapabilityResponse, 1)
	p.mu.Lock()
	if len(p.pending) >= p.config.Window {
		p.mu.Unlock()
		return nil, failure(authorization.Unavailable)
	}
	if _, ok := p.pending[r.MessageId]; ok {
		p.mu.Unlock()
		return nil, failure(authorization.IdentityConflict)
	}
	p.pending[r.MessageId] = pending{r, ch}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, r.MessageId); p.mu.Unlock() }()
	if reliable {
		b, err := p.reliableJournal(ctx)
		if err != nil {
			return nil, err
		}
		if err = b.Disclose(ctx, p.identity, r); err != nil {
			return nil, err
		}
		if err = b.Retain(ctx, p.identity, r); err != nil {
			return nil, err
		}
		if _, err = b.Journal.Prepare(ctx, r); err != nil {
			return nil, err
		}
		if out, err := p.Result(ctx, r.MessageId); err == nil {
			return proto.Marshal(out)
		} else if !authorization.Is(err, authorization.NotFound) {
			return nil, err
		}
	} else if err := p.enqueue(p.outgoing, &wire.Envelope{MessageId: r.MessageId, Body: &wire.Envelope_Query{Query: r}}); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	select {
	case out := <-ch:
		return proto.Marshal(out)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, p.Err()
	}
}

// Subscription retains only the newest full snapshot. It acknowledges no durable
// receipt; callers query current state again after disconnect or a sequence gap.
type Subscription struct {
	peer    *Peer
	id, op  string
	updates chan *wire.CapabilityResponse
	done    chan struct{}
	once    sync.Once
}

func (p *Peer) Subscribe(ctx context.Context, op string) (*Subscription, error) {
	if op == "" || len(op) > 512 {
		return nil, failure(authorization.Invalid)
	}
	if !slices.Contains(p.capabilities, "progress.v1") || !slices.Contains(p.capabilities, "invocation.read.v1") {
		return nil, failure(authorization.Unsupported)
	}
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	s := &Subscription{peer: p, id: id, op: op, updates: make(chan *wire.CapabilityResponse, 1), done: make(chan struct{})}
	p.mu.Lock()
	if len(p.receiving) >= p.config.Window {
		p.mu.Unlock()
		return nil, failure(authorization.Unavailable)
	}
	p.receiving[id] = s
	p.mu.Unlock()
	err = p.enqueue(p.control, &wire.Envelope{MessageId: id, Body: &wire.Envelope_Subscription{Subscription: &wire.WSSubscription{OperationId: op}}})
	if err != nil {
		s.Close()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.done:
		case <-p.ctx.Done():
		}
	}()
	return s, nil
}
func (s *Subscription) Receive(ctx context.Context) (*wire.CapabilityResponse, error) {
	select {
	case <-s.done:
		return nil, context.Canceled
	default:
	}
	select {
	case v := <-s.updates:
		if f := v.GetFailure(); f != nil {
			return nil, failure(authorization.Code(f.Code))
		}
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, context.Canceled
	case <-s.peer.ctx.Done():
		return nil, s.peer.Err()
	}
}
func (s *Subscription) Close() {
	s.once.Do(func() {
		close(s.done)
		s.peer.mu.Lock()
		delete(s.peer.receiving, s.id)
		s.peer.mu.Unlock()
		id, err := randomid.New()
		if err == nil {
			err = s.peer.enqueue(s.peer.control, &wire.Envelope{MessageId: id, ReplyTo: &s.id, Body: &wire.Envelope_Subscription{Subscription: &wire.WSSubscription{OperationId: s.op, Close: true}}})
		}
		if err != nil {
			s.peer.stop(err)
		}
	})
}

func stream(e *wire.Envelope) (uint32, string) {
	switch e.Body.(type) {
	case *wire.Envelope_Control, *wire.Envelope_Subscription, *wire.Envelope_Cursor:
		return 1, "control"
	case *wire.Envelope_Query:
		return 2, "query"
	case *wire.Envelope_Answer:
		return 3, "response"
	case *wire.Envelope_Progress:
		return 4, "temporary"
	case *wire.Envelope_Chunk:
		return 5, "content"
	case *wire.Envelope_Extension:
		return 2, "query"
	case *wire.Envelope_Reliable:
		return 6, "reliable"
	}
	return 0, ""
}
func (p *Peer) stamp(e *wire.Envelope) error {
	id, class := stream(e)
	if id == 0 {
		return failure(authorization.Unsupported)
	}
	if e.MessageId == "" {
		var err error
		e.MessageId, err = randomid.New()
		if err != nil {
			return err
		}
	}
	p.sendSeq[id]++
	e.ProtocolMajor = 1
	e.Namespace = p.identity.Namespace
	e.StreamId = id
	e.Seq = p.sendSeq[id]
	e.DeliveryClass = class
	return nil
}
func (p *Peer) send(e *wire.Envelope) error { return p.sendChecked(e, nil) }
func (p *Peer) sendChecked(e *wire.Envelope, request *wire.CapabilityRequest) error {
	check := func() error {
		ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
		defer cancel()
		b, err := p.current(ctx)
		if err != nil {
			return err
		}
		if request != nil {
			if err := b.Disclose(ctx, p.identity, request); err != nil {
				return err
			}
			if m := e.GetReliable(); m != nil && m.Response != nil && m.Response.GetFailure() == nil && request.GetInvoke() != nil {
				if b.Execution == nil {
					return failure(authorization.Unsupported)
				}
				if err := b.Execution.MatchPeer(p.identity); err != nil {
					return err
				}
				_, err := b.Execution.ReadRemoteInvocation(ctx, request.GetInvoke().GetInvocation().GetOperationId())
				return err
			}
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	if err := p.stamp(e); err != nil {
		return err
	}
	raw, err := proto.Marshal(e)
	if err != nil {
		return err
	}
	if len(raw) > p.config.MaxMessage {
		return failure(authorization.Invalid)
	}
	if len(raw) <= p.config.Chunk {
		return writeEnvelope(p.conn, e, p.config.Timeout)
	}
	for offset := 0; offset < len(raw); offset += p.config.Chunk {
		chunk := &wire.Envelope{Body: &wire.Envelope_Chunk{Chunk: &wire.WSChunk{MessageId: e.MessageId, Offset: uint32(offset), Total: uint32(len(raw)), Data: raw[offset:min(offset+p.config.Chunk, len(raw))]}}}
		if err = p.stamp(chunk); err != nil {
			return err
		}
		if err = check(); err != nil {
			return err
		}
		if err = writeEnvelope(p.conn, chunk, p.config.Timeout); err != nil {
			return err
		}
		// Other streams yield to control, but a chunked control must finish
		// before the next control's sequence number can be observed.
		if e.StreamId != 1 {
			select {
			case ctl := <-p.control:
				if err = p.send(ctl); err != nil {
					return err
				}
			default:
			}
		}
	}
	return nil
}
func (p *Peer) check(e *wire.Envelope) error {
	id, class := stream(e)
	if id == 0 || e.ProtocolMajor != 1 || e.Namespace != p.identity.Namespace || e.OperationId != "" || e.MessageId == "" || len(e.MessageId) > 128 || len(e.GetReplyTo()) > 128 || e.StreamId != id || e.DeliveryClass != class || e.Seq != p.recvSeq[id]+1 {
		return failure(authorization.Invalid)
	}
	p.recvSeq[id] = e.Seq
	return nil
}
func (p *Peer) readLoop() {
	pieces := map[string]*assembly{}
	reserved := 0
	for {
		wait := p.config.Timeout
		for _, a := range pieces {
			wait = min(wait, time.Until(a.until))
		}
		if wait <= 0 {
			p.stop(context.DeadlineExceeded)
			return
		}
		e, err := readEnvelope(p.conn, p.config.Chunk+512, wait)
		if err == nil {
			_, err = p.current(p.ctx)
		}
		if err == nil {
			err = p.check(e)
		}
		if err != nil {
			p.stop(err)
			return
		}
		if part := e.GetChunk(); part != nil {
			if e.ReplyTo != nil || part.MessageId == "" || len(part.MessageId) > 128 || part.Total == 0 || int(part.Total) > p.config.MaxMessage || len(part.Data) == 0 || len(part.Data) > p.config.Chunk {
				p.stop(failure(authorization.Invalid))
				return
			}
			a := pieces[part.MessageId]
			if a == nil {
				if part.Offset != 0 || len(pieces) >= p.config.Window || reserved+int(part.Total) > p.config.MaxMessage {
					p.stop(failure(authorization.Unavailable))
					return
				}
				a = &assembly{total: part.Total, until: time.Now().Add(p.config.Timeout)}
				pieces[part.MessageId] = a
				reserved += int(part.Total)
			}
			if a.total != part.Total || int(part.Offset) != len(a.data) || len(a.data)+len(part.Data) > int(a.total) {
				p.stop(failure(authorization.Invalid))
				return
			}
			a.data = append(a.data, part.Data...)
			if len(a.data) < int(a.total) {
				continue
			}
			inner := new(wire.Envelope)
			if proto.Unmarshal(a.data, inner) != nil || !taskwire.Known(inner.ProtoReflect()) || inner.GetChunk() != nil || inner.MessageId != part.MessageId {
				p.stop(failure(authorization.Invalid))
				return
			}
			delete(pieces, part.MessageId)
			reserved -= int(a.total)
			if err = p.check(inner); err != nil {
				p.stop(err)
				return
			}
			e = inner
		}
		if err = p.receive(e); err != nil {
			p.stop(err)
			return
		}
	}
}
func (p *Peer) receive(e *wire.Envelope) error {
	switch v := e.Body.(type) {
	case *wire.Envelope_Reliable, *wire.Envelope_Cursor:
		return p.receiveReliable(e)
	case *wire.Envelope_Query:
		if e.ReplyTo != nil || v.Query.MessageId != e.MessageId || v.Query.Namespace != e.Namespace {
			return failure(authorization.Invalid)
		}
		// Unsupported mutations are rejected by answer() before the domain service.
		return p.enqueue(p.incoming, e)
	case *wire.Envelope_Answer:
		p.mu.Lock()
		waiting, ok := p.pending[e.GetReplyTo()]
		p.mu.Unlock()
		if !ok {
			return nil
		} // Late/unsolicited read replies cannot start or mutate work.
		if !validAnswer(waiting.request, v.Answer, e) {
			return failure(authorization.Invalid)
		}
		select {
		case waiting.response <- v.Answer:
		default:
			return failure(authorization.IdentityConflict)
		}
	case *wire.Envelope_Subscription:
		if !slices.Contains(p.capabilities, "progress.v1") || !slices.Contains(p.capabilities, "invocation.read.v1") || v.Subscription.OperationId == "" || len(v.Subscription.OperationId) > 512 {
			return failure(authorization.Unsupported)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if v.Subscription.Close {
			if old, ok := p.publishing[e.GetReplyTo()]; ok && old.op == v.Subscription.OperationId {
				delete(p.publishing, e.GetReplyTo())
			}
			return nil
		}
		if e.ReplyTo != nil {
			return failure(authorization.Invalid)
		}
		if len(p.publishing) >= p.config.Window {
			return failure(authorization.Unavailable)
		}
		if _, ok := p.publishing[e.MessageId]; ok {
			return failure(authorization.IdentityConflict)
		}
		p.publishing[e.MessageId] = subscription{op: v.Subscription.OperationId}
	case *wire.Envelope_Progress:
		p.mu.Lock()
		s := p.receiving[e.GetReplyTo()]
		p.mu.Unlock()
		if s == nil {
			return nil
		}
		req := &wire.CapabilityRequest{MessageId: s.id, Namespace: p.identity.Namespace, Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: s.op}}
		if !validAnswer(req, v.Progress, e) {
			return failure(authorization.Invalid)
		}
		select {
		case s.updates <- v.Progress:
		default:
			select {
			case <-s.updates:
			default:
			}
			select {
			case s.updates <- v.Progress:
			default:
			}
		}
	case *wire.Envelope_Control:
		if v.Control.Kind == "PING" {
			id, err := randomid.New()
			if err != nil {
				return err
			}
			return p.enqueue(p.control, &wire.Envelope{MessageId: id, ReplyTo: &e.MessageId, Body: &wire.Envelope_Control{Control: &wire.WSControl{Kind: "PONG"}}})
		}
		if v.Control.Kind != "PONG" {
			return failure(authorization.Unsupported)
		}
	case *wire.Envelope_Extension:
		if p.host.Schema == nil {
			return failure(authorization.Unsupported)
		}
		if err := p.host.Schema.Validate(v.Extension); err != nil {
			return failure(authorization.Invalid)
		}
		return failure(authorization.Unsupported) // No extension behavior advertised.
	default:
		return failure(authorization.Unsupported)
	}
	return nil
}
func (p *Peer) answer(ctx context.Context, r *wire.CapabilityRequest) *wire.CapabilityResponse {
	out := new(wire.CapabilityResponse)
	var err error
	if method(r) == "" || !slices.Contains(p.capabilities, method(r)) {
		err = failure(authorization.Unsupported)
	} else {
		var b *Binding
		b, err = p.current(ctx)
		if err == nil {
			out, err = b.query(ctx, p.identity, r)
		}
	}
	if err != nil {
		out = &wire.CapabilityResponse{Body: &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: code(err)}}}
	}
	out.MessageId, _ = randomid.New()
	out.ReplyTo = r.MessageId
	out.Namespace = r.Namespace
	out.Evidence = "durable_capability_execution"
	return out
}
func (p *Peer) writeLoop() {
	if slices.Contains(p.capabilities, "reliable.v1") {
		ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
		b, err := p.reliableJournal(ctx)
		var cursor *wire.WSCursor
		if err == nil {
			cursor, err = b.Journal.Cursor(ctx)
		}
		cancel()
		if err == nil {
			err = p.send(&wire.Envelope{Body: &wire.Envelope_Cursor{Cursor: cursor}})
		}
		if err != nil {
			p.stop(err)
			return
		}
	}
	poll := time.NewTicker(p.config.Poll)
	defer poll.Stop()
	heartbeat := time.NewTicker(p.config.Timeout / 3)
	defer heartbeat.Stop()
	for {
		var e *wire.Envelope
		var disclosure *wire.CapabilityRequest
		// At most one control before each data selection; neither class can starve.
		select {
		case e = <-p.control:
			if err := p.send(e); err != nil {
				p.stop(err)
				return
			}
		default:
		}
		select {
		case <-p.ctx.Done():
			return
		case e = <-p.control:
		case e = <-p.outgoing:
		case req := <-p.incoming:
			ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
			out := p.answer(ctx, req.GetQuery())
			if out.GetFailure() == nil {
				disclosure = req.GetQuery()
			}
			cancel()
			e = &wire.Envelope{MessageId: out.MessageId, ReplyTo: &req.MessageId, Body: &wire.Envelope_Answer{Answer: out}}
		case <-heartbeat.C:
			e = &wire.Envelope{Body: &wire.Envelope_Control{Control: &wire.WSControl{Kind: "PING"}}}
		case <-poll.C:
			if err := p.recoverReliable(); err != nil {
				p.stop(err)
				return
			}
			if err := p.publish(); err != nil {
				p.stop(err)
				return
			}
			continue
		}
		if _, err := p.current(p.ctx); err != nil {
			p.stop(err)
			return
		}
		if err := p.sendChecked(e, disclosure); err != nil {
			p.stop(err)
			return
		}
	}
}
func (p *Peer) publish() error {
	p.mu.Lock()
	copies := make(map[string]subscription, len(p.publishing))
	for k, v := range p.publishing {
		copies[k] = v
	}
	p.mu.Unlock()
	for id, sub := range copies {
		r := &wire.CapabilityRequest{MessageId: id, Namespace: p.identity.Namespace, Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: sub.op}}
		ctx, cancel := context.WithTimeout(p.ctx, p.config.Timeout)
		out := p.answer(ctx, r)
		cancel()
		// Ignore fresh response IDs when deciding whether the view changed.
		comparable := proto.Clone(out).(*wire.CapabilityResponse)
		comparable.MessageId = ""
		if proto.Equal(sub.last, comparable) {
			continue
		}
		p.mu.Lock()
		_, active := p.publishing[id]
		if active {
			p.publishing[id] = subscription{op: sub.op, last: comparable}
		}
		p.mu.Unlock()
		if !active {
			continue
		}
		var guard *wire.CapabilityRequest
		if out.GetFailure() == nil {
			guard = r
		}
		if err := p.sendChecked(&wire.Envelope{MessageId: out.MessageId, ReplyTo: &id, Body: &wire.Envelope_Progress{Progress: out}}, guard); err != nil {
			return err
		}
		if out.GetFailure() != nil {
			p.mu.Lock()
			delete(p.publishing, id)
			p.mu.Unlock()
		}
		select {
		case ctl := <-p.control:
			if err := p.send(ctl); err != nil {
				return err
			}
		default:
		}
	}
	return nil
}

func validAnswer(r *wire.CapabilityRequest, out *wire.CapabilityResponse, e *wire.Envelope) bool {
	if out == nil || out.MessageId != e.MessageId || out.ReplyTo != e.GetReplyTo() || out.ReplyTo != r.MessageId || out.Namespace != r.Namespace || out.Evidence != "durable_capability_execution" {
		return false
	}
	if f := out.GetFailure(); f != nil {
		return slices.Contains([]string{"UNAUTHENTICATED", "PERMISSION_DENIED", "INVALID_ARGUMENT", "UNSUPPORTED", "VERSION_CONFLICT", "IDENTITY_CONFLICT", "ADMISSION_EXPIRED", "NOT_FOUND", "TIME_UNTRUSTED", "UNAVAILABLE", "OUTCOME_UNKNOWN"}, f.Code)
	}
	switch r.Body.(type) {
	case *wire.CapabilityRequest_Invoke:
		return out.GetReceipt() != nil && out.GetReceipt().OperationId == r.GetInvoke().GetInvocation().GetOperationId() && out.GetReceipt().Revision > 0 && out.GetReceipt().Revision <= 32
	case *wire.CapabilityRequest_List, *wire.CapabilityRequest_Search:
		return out.GetCatalogPage() != nil
	case *wire.CapabilityRequest_Describe:
		return proto.Equal(out.GetCatalogDeclaration().GetRef(), r.GetDescribe())
	case *wire.CapabilityRequest_GetInvocation:
		return out.GetSnapshot() != nil && execution.ValidRecord(executionwire.Record(out.GetSnapshot())) && out.GetSnapshot().GetInvocation().GetOperationId() == r.GetGetInvocation() && out.GetSnapshot().GetInvocation().GetQualification().GetTask().GetNamespace() == r.Namespace
	}
	return false
}
