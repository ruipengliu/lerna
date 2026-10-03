package endpointchannel

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	harness "github.com/ruipengliu/lerna/sdk/go"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type packet struct {
	raw     []byte
	control bool
	pending *pending
}
type flow struct {
	connection                                  *Connection
	id                                          string
	revision                                    uint64
	application                                 Application
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	conn                                        *grpcgo.ClientConn
	stream                                      grpcgo.BidiStreamingClient[rpcv1.ChannelFrame, rpcv1.ChannelFrame]
	mu                                          sync.Mutex
	closed                                      bool
	queuedBytes, normalBytes, ordinary, control int
	normalQueue                                 chan packet
	controlQueue                                chan packet
	workers                                     sync.WaitGroup
	closeOnce                                   sync.Once
	reapOnce                                    sync.Once
}

func (c *Connection) connect(ctx context.Context) error {
	var last error
	for range len(c.router.cfg.Applications) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.router.cfg.Identity.CheckCurrent(ctx, c.auth); err != nil {
			return err
		}
		token, err := c.router.cfg.Credentials(ctx, c.auth)
		if err != nil {
			return err
		}
		if token == "" || len(token) > 4089 {
			return api.E("forbidden", "original_endpoint_credential_required")
		}
		c.mu.Lock()
		if c.ctx.Err() != nil {
			c.mu.Unlock()
			return c.ctx.Err()
		}
		app := c.router.cfg.Applications[c.nextApplication%len(c.router.cfg.Applications)]
		c.nextApplication++
		c.candidateRevision++
		revision := c.candidateRevision
		c.mu.Unlock()
		candidateCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		f, err := c.candidate(candidateCtx, app, token, revision)
		stop()
		if err != nil {
			last = err
			continue
		}
		c.mu.Lock()
		if c.ctx.Err() != nil {
			c.mu.Unlock()
			f.close()
			return c.ctx.Err()
		}
		old := c.active
		c.active = f
		c.flows = append(c.flows, f)
		c.mu.Unlock()
		f.workers.Add(2)
		go f.readLoop()
		go f.writeLoop()
		if old != nil {
			c.lose(old)
		}
		c.recoverOriginals(f)
		return nil
	}
	if last == nil {
		last = originalUnavailable("application_channel_unavailable")
	}
	return last
}
func (c *Connection) candidate(setup context.Context, app Application, token string, revision uint64) (*flow, error) {
	u, err := url.Parse(app.Address)
	if err != nil {
		return nil, err
	}
	conn, err := grpcgo.NewClient(u.Host, grpcgo.WithTransportCredentials(credentials.NewTLS(app.ClientTLS.Clone())), grpcgo.WithDisableRetry(), grpcgo.WithDisableServiceConfig(), grpcgo.WithDefaultCallOptions(grpcgo.ForceCodec(grpcwire.Codec{}), grpcgo.MaxCallRecvMsgSize(grpcwire.MaxFrameBytes), grpcgo.MaxCallSendMsgSize(grpcwire.MaxFrameBytes)))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(c.ctx)
	f := &flow{connection: c, id: api.NewID("binding"), revision: revision, application: app, ctx: ctx, cancel: cancel, conn: conn, normalQueue: make(chan packet, 28), controlQueue: make(chan packet, 4)}
	stopSetup := context.AfterFunc(setup, func() { cancel(); conn.Close() })
	defer stopSetup()
	streamCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	f.stream, err = rpcv1.NewHarnessServiceClient(conn).EndpointChannel(streamCtx)
	if err != nil {
		f.close()
		return nil, err
	}
	bind := grpcwire.Bind{Type: "bind", LogicalServiceID: c.router.cfg.OwnerID, ConnectionID: c.info.ConnectionID, GatewayInstanceID: c.router.cfg.GatewayInstanceID, EndpointID: c.registration.EndpointID, InstanceID: c.registration.InstanceID, EndpointGeneration: c.registration.Generation, Protocol: api.Protocol, Profile: api.Profile, TransportProfile: grpcwire.EndpointProfile, MethodsDigest: c.router.cfg.MethodsDigest}
	if err = f.stream.Send(&rpcv1.ChannelFrame{BindingId: f.id, BindingRevision: f.revision, FrameJson: api.Raw(bind)}); err != nil {
		f.close()
		return nil, err
	}
	frame, err := f.stream.Recv()
	if err != nil {
		f.close()
		return nil, err
	}
	decoded, err := grpcwire.DecodeFrame(frame.FrameJson)
	ready, ok := decoded.(*grpcwire.Ready)
	bind.Type = "ready"
	if err != nil || !ok || frame.BindingId != f.id || frame.BindingRevision != f.revision || !api.Equal(ready.Bind, bind) || !api.Equal(ready.Limits, grpcwire.Limits()) {
		f.close()
		return nil, api.E("unsupported", "replacement_channel_ready_mismatch")
	}
	if err = setup.Err(); err != nil {
		f.close()
		return nil, err
	}
	return f, nil
}
func (f *flow) enqueue(body any, control bool, p *pending) error {
	raw := api.Raw(body)
	if _, err := grpcwire.DecodeFrame(raw); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	limit := 3 << 20
	if control {
		limit = 4 << 20
	}
	if f.closed || f.ctx.Err() != nil {
		return originalUnavailable("channel_binding_not_current")
	}
	if f.queuedBytes+len(raw) > limit || !control && (f.ordinary >= 28 || f.normalBytes+len(raw) > 3<<20) || control && f.control >= 4 {
		return api.E("overloaded", "endpoint_channel_queue_limit")
	}
	if !f.connection.router.reserveBytes(f.connection.auth.TenantID, len(raw)) {
		return api.E("overloaded", "endpoint_channel_global_queue_limit")
	}
	item := packet{raw: raw, control: control, pending: p}
	queue := f.normalQueue
	if control {
		queue = f.controlQueue
		f.control++
	} else {
		f.ordinary++
		f.normalBytes += len(raw)
	}
	f.queuedBytes += len(raw)
	select {
	case queue <- item:
		return nil
	default:
		f.releaseLocked(item)
		return api.E("overloaded", "endpoint_channel_queue_items")
	}
}
func (f *flow) releaseLocked(p packet) {
	f.queuedBytes -= len(p.raw)
	if p.control {
		f.control--
	} else {
		f.ordinary--
		f.normalBytes -= len(p.raw)
	}
	f.connection.router.releaseBytes(f.connection.auth.TenantID, len(p.raw))
}
func (f *flow) release(p packet) { f.mu.Lock(); f.releaseLocked(p); f.mu.Unlock() }
func (f *flow) drain() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for {
		select {
		case p := <-f.normalQueue:
			f.releaseLocked(p)
		case p := <-f.controlQueue:
			f.releaseLocked(p)
		default:
			return
		}
	}
}
func (f *flow) writeLoop() {
	defer f.workers.Done()
	defer f.drain()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	var ordinary, control *packet
	defer func() {
		if ordinary != nil {
			f.release(*ordinary)
		}
		if control != nil {
			f.release(*control)
		}
	}()
	for {
		if ordinary == nil {
			select {
			case p := <-f.normalQueue:
				ordinary = &p
			default:
			}
		}
		if control == nil {
			select {
			case p := <-f.controlQueue:
				control = &p
			default:
			}
		}
		if ordinary == nil && control == nil {
			select {
			case <-f.ctx.Done():
				return
			case p := <-f.normalQueue:
				ordinary = &p
			case p := <-f.controlQueue:
				control = &p
			case <-heartbeat.C:
				if err := f.enqueue(grpcwire.Heartbeat{Type: "ping", Nonce: api.NewID("ping")}, true, nil); err != nil {
					f.connection.lose(f)
					return
				}
				continue
			}
			continue
		}
		// Reply/heartbeat control可优先；有序request仍按原外seq进入唯一出口。
		chosen := ordinary
		fromControl := false
		if control != nil && (ordinary == nil || control.pending == nil || ordinary.pending != nil && control.pending.seq < ordinary.pending.seq) {
			chosen = control
			fromControl = true
		}
		if fromControl {
			control = nil
		} else {
			ordinary = nil
		}
		if chosen.pending != nil && chosen.pending.ctx.Err() != nil {
			f.release(*chosen)
			continue
		}
		if err := f.connection.router.cfg.Identity.CheckCurrent(f.ctx, f.connection.auth); err != nil {
			f.release(*chosen)
			f.connection.info.Stop()
			return
		}
		f.connection.mu.Lock()
		current := f.connection.active == f
		if current && chosen.pending != nil {
			chosen.pending.sent = true
		}
		f.connection.mu.Unlock()
		if !current {
			f.release(*chosen)
			return
		}
		timer := time.AfterFunc(5*time.Second, func() { f.connection.lose(f); f.conn.Close() })
		err := f.stream.Send(&rpcv1.ChannelFrame{BindingId: f.id, BindingRevision: f.revision, FrameJson: chosen.raw})
		timer.Stop()
		f.release(*chosen)
		if err != nil {
			f.connection.lose(f)
			return
		}
	}
}
func (f *flow) readLoop() {
	defer f.workers.Done()
	for {
		frame, err := f.stream.Recv()
		if err != nil {
			f.connection.lose(f)
			return
		}
		f.connection.mu.Lock()
		current := f.connection.active == f
		if !current {
			f.connection.discarded++
		}
		f.connection.mu.Unlock()
		if !current {
			continue
		}
		if frame.BindingId != f.id || frame.BindingRevision != f.revision {
			f.connection.lose(f)
			return
		}
		decoded, err := grpcwire.DecodeFrame(frame.FrameJson)
		if err != nil {
			f.connection.lose(f)
			return
		}
		ctx, stop := context.WithTimeout(f.ctx, 5*time.Second)
		if err = f.connection.router.cfg.Identity.CheckCurrent(ctx, f.connection.auth); err != nil {
			stop()
			f.connection.info.Stop()
			return
		}
		switch body := decoded.(type) {
		case *grpcwire.Response:
			err = f.response(*body)
		case *grpcwire.Heartbeat:
			if body.Type == "ping" {
				body.Type = "pong"
				err = f.enqueue(*body, true, nil)
			}
		case *grpcwire.Delivery:
			err = f.delivery(ctx, *body)
		case *grpcwire.ReplyAck:
			err = f.ack(*body)
		default:
			err = api.E("invalid_request", "unexpected_application_channel_frame")
		}
		stop()
		if err != nil {
			f.connection.lose(f)
			return
		}
	}
}
func (f *flow) response(response grpcwire.Response) error {
	c := f.connection
	c.mu.Lock()
	p := c.pending[response.RequestSeq]
	if p == nil {
		c.mu.Unlock()
		return nil
	}
	if c.active != f || p.flow != f {
		c.discarded++
		c.mu.Unlock()
		return nil
	}
	delete(c.pending, response.RequestSeq)
	p.completedBy = f
	c.mu.Unlock()
	if p.ctx.Err() != nil {
		return nil
	}
	if response.ResultKind != "error" && (p.kind == "query" && response.ResultKind != "query_result" || p.kind != "query" && response.ResultKind != "receipt") {
		return api.E("invalid_request", "original_response_kind_mismatch")
	}
	if response.ResultKind == "error" {
		var problem api.Error
		if err := api.Decode(response.Payload, &problem); err != nil {
			return err
		}
		p.complete(result{err: &problem})
	} else {
		p.complete(result{kind: response.ResultKind, body: response.Payload})
	}
	return nil
}
func (f *flow) delivery(ctx context.Context, d grpcwire.Delivery) error {
	c := f.connection
	if c.router.cfg.DeliveryProofs == nil {
		return api.E("unsupported", "endpoint_delivery_proof_unconfigured")
	}
	if err := c.router.cfg.DeliveryProofs.VerifyDelivery(ctx, c.registration, d); err != nil {
		return err
	}
	c.mu.Lock()
	if c.active != f {
		c.discarded++
		c.mu.Unlock()
		return nil
	}
	old, exists := c.deliveries[d.DeliveryID]
	if exists && !api.Equal(old.original, d) {
		c.mu.Unlock()
		return api.E("idempotency_conflict", "original_delivery_changed")
	}
	if !exists {
		if len(c.deliveries) >= 32 {
			c.mu.Unlock()
			return api.E("overloaded", "endpoint_delivery_pending_limit")
		}
		c.deliveries[d.DeliveryID] = delivery{original: d}
	}
	c.mu.Unlock()
	if exists && old.reply != nil {
		return f.enqueue(*old.reply, true, nil)
	}
	return c.info.EmitChecked(api.Raw(d), false, f.beginDisclosure)
}
func (f *flow) ack(ack grpcwire.ReplyAck) error {
	c := f.connection
	c.mu.Lock()
	if c.active != f {
		c.discarded++
		c.mu.Unlock()
		return nil
	}
	saved, exists := c.deliveries[ack.DeliveryID]
	if !exists || saved.reply == nil || ack.RequestDigest != saved.reply.RequestDigest || !ack.Stored {
		c.mu.Unlock()
		return api.E("invalid_request", "original_reply_ack_mismatch")
	}
	delete(c.deliveries, ack.DeliveryID)
	c.mu.Unlock()
	return c.info.EmitChecked(api.Raw(ack), true, f.beginDisclosure)
}
func (f *flow) beginDisclosure(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := f.connection
	c.mu.Lock()
	if c.active != f {
		c.discarded++
		c.mu.Unlock()
		return nil, api.E("revision_conflict", "old_binding_output_discarded")
	}
	return c.mu.Unlock, nil
}
func (c *Connection) recoverOriginals(f *flow) {
	c.mu.Lock()
	requests := []*pending{}
	replies := []grpcwire.Reply{}
	for _, p := range c.pending {
		if p.flow != f {
			requests = append(requests, p)
		}
	}
	for _, d := range c.deliveries {
		if d.reply != nil {
			replies = append(replies, *d.reply)
		}
	}
	c.mu.Unlock()
	for _, reply := range replies {
		if err := f.enqueue(reply, true, nil); err != nil {
			c.lose(f)
			return
		}
	}
	for _, p := range requests {
		c.workers.Add(1)
		go func(p *pending) { defer c.workers.Done(); c.recoverOriginal(f, p) }(p)
	}
}
func (c *Connection) recoverOriginal(f *flow, p *pending) {
	c.mu.Lock()
	sent := p.sent
	current := c.active == f
	c.mu.Unlock()
	if !current {
		return
	}
	if !sent {
		c.remove(p)
		p.complete(result{err: originalUnavailable("original_channel_request_not_confirmed")})
		return
	}
	kind, payload := p.kind, p.payload
	if kind == "command" {
		var original api.Command
		if err := api.Decode(payload, &original); err != nil {
			c.remove(p)
			p.complete(result{err: err})
			return
		}
		kind = "receipt_lookup"
		payload = api.Raw(api.ReceiptLookup{LogicalServiceID: original.LogicalServiceID, CommandID: original.CommandID})
	}
	for p.ctx.Err() == nil {
		if err := c.router.cfg.Identity.CheckCurrent(p.ctx, c.auth); err != nil {
			c.remove(p)
			p.complete(result{err: err})
			return
		}
		token, err := c.router.cfg.Credentials(p.ctx, c.auth)
		if err != nil {
			c.remove(p)
			p.complete(result{err: err})
			return
		}
		manifest := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.router.cfg.OwnerID, SchemaDigest: api.CoreDigest(), IdentityScope: c.auth.TenantID + "/" + c.auth.SubjectID, IdentityRevision: c.auth.CredentialGeneration, Methods: []api.MethodContract{}}
		manifest.MethodsDigest, _ = api.Digest(manifest.Methods)
		conn, err := harness.DialGRPC(p.ctx, f.application.Address, token, manifest, f.application.ClientTLS, false)
		var raw json.RawMessage
		if err == nil {
			raw, err = conn.Call(p.ctx, kind, payload)
			conn.Close()
		}
		if err == nil {
			c.mu.Lock()
			current = c.active == f
			if current && c.pending[p.seq] == p {
				delete(c.pending, p.seq)
				p.completedBy = f
			}
			c.mu.Unlock()
			if current {
				resultKind := "receipt"
				if kind == "query" {
					resultKind = "query_result"
				}
				p.complete(result{kind: resultKind, body: raw})
			}
			return
		}
		if !api.IsCode(err, "not_found") && !api.IsCode(err, "dependency_unavailable") {
			c.remove(p)
			p.complete(result{err: err})
			return
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-timer.C:
		case <-p.ctx.Done():
			timer.Stop()
		}
		c.mu.Lock()
		current = c.active == f
		c.mu.Unlock()
		if !current {
			return
		}
	}
	c.remove(p)
	p.complete(result{err: originalUnavailable("original_request_wait_expired")})
}
func (f *flow) close() {
	f.closeOnce.Do(func() { f.cancel(); f.conn.Close(); f.workers.Wait(); f.drain() })
}
