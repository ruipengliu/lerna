package wsbinding

import (
	"context"
	"crypto/tls"
	"github.com/gorilla/websocket"
	"golang.org/x/net/netutil"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/nodetls"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/schema"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

var supported = []string{"catalog.read.v1", "invocation.read.v1", "progress.v1", "chunks.v1", "reliable.v1", "authorization.sync.v1"}

const subprotocol = "harness.bootstrap.v1"

// Host may accept or dial. Both directions use the same peer/session machinery.
type Host struct {
	Endpoint           *nodetls.Endpoint
	Config             Config
	Subject            string
	Required, Optional []string
	Resolve            Resolve
	Schema             *schema.Registry
}

func (h Host) valid() error {
	if h.Endpoint == nil || h.Resolve == nil || len(h.Subject) == 0 || len(h.Subject) > 128 || len(h.Required) > 16 || len(h.Optional) > 16 {
		return failure(authorization.Invalid)
	}
	return h.Config.Validate()
}

type Server struct {
	http     *http.Server
	host     Host
	mu       sync.Mutex
	peers    map[*Peer]bool
	stopping bool
	accepted chan *Peer
}

func NewServer(h Host) (*Server, error) {
	if err := h.valid(); err != nil {
		return nil, err
	}
	s := &Server{host: h, peers: map[*Peer]bool{}, accepted: make(chan *Peer, h.Config.MaxConnections)}
	s.http = &http.Server{Handler: s, ReadHeaderTimeout: h.Config.Timeout, ReadTimeout: h.Config.Timeout, WriteTimeout: h.Config.Timeout, IdleTimeout: h.Config.Timeout, MaxHeaderBytes: 8192, TLSConfig: h.Endpoint.ServerConfig()}
	return s, nil
}
func (s *Server) Serve(l net.Listener) error {
	return s.http.ServeTLS(netutil.LimitListener(l, s.host.Config.MaxConnections), "", "")
}
func (s *Server) Accept(ctx context.Context) (*Peer, error) {
	select {
	case p := <-s.accepted:
		return p, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Server) Close() error {
	s.mu.Lock()
	s.stopping = true
	for p := range s.peers {
		p.Close()
	}
	s.mu.Unlock()
	return s.http.Close()
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.URL.Path != "/harness" || r.Header.Get("Origin") != "" || !slices.Contains(websocket.Subprotocols(r), subprotocol) {
		http.Error(w, "unsupported endpoint", 400)
		return
	}
	if _, err := s.host.Endpoint.Authenticate(r.Context(), *r.TLS, "", true); err != nil {
		http.Error(w, "unauthenticated", 403)
		return
	}
	// Hijacked sockets no longer count against netutil's listener after Close,
	// and remain explicitly owned here until the peer finishes.
	s.mu.Lock()
	full := s.stopping || len(s.peers) >= s.host.Config.MaxConnections
	s.mu.Unlock()
	if full {
		http.Error(w, "capacity", 503)
		return
	}
	u := websocket.Upgrader{HandshakeTimeout: s.host.Config.Timeout, ReadBufferSize: 4096, WriteBufferSize: 4096, Subprotocols: []string{subprotocol}}
	c, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p, err := s.host.start(context.Background(), c, *r.TLS, true, "")
	if err != nil {
		c.Close()
		return
	}
	s.mu.Lock()
	if s.stopping || len(s.peers) >= s.host.Config.MaxConnections {
		s.mu.Unlock()
		p.Close()
		return
	}
	s.peers[p] = true
	s.mu.Unlock()
	defer func() { p.Close(); s.mu.Lock(); delete(s.peers, p); s.mu.Unlock() }()
	select {
	case s.accepted <- p:
	case <-p.ctx.Done():
		return
	default:
		return
	}
	<-p.ctx.Done()
}
func (h Host) Dial(ctx context.Context, address, target string) (*Peer, error) {
	if err := h.valid(); err != nil {
		return nil, err
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "wss" || u.User != nil || u.Path != "/harness" || u.RawQuery != "" {
		return nil, failure(authorization.Invalid)
	}
	cfg, err := h.Endpoint.ClientConfig(target)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{TLSClientConfig: cfg, HandshakeTimeout: h.Config.Timeout, ReadBufferSize: 4096, WriteBufferSize: 4096, Subprotocols: []string{subprotocol}}
	c, resp, err := d.DialContext(ctx, address, nil)
	if err != nil {
		return nil, err
	}
	if resp == nil || c.Subprotocol() != subprotocol {
		c.Close()
		return nil, failure(authorization.Unsupported)
	}
	tc, ok := c.UnderlyingConn().(*tls.Conn)
	if !ok {
		c.Close()
		return nil, failure(authorization.Unauthenticated)
	}
	p, err := h.start(ctx, c, tc.ConnectionState(), false, target)
	if err != nil {
		c.Close()
	}
	return p, err
}
func readEnvelope(c *websocket.Conn, limit int, timeout time.Duration) (*wire.Envelope, error) {
	c.SetReadLimit(int64(limit))
	if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	kind, b, err := c.ReadMessage()
	if err != nil {
		return nil, err
	}
	e := new(wire.Envelope)
	if kind != websocket.BinaryMessage || len(b) > limit || proto.Unmarshal(b, e) != nil || !taskwire.Known(e.ProtoReflect()) {
		return nil, failure(authorization.Invalid)
	}
	return e, nil
}
func writeEnvelope(c *websocket.Conn, e *wire.Envelope, timeout time.Duration) error {
	raw, err := proto.Marshal(e)
	if err != nil {
		return err
	}
	if err = c.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	return c.WriteMessage(websocket.BinaryMessage, raw)
}
func (h Host) start(parent context.Context, c *websocket.Conn, state tls.ConnectionState, server bool, target string) (*Peer, error) {
	// Bootstrap order is asymmetric only to avoid both peers blocking on writes.
	nonce, err := randomid.New()
	if err != nil {
		return nil, err
	}
	local := &wire.WSHello{Bootstrap: 1, Versions: []uint32{1}, Required: h.Required, Optional: h.Optional, Subject: h.Subject, MaxMessage: uint32(h.Config.MaxMessage), Chunk: uint32(h.Config.Chunk), Window: uint32(h.Config.Window), TimeoutMs: uint32(h.Config.Timeout / time.Millisecond), LifetimeMs: uint32(h.Config.Lifetime / time.Millisecond), Nonce: nonce}
	send := func() error {
		return writeEnvelope(c, &wire.Envelope{Body: &wire.Envelope_Hello{Hello: local}}, h.Config.Timeout)
	}
	if !server {
		if err = send(); err != nil {
			return nil, err
		}
	}
	remote, err := readEnvelope(c, 4096, h.Config.Timeout)
	if err != nil {
		return nil, err
	}
	offer := remote.GetHello()
	if offer == nil || proto.Size(remote) != proto.Size(&wire.Envelope{Body: &wire.Envelope_Hello{Hello: offer}}) || offer.Bootstrap != 1 || !slices.Contains(offer.Versions, uint32(1)) || len(offer.Versions) > 8 || len(offer.Required) > 16 || len(offer.Optional) > 16 || len(offer.Nonce) != 64 || len(offer.Subject) == 0 || len(offer.Subject) > 128 {
		return nil, failure(authorization.Invalid)
	}
	rc := h.Config
	rc.MaxMessage = int(offer.MaxMessage)
	rc.Chunk = int(offer.Chunk)
	rc.Window = int(offer.Window)
	rc.Timeout = time.Duration(offer.TimeoutMs) * time.Millisecond
	rc.Lifetime = time.Duration(offer.LifetimeMs) * time.Millisecond
	if err = rc.Validate(); err != nil {
		return nil, err
	}
	selected := []string{}
	for _, name := range append(slices.Clone(local.Required), offer.Required...) {
		if !slices.Contains(supported, name) || !slices.Contains(append(slices.Clone(local.Required), local.Optional...), name) || !slices.Contains(append(slices.Clone(offer.Required), offer.Optional...), name) {
			return nil, failure(authorization.Unsupported)
		}
	}
	for _, name := range supported {
		if slices.Contains(append(slices.Clone(local.Required), local.Optional...), name) && slices.Contains(append(slices.Clone(offer.Required), offer.Optional...), name) {
			selected = append(selected, name)
		}
	}
	if !slices.Contains(selected, "chunks.v1") {
		return nil, failure(authorization.Unsupported)
	}
	if _, err = h.Endpoint.Authenticate(parent, state, target, server); err != nil {
		return nil, err
	}
	// Endpoint certificates have both EKUs. Presentation also checks the subject's
	// current proxy registration in both WebSocket directions.
	presented, err := h.Endpoint.Presentation(parent, state, offer.Subject, "", "")
	if err != nil {
		return nil, err
	}
	b, err := h.Resolve(parent, presented)
	if err != nil {
		return nil, err
	}
	if b == nil || b.Peer != presented || b.Disclose == nil {
		return nil, failure(authorization.Denied)
	}
	if slices.Contains(selected, "reliable.v1") && (b.Journal == nil || b.Execution == nil || b.Retain == nil) {
		return nil, failure(authorization.Unsupported)
	}
	if server {
		if err = send(); err != nil {
			return nil, err
		}
	}
	cfg := h.Config
	cfg.MaxMessage = min(cfg.MaxMessage, rc.MaxMessage)
	cfg.Chunk = min(cfg.Chunk, rc.Chunk)
	cfg.Window = min(cfg.Window, rc.Window)
	cfg.Timeout = min(cfg.Timeout, rc.Timeout)
	cfg.Lifetime = min(cfg.Lifetime, rc.Lifetime)
	welcome := &wire.WSWelcome{PeerNonce: offer.Nonce, Version: 1, Capabilities: selected, MaxMessage: uint32(cfg.MaxMessage), Chunk: uint32(cfg.Chunk), Window: uint32(cfg.Window), TimeoutMs: uint32(cfg.Timeout / time.Millisecond), LifetimeMs: uint32(cfg.Lifetime / time.Millisecond)}
	sendWelcome := func() error {
		return writeEnvelope(c, &wire.Envelope{Body: &wire.Envelope_Welcome{Welcome: welcome}}, cfg.Timeout)
	}
	if !server {
		if err = sendWelcome(); err != nil {
			return nil, err
		}
	}
	got, err := readEnvelope(c, 4096, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	expected := proto.Clone(welcome).(*wire.WSWelcome)
	expected.PeerNonce = nonce
	if !proto.Equal(got, &wire.Envelope{Body: &wire.Envelope_Welcome{Welcome: expected}}) {
		return nil, failure(authorization.Invalid)
	}
	if server {
		if err = sendWelcome(); err != nil {
			return nil, err
		}
	}
	var journal *Journal
	if slices.Contains(selected, "reliable.v1") {
		journal, err = b.Journal.Claim(parent)
		if err != nil {
			return nil, err
		}
	}
	return newPeer(c, h, cfg, state, server, target, presented, selected, journal), nil
}
