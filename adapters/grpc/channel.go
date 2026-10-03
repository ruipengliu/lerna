package grpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	rt "github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type EndpointRegistration struct {
	TenantID             string `json:"tenant_id"`
	SubjectID            string `json:"subject_id"`
	CredentialGeneration uint64 `json:"credential_generation"`
	EndpointID           string `json:"endpoint_id"`
	InstanceID           string `json:"instance_id"`
	Generation           uint64 `json:"generation"`
	RecipientServiceID   string `json:"recipient_service_id"`
}

// EndpointAuthority 由原配对与业务 owner 装配；每一步都在 SQL Tx 外。
type EndpointAuthority interface {
	Bind(context.Context, string, rt.Auth, grpcwire.Bind) (EndpointRegistration, error)
	Check(context.Context, EndpointRegistration) error
	VerifyDelivery(context.Context, EndpointRegistration, grpcwire.Delivery) error
	ReceiveReply(context.Context, EndpointRegistration, grpcwire.Delivery, grpcwire.Reply) (bool, error)
}

// EndpointReplyValidator在冻结首份Reply前检查原recipient输出合同；不得写业务账本。
// 验证运行在SQL Tx外，随后的本库Tx仍核原Delivery和当前binding。
type EndpointReplyValidator interface {
	ValidateReply(context.Context, EndpointRegistration, grpcwire.Delivery, grpcwire.Reply) error
}
type bindingRecord struct {
	Revision              uint64               `json:"revision"`
	Bind                  grpcwire.Bind        `json:"bind"`
	Registration          EndpointRegistration `json:"registration"`
	GatewayPeer           string               `json:"gateway_peer"`
	ApplicationInstanceID string               `json:"application_instance_id"`
	BindingID             string               `json:"binding_id"`
	BindingRevision       uint64               `json:"binding_revision"`
	LastRequestSeq        uint64               `json:"last_request_seq"`
}
type channelGate struct {
	token chan struct{}
	refs  int
}

func newGate() *channelGate {
	g := &channelGate{token: make(chan struct{}, 1)}
	g.token <- struct{}{}
	return g
}
func (g *channelGate) lock(ctx context.Context) error {
	select {
	case <-g.token:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *channelGate) unlock() { g.token <- struct{}{} }

type receivedFrame struct {
	frame *rpcv1.ChannelFrame
	err   error
}
type channelSession struct {
	s                                            *Server
	stream                                       grpcgo.BidiStreamingServer[rpcv1.ChannelFrame, rpcv1.ChannelFrame]
	ctx                                          context.Context
	cancel                                       context.CancelFunc
	auth                                         rt.Auth
	reg                                          EndpointRegistration
	bind                                         bindingRecord
	key                                          string
	gate                                         *channelGate
	rx                                           chan receivedFrame
	errors                                       chan error
	readDone, writeDone                          chan struct{}
	workers                                      sync.WaitGroup
	queue                                        *frameQueue
	lastRead                                     atomic.Int64
	sendStarted                                  atomic.Int64
	lastSend                                     atomic.Int64
	inOrd, inControl, inOrdBytes, inControlBytes int
}

func (c *channelSession) scope() rt.Scope {
	return rt.Scope{TenantID: c.auth.TenantID, OwnerID: c.s.cfg.OwnerID, DatabaseID: c.s.cfg.Store.ID()}
}
func (c *channelSession) stop(err error) {
	select {
	case c.errors <- err:
	default:
	}
	c.cancel()
}
func (s *Server) gateway(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "gateway_identity_missing")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return "", status.Error(codes.Unauthenticated, "gateway_mtls_required")
	}
	for _, uri := range tlsInfo.State.VerifiedChains[0][0].URIs {
		for _, allowed := range s.cfg.GatewayIdentities {
			if uri.String() == allowed {
				return allowed, nil
			}
		}
	}
	return "", status.Error(codes.PermissionDenied, "gateway_identity_unregistered")
}
func (s *Server) reserveChannel(a rt.Auth) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := a.TenantID + "/" + a.SubjectID
	if s.channelCount >= 16 || s.identityChannels[key] >= 2 {
		return false
	}
	s.channelCount++
	s.identityChannels[key]++
	return true
}
func (s *Server) releaseChannel(c *channelSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := c.auth.TenantID + "/" + c.auth.SubjectID
	s.channelCount--
	s.identityChannels[key]--
	if s.identityChannels[key] == 0 {
		delete(s.identityChannels, key)
	}
	if s.channels[c.key] == c {
		delete(s.channels, c.key)
	}
	if c.gate != nil {
		c.gate.refs--
		if c.gate.refs == 0 {
			delete(s.gates, c.key)
		}
	}
}
func (s *Server) EndpointChannel(stream grpcgo.BidiStreamingServer[rpcv1.ChannelFrame, rpcv1.ChannelFrame]) error {
	if s.cfg.EndpointAuthority == nil {
		return status.Error(codes.Unimplemented, "endpoint_authority_not_configured")
	}
	if !s.begin() {
		return status.Error(codes.Unavailable, "grpc_service_closing")
	}
	defer s.owned.Done()
	setup, stopSetup := context.WithTimeout(stream.Context(), 5*time.Second)
	a, e := s.authenticate(setup)
	if e != nil {
		stopSetup()
		return e
	}
	gateway, e := s.gateway(setup)
	if e != nil {
		stopSetup()
		return e
	}
	if !s.reserveChannel(a) {
		stopSetup()
		return status.Error(codes.ResourceExhausted, "endpoint_channel_limit")
	}
	ctx, cancel := context.WithCancel(stream.Context())
	c := &channelSession{s: s, stream: stream, ctx: ctx, cancel: cancel, auth: a, rx: make(chan receivedFrame, 1), errors: make(chan error, 1), readDone: make(chan struct{})}
	s.owned.Add(1)
	go c.readLoop()
	defer func() {
		cancel()
		if c.queue != nil {
			c.queue.close()
		}
		s.owned.Add(1)
		go func() {
			defer s.owned.Done()
			<-c.readDone
			if c.writeDone != nil {
				<-c.writeDone
			}
			c.workers.Wait()
			s.releaseChannel(c)
		}()
	}()
	var opening *rpcv1.ChannelFrame
	select {
	case r := <-c.rx:
		if r.err != nil {
			stopSetup()
			return r.err
		}
		opening = r.frame
	case <-setup.Done():
		stopSetup()
		return status.Error(codes.DeadlineExceeded, "channel_bind_timeout")
	}
	frame, e := grpcwire.DecodeFrame(opening.FrameJson)
	if e != nil {
		stopSetup()
		return status.Error(codes.InvalidArgument, "invalid_channel_binding")
	}
	b, ok := frame.(*grpcwire.Bind)
	if !ok || b.LogicalServiceID != s.cfg.OwnerID || b.MethodsDigest != s.cfg.MethodsDigest {
		stopSetup()
		return status.Error(codes.InvalidArgument, "channel_contract_mismatch")
	}
	c.reg, e = s.cfg.EndpointAuthority.Bind(setup, gateway, a, *b)
	if e != nil {
		stopSetup()
		return status.Error(codes.PermissionDenied, "endpoint_pairing_mismatch")
	}
	if c.reg.TenantID != a.TenantID || c.reg.SubjectID != a.SubjectID || c.reg.CredentialGeneration != a.CredentialGeneration || c.reg.EndpointID != b.EndpointID || c.reg.InstanceID != b.InstanceID || c.reg.Generation != b.EndpointGeneration || !api.ValidID(c.reg.RecipientServiceID) {
		stopSetup()
		return status.Error(codes.PermissionDenied, "endpoint_pairing_mismatch")
	}
	e = s.bindChannel(setup, c, *b, opening.BindingId, opening.BindingRevision, gateway)
	stopSetup()
	if e != nil {
		return status.Error(codes.FailedPrecondition, "channel_binding_rejected")
	}
	c.queue = newFrameQueue(c)
	c.writeDone = make(chan struct{})
	s.owned.Add(1)
	go c.writeLoop()
	readyBind := *b
	readyBind.Type = "ready"
	if e = c.queue.push(grpcwire.Ready{Bind: readyBind, Limits: grpcwire.Limits()}, true); e != nil {
		return e
	}
	if e = c.recoverDeliveries(); e != nil {
		return e
	}
	c.lastRead.Store(time.Now().UnixNano())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case e := <-c.errors:
			if errors.Is(e, io.EOF) {
				return nil
			}
			return e
		case <-c.ctx.Done():
			return status.Error(codes.Aborted, "channel_superseded_or_closed")
		case tick := <-ticker.C:
			if tick.Sub(time.Unix(0, c.lastRead.Load())) >= 90*time.Second {
				return status.Error(codes.DeadlineExceeded, "channel_heartbeat_timeout")
			}
			if since := c.sendStarted.Load(); since != 0 && tick.Sub(time.Unix(0, since)) > 5*time.Second {
				return status.Error(codes.DeadlineExceeded, "channel_send_timeout")
			}
			if tick.Sub(time.Unix(0, c.lastSend.Load())) >= 30*time.Second && c.sendStarted.Load() == 0 {
				if e = c.queue.push(grpcwire.Heartbeat{Type: "ping", Nonce: api.NewID("ping")}, true); e != nil {
					return e
				}
			}
		case received := <-c.rx:
			if received.err != nil {
				if errors.Is(received.err, io.EOF) {
					return nil
				}
				return received.err
			}
			if received.frame.BindingId != c.bind.BindingID || received.frame.BindingRevision != c.bind.BindingRevision {
				return status.Error(codes.InvalidArgument, "channel_binding_mismatch")
			}
			if e = s.cfg.Identity.CheckCurrent(c.ctx, c.auth); e != nil {
				return status.Error(codes.Unauthenticated, "credential_revoked")
			}
			if e = s.cfg.EndpointAuthority.Check(c.ctx, c.reg); e != nil {
				return status.Error(codes.PermissionDenied, "endpoint_pairing_changed")
			}
			decoded, e := grpcwire.DecodeFrame(received.frame.FrameJson)
			if e != nil {
				return status.Error(codes.InvalidArgument, "invalid_channel_frame")
			}
			c.lastRead.Store(time.Now().UnixNano())
			switch f := decoded.(type) {
			case *grpcwire.Request:
				if e = c.request(*f, len(received.frame.FrameJson)); e != nil {
					return status.Error(codes.FailedPrecondition, "channel_request_rejected")
				}
			case *grpcwire.Reply:
				if e = c.reply(*f, len(received.frame.FrameJson)); e != nil {
					return status.Error(codes.FailedPrecondition, "delivery_reply_rejected")
				}
			case *grpcwire.Heartbeat:
				if f.Type == "ping" {
					f.Type = "pong"
					if e = c.queue.push(*f, true); e != nil {
						return e
					}
				}
			default:
				return status.Error(codes.Unimplemented, "channel_frame_not_opened")
			}
		}
	}
}
func (c *channelSession) readLoop() {
	defer c.s.owned.Done()
	defer close(c.readDone)
	for {
		frame, e := c.stream.Recv()
		select {
		case c.rx <- receivedFrame{frame, e}:
		case <-c.ctx.Done():
			return
		}
		if e != nil {
			return
		}
	}
}
func (s *Server) bindChannel(ctx context.Context, c *channelSession, b grpcwire.Bind, id string, revision uint64, gateway string) error {
	if !api.ValidID(id) || revision == 0 || revision > api.MaxSafeInteger {
		return api.E("invalid_request", "invalid_binding_identity")
	}
	c.key = c.auth.TenantID + "/" + b.ConnectionID
	s.mu.Lock()
	g := s.gates[c.key]
	if g == nil {
		g = newGate()
		s.gates[c.key] = g
	}
	g.refs++
	c.gate = g
	s.mu.Unlock()
	if e := g.lock(ctx); e != nil {
		return e
	}
	defer g.unlock()
	var saved bindingRecord
	state, e := s.cfg.Store.Within(ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
		oldRevision, e := tx.Get(ctx, "grpc.bindings", b.ConnectionID, &saved)
		if api.IsCode(e, "not_found") {
			saved = bindingRecord{Revision: 1, Bind: b, Registration: c.reg, GatewayPeer: gateway, ApplicationInstanceID: s.cfg.ApplicationInstanceID, BindingID: id, BindingRevision: revision}
			return tx.Create(ctx, "grpc.bindings", b.ConnectionID, b.EndpointID, saved)
		}
		if e != nil {
			return e
		}
		if !api.Equal(saved.Registration, c.reg) || saved.Bind.GatewayInstanceID != b.GatewayInstanceID || saved.GatewayPeer != gateway {
			return api.E("forbidden", "binding_identity_changed")
		}
		if revision < saved.BindingRevision {
			return api.E("revision_conflict", "binding_stale")
		}
		if revision == saved.BindingRevision {
			if id != saved.BindingID || !api.Equal(saved.Bind, b) || saved.ApplicationInstanceID != s.cfg.ApplicationInstanceID {
				return api.E("idempotency_conflict", "binding_candidate_changed")
			}
			return nil
		}
		saved.Revision = oldRevision + 1
		saved.Bind = b
		saved.BindingID = id
		saved.BindingRevision = revision
		saved.ApplicationInstanceID = s.cfg.ApplicationInstanceID
		return tx.Put(ctx, "grpc.bindings", b.ConnectionID, oldRevision, saved)
	})
	if state == rt.CommitUnknown {
		return rt.ErrCommitUnknown
	}
	if e != nil {
		return e
	}
	c.bind = saved
	s.mu.Lock()
	old := s.channels[c.key]
	s.channels[c.key] = c
	s.mu.Unlock()
	if old != nil && old != c {
		old.stop(status.Error(codes.Aborted, "channel_superseded"))
	}
	return nil
}
func (c *channelSession) current(ctx context.Context) error {
	c.s.mu.Lock()
	active := c.s.channels[c.key] == c
	c.s.mu.Unlock()
	if !active {
		return api.E("revision_conflict", "binding_stale")
	}
	var record bindingRecord
	_, e := c.s.cfg.Store.Read(ctx, c.scope(), "grpc.bindings", c.bind.Bind.ConnectionID, 0, &record)
	if e != nil {
		return e
	}
	return c.checkRecord(record)
}
func (c *channelSession) checkRecord(record bindingRecord) error {
	if record.BindingID != c.bind.BindingID || record.BindingRevision != c.bind.BindingRevision || record.ApplicationInstanceID != c.s.cfg.ApplicationInstanceID || !api.Equal(record.Registration, c.reg) {
		return api.E("revision_conflict", "binding_stale")
	}
	return nil
}
func (c *channelSession) request(r grpcwire.Request, size int) error {
	if e := transportPayload(c.s.cfg.OwnerID, r.Kind, r.Payload); e != nil {
		return e
	}
	control := isControl(r.Kind, r.Payload)
	if e := c.reserveIncoming(control, size); e != nil {
		return e
	}
	state, e := c.s.cfg.Store.Within(c.ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
		var record bindingRecord
		rev, e := tx.Get(c.ctx, "grpc.bindings", c.bind.Bind.ConnectionID, &record)
		if e != nil {
			return e
		}
		if e = c.checkRecord(record); e != nil {
			return e
		}
		if r.RequestSeq <= record.LastRequestSeq {
			return api.E("invalid_request", "request_sequence_reused")
		}
		record.Revision = rev + 1
		record.LastRequestSeq = r.RequestSeq
		return tx.Put(c.ctx, "grpc.bindings", c.bind.Bind.ConnectionID, rev, record)
	})
	if state == rt.CommitUnknown {
		e = rt.ErrCommitUnknown
	}
	if e != nil {
		c.releaseIncoming(control, size)
		return e
	}
	c.workers.Add(1)
	c.s.owned.Add(1)
	go func() {
		defer c.workers.Done()
		defer c.s.owned.Done()
		defer c.releaseIncoming(control, size)
		// 流重绑只终止等待和披露；已进入领域的原命令仍在有限等待内完成原准入。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.stream.Context()), 5*time.Second)
		defer cancel()
		kind, body, e := c.s.cfg.Processor.Call(ctx, c.auth, r.Kind, r.Payload)
		if e != nil {
			kind = "error"
			body = api.Raw(publicError(e))
		}
		response := grpcwire.Response{Type: "response", RequestSeq: r.RequestSeq, ResultKind: kind, Payload: body}
		if e = c.queue.push(response, control); e != nil {
			c.stop(e)
		}
	}()
	return nil
}
