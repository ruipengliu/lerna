package endpointchannel

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type runtimeAuth = runtime.Auth
type result struct {
	kind string
	body json.RawMessage
	err  error
}
type pending struct {
	ctx         context.Context
	seq         uint64
	kind        string
	payload     json.RawMessage
	flow        *flow
	result      chan result
	done        chan struct{}
	once        sync.Once
	sent        bool
	completedBy *flow
}

func (p *pending) BeginDisclosure(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := p.flow.connection
	c.mu.Lock()
	if p.completedBy != nil && c.active != p.completedBy {
		c.mu.Unlock()
		return nil, api.E("revision_conflict", "old_binding_output_discarded")
	}
	return c.mu.Unlock, nil
}

func (p *pending) complete(r result) { p.once.Do(func() { p.result <- r; close(p.done) }) }
func (p *pending) Wait(ctx context.Context) (string, json.RawMessage, error) {
	select {
	case r := <-p.result:
		return r.kind, r.body, r.err
	case <-ctx.Done():
		return "", nil, originalUnavailable("original_request_wait_expired")
	}
}

type delivery struct {
	original grpcwire.Delivery
	reply    *grpcwire.Reply
}
type Connection struct {
	router            *Router
	auth              runtime.Auth
	registration      transport.EndpointRegistration
	info              wss.ConnectionInfo
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	active            *flow
	flows             []*flow
	candidateRevision uint64
	nextApplication   int
	lastRequestSeq    uint64
	pending           map[uint64]*pending
	deliveries        map[string]delivery
	lost              chan struct{}
	workers           sync.WaitGroup
	closeOnce         sync.Once
	discarded         uint64
}

func newConnection(r *Router, ctx context.Context, a runtime.Auth, reg transport.EndpointRegistration, info wss.ConnectionInfo) *Connection {
	ctx, cancel := context.WithCancel(ctx)
	return &Connection{router: r, auth: a, registration: reg, info: info, ctx: ctx, cancel: cancel, pending: map[uint64]*pending{}, deliveries: map[string]delivery{}, lost: make(chan struct{}, 1)}
}
func originalUnavailable(reason string) *api.Error {
	return &api.Error{Code: "dependency_unavailable", Scope: "transport", Reason: reason, Retry: "query_original"}
}
func (c *Connection) Call(context.Context, runtime.Auth, string, json.RawMessage) (string, json.RawMessage, error) {
	return "", nil, api.E("unsupported", "original_connection_sequence_required")
}
func (c *Connection) Begin(ctx context.Context, a runtime.Auth, seq uint64, kind string, payload json.RawMessage) (wss.PendingResponse, error) {
	if !api.Equal(a, c.auth) || seq == 0 || seq > api.MaxSafeInteger || kind != "command" && kind != "query" && kind != "receipt_lookup" {
		return nil, api.E("forbidden", "original_connection_identity_or_kind_changed")
	}
	if err := c.router.cfg.Identity.CheckCurrent(ctx, a); err != nil {
		c.info.Stop()
		return nil, err
	}
	if err := originalPayload(c.router.cfg.OwnerID, kind, payload); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if seq <= c.lastRequestSeq {
		c.mu.Unlock()
		return nil, api.E("invalid_request", "external_request_sequence_reused")
	}
	c.lastRequestSeq = seq
	f := c.active
	if f == nil || c.ctx.Err() != nil {
		c.mu.Unlock()
		return nil, originalUnavailable("endpoint_channel_rebinding")
	}
	p := &pending{ctx: ctx, seq: seq, kind: kind, payload: append(json.RawMessage{}, payload...), flow: f, result: make(chan result, 1), done: make(chan struct{})}
	c.pending[seq] = p
	c.mu.Unlock()
	if err := f.enqueue(grpcwire.Request{Type: "request", RequestSeq: seq, Kind: kind, Payload: p.payload}, isPriority(kind, payload), p); err != nil {
		c.remove(p)
		return nil, err
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		select {
		case <-ctx.Done():
			c.remove(p)
			p.complete(result{err: originalUnavailable("original_request_wait_expired")})
		case <-c.ctx.Done():
			c.remove(p)
			p.complete(result{err: originalUnavailable("external_connection_closed")})
		case <-p.done:
		}
	}()
	return p, nil
}

func (c *Connection) remove(p *pending) {
	c.mu.Lock()
	if c.pending[p.seq] == p {
		delete(c.pending, p.seq)
	}
	c.mu.Unlock()
}
func originalPayload(owner, kind string, b []byte) error {
	v, err := api.ParseJSON(b)
	if err != nil {
		return err
	}
	o, ok := v.(map[string]any)
	if !ok || o["logical_service_id"] != owner {
		return api.E("invalid_request", "wrong_logical_service")
	}
	if kind != "receipt_lookup" && (o["protocol"] != api.Protocol || o["profile"] != api.Profile) {
		return api.E("unsupported", "profile_not_supported")
	}
	return nil
}
func isPriority(kind string, b []byte) bool {
	if kind == "receipt_lookup" {
		return true
	}
	if kind != "command" {
		return false
	}
	v, err := api.ParseJSON(b)
	if err != nil {
		return false
	}
	o, ok := v.(map[string]any)
	if !ok {
		return false
	}
	m, _ := o["method"].(string)
	return api.IsControlMethod(m)
}
func (c *Connection) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := State{ConnectionID: c.info.ConnectionID, EndpointID: c.registration.EndpointID, EndpointInstanceID: c.registration.InstanceID, EndpointGeneration: c.registration.Generation, GatewayInstanceID: c.router.cfg.GatewayInstanceID, MethodsDigest: c.router.cfg.MethodsDigest, BindingRevision: c.candidateRevision, LastRequestSeq: c.lastRequestSeq, Pending: uint64(len(c.pending)), DiscardedOldOutputs: c.discarded}
	if c.active != nil {
		s.BindingID = c.active.id
		s.BindingRevision = c.active.revision
		s.Connected = true
	}
	return s
}
func (c *Connection) lose(f *flow) {
	c.mu.Lock()
	if c.active == f {
		c.active = nil
		select {
		case c.lost <- struct{}{}:
		default:
		}
	}
	c.mu.Unlock()
	f.cancel()
	f.reapOnce.Do(func() {
		c.workers.Add(1)
		go func() {
			defer c.workers.Done()
			f.close()
			c.mu.Lock()
			for i, old := range c.flows {
				if old == f {
					c.flows = append(c.flows[:i], c.flows[i+1:]...)
					break
				}
			}
			c.mu.Unlock()
		}()
	})
}
func (c *Connection) rebindLoop() {
	defer c.workers.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.lost:
		}
		deadline := time.Now().Add(60 * time.Second)
		for c.ctx.Err() == nil {
			if err := c.router.cfg.Identity.CheckCurrent(c.ctx, c.auth); err != nil {
				c.info.Stop()
				return
			}
			setup, stop := context.WithTimeout(c.ctx, 5*time.Second)
			err := c.connect(setup)
			stop()
			if err == nil {
				break
			}
			if !time.Now().Before(deadline) {
				c.info.Stop()
				return
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-c.ctx.Done():
				timer.Stop()
				return
			}
		}
	}
}
func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		c.mu.Lock()
		flows := append([]*flow{}, c.flows...)
		for _, p := range c.pending {
			p.complete(result{err: originalUnavailable("external_connection_closed")})
		}
		c.pending = map[uint64]*pending{}
		c.mu.Unlock()
		for _, f := range flows {
			f.close()
		}
		c.workers.Wait()
		c.router.mu.Lock()
		delete(c.router.connections, c.info.ConnectionID)
		key := c.auth.TenantID + "/" + c.auth.SubjectID
		c.router.identityCounts[key]--
		if c.router.identityCounts[key] == 0 {
			delete(c.router.identityCounts, key)
		}
		c.router.mu.Unlock()
	})
	return nil
}
func (c *Connection) Receive(ctx context.Context, raw json.RawMessage) error {
	frame, err := grpcwire.DecodeFrame(raw)
	if err != nil {
		return err
	}
	reply, ok := frame.(*grpcwire.Reply)
	if !ok {
		return api.E("unsupported", "external_endpoint_frame_not_supported")
	}
	if err = c.router.cfg.Identity.CheckCurrent(ctx, c.auth); err != nil {
		return err
	}
	c.mu.Lock()
	saved, ok := c.deliveries[reply.DeliveryID]
	f := c.active
	if !ok {
		c.mu.Unlock()
		return api.E("forbidden", "original_delivery_required")
	}
	if err = grpcwire.ValidateReply(saved.original, *reply); err != nil {
		c.mu.Unlock()
		return err
	}
	if saved.reply != nil && !api.Equal(*saved.reply, *reply) {
		c.mu.Unlock()
		return api.E("idempotency_conflict", "original_endpoint_reply_changed")
	}
	copy := *reply
	saved.reply = &copy
	c.deliveries[reply.DeliveryID] = saved
	c.mu.Unlock()
	if f == nil {
		return nil
	}
	return f.enqueue(*reply, true, nil)
}

var _ wss.ConnectionProcessor = (*Router)(nil)
var _ wss.SequentialConnection = (*Connection)(nil)
var _ wss.InboundConnection = (*Connection)(nil)
