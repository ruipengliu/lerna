// Package wss 实现有界端云帧；连接身份不代替业务命令。
package wss

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type Processor interface {
	Call(context.Context, runtime.Auth, string, json.RawMessage) (string, json.RawMessage, error)
}
type LocalProcessor struct{ Dispatcher *runtime.Dispatcher }

func (p LocalProcessor) Call(ctx context.Context, a runtime.Auth, kind string, payload json.RawMessage) (string, json.RawMessage, error) {
	switch kind {
	case "command":
		r, e := p.Dispatcher.Command(ctx, a, payload)
		return "receipt", api.Raw(r), e
	case "query":
		r, e := p.Dispatcher.Query(ctx, a, payload)
		return "query_result", r, e
	case "receipt_lookup":
		var q api.ReceiptLookup
		if e := api.Decode(payload, &q); e != nil {
			return "", nil, e
		}
		if q.LogicalServiceID != p.Dispatcher.OwnerID {
			return "", nil, api.E("invalid_request", "wrong_logical_service")
		}
		r, e := p.Dispatcher.Lookup(ctx, a, q.CommandID)
		return "receipt", api.Raw(r), e
	default:
		return "", nil, api.E("unsupported", "frame_kind_not_supported")
	}
}

type ContentReader interface {
	ReadBytes(context.Context, runtime.Scope, runtime.Auth, api.ContentRef, string, string) ([]byte, error)
}
type Config struct {
	OwnerID               string
	Store                 runtime.Store
	Registry              *runtime.Registry
	Identity              *platform.DevIdentity
	Processor             Processor
	Content               ContentReader
	Uploader              ContentUploader
	Development           *DevelopmentConfiguration
	Origins               []string
	AllowInsecureLoopback bool
	StaticDir             string
	Location              string
	MaxConnections        int
	MaxQueuedBytes        int
}
type Server struct {
	config      Config
	mux         *http.ServeMux
	connections chan struct{}
	bytesMu     sync.Mutex
	queuedBytes int
	logger      *slog.Logger
}

func New(config Config) (*Server, error) {
	if config.Identity == nil || config.Registry == nil || config.Processor == nil || config.Store == nil || !api.ValidID(config.OwnerID) || config.MaxConnections < 1 || config.MaxConnections > 10000 || config.MaxQueuedBytes < 4<<20 {
		return nil, api.E("invalid_request", "invalid_gateway_configuration")
	}
	s := &Server{config: config, mux: http.NewServeMux(), connections: make(chan struct{}, config.MaxConnections), logger: slog.Default()}
	s.mux.HandleFunc("GET /.well-known/harness", s.wellKnown)
	s.mux.HandleFunc("GET /api/schema/core", s.schema)
	s.mux.HandleFunc("GET /api/discovery", s.discovery)
	s.mux.HandleFunc("POST /auth/session", s.login)
	s.mux.HandleFunc("GET /auth/session", s.currentSession)
	s.mux.HandleFunc("POST /auth/logout", s.logout)
	s.mux.HandleFunc("GET /api/development/config", s.development)
	s.mux.HandleFunc("POST /api/transfers/{transfer_id}", s.upload)
	s.mux.HandleFunc("POST /api/call", s.call)
	s.mux.HandleFunc("GET /api/content", s.content)
	s.mux.HandleFunc("GET /connect", s.connect)
	s.mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, struct {
			Live bool `json:"live"`
		}{true})
	})
	s.mux.HandleFunc("GET /health/ready", s.ready)
	if config.StaticDir != "" {
		s.mux.Handle("GET /", http.FileServer(http.Dir(config.StaticDir)))
	}
	return s, nil
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; connect-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		if !s.config.AllowInsecureLoopback && r.TLS == nil {
			problem(w, api.E("forbidden", "tls_required"))
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(api.Raw(v))
}
func publicError(err error) *api.Error {
	var p *api.Error
	if errors.As(err, &p) {
		copy := *p
		copy.Cause = nil
		return &copy
	}
	return api.E("dependency_unavailable", "internal_dependency_failure")
}
func problem(w http.ResponseWriter, err error) {
	p := publicError(err)
	status := 400
	switch p.Code {
	case "forbidden":
		status = 403
	case "dependency_unavailable", "overloaded":
		status = 503
	case "not_found":
		status = 404
	case "gone":
		status = 410
	}
	write(w, status, p)
}
func (s *Server) wellKnown(w http.ResponseWriter, r *http.Request) {
	write(w, 200, struct {
		Protocol         string `json:"protocol"`
		TransportProfile string `json:"transport_profile"`
		LoginPath        string `json:"login_path"`
		ConnectPath      string `json:"connect_path"`
	}{api.Protocol, "harness-wss/1", "/auth/session", "/connect"})
}
func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Write(api.CoreSchemaBytes())
}
func (s *Server) authenticate(r *http.Request) (runtime.Auth, error) {
	return s.config.Identity.Authenticate(r.Context(), r)
}
func (s *Server) manifest(a runtime.Auth) harness.Discovery {
	methods := s.config.Registry.Contracts()
	digest, _ := api.Digest(methods)
	identity, _ := api.Digest([]string{a.TenantID, a.SubjectID})
	return harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.config.OwnerID, SchemaDigest: api.CoreDigest(), CoreSchemaPath: "/api/schema/core", Methods: methods, MethodsDigest: digest, Limits: harness.Limits{MaxDomainBytes: api.MaxJSONBytes, MaxFrameBytes: 1 << 20, MaxPending: 32}, IdentityScope: identity, IdentityRevision: a.CredentialGeneration}
}
func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	write(w, 200, s.manifest(a))
}
func (s *Server) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	scheme := "https"
	if s.config.AllowInsecureLoopback {
		scheme = "http"
	}
	if origin == scheme+"://"+r.Host {
		return true
	}
	for _, o := range s.config.Origins {
		if origin == o {
			return true
		}
	}
	return false
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil {
		problem(w, api.E("invalid_request", "login_too_large"))
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if e = api.Decode(b, &in); e != nil {
		problem(w, e)
		return
	}
	_, id, csrf, e := s.config.Identity.Login(r.Context(), in.Token)
	if e != nil {
		problem(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "harness_session", Value: id, Path: "/", HttpOnly: true, Secure: !s.config.AllowInsecureLoopback, SameSite: http.SameSiteStrictMode, MaxAge: int(s.config.Identity.SessionTTL.Seconds())})
	write(w, 200, struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{true, csrf})
}
func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	if e = s.config.Identity.CheckCSRF(r.Context(), r, a); e != nil {
		problem(w, e)
		return
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, api.MaxJSONBytes+4096))
	if e != nil {
		problem(w, api.E("invalid_request", "frame_too_large"))
		return
	}
	var in struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	if e = api.DecodeLimit(b, &in, 1<<20); e != nil {
		problem(w, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	kind, result, e := s.config.Processor.Call(ctx, a, in.Kind, in.Payload)
	if e != nil {
		write(w, 200, struct {
			ResultKind string     `json:"result_kind"`
			Payload    *api.Error `json:"payload"`
		}{"error", publicError(e)})
		return
	}
	write(w, 200, struct {
		ResultKind string          `json:"result_kind"`
		Payload    json.RawMessage `json:"payload"`
	}{kind, result})
}
func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if s.config.Content == nil {
		problem(w, api.E("unsupported", "content_transport_not_supported"))
		return
	}
	raw, e := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("ref"))
	if e != nil {
		problem(w, api.E("invalid_request", "invalid_content_ref"))
		return
	}
	var ref api.ContentRef
	if e = api.Decode(raw, &ref); e != nil {
		problem(w, e)
		return
	}
	purpose, location := r.URL.Query().Get("purpose"), r.URL.Query().Get("location")
	if purpose == "" {
		purpose = "read"
	}
	if location == "" {
		location = s.config.Location
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	scope := runtime.Scope{TenantID: a.TenantID, OwnerID: s.config.OwnerID, DatabaseID: s.config.Store.ID()}
	bytes, e := s.config.Content.ReadBytes(ctx, scope, a, ref, purpose, location)
	if e != nil {
		problem(w, e)
		return
	}
	w.Header().Set("Content-Type", ref.MediaType)
	w.Header().Set("X-Harness-Content-Hash", ref.Hash)
	w.Write(bytes)
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if len(s.config.Identity.Principals) == 0 {
		problem(w, api.E("dependency_unavailable", "identity_not_ready"))
		return
	}
	if e := s.config.Identity.CheckCurrent(ctx, s.config.Identity.Principals[0].Auth); e != nil {
		problem(w, e)
		return
	}
	write(w, 200, struct {
		Ready bool `json:"ready"`
	}{true})
}

type queued struct {
	bytes   []byte
	control bool
}

func (s *Server) reserveBytes(n int, control bool) bool {
	s.bytesMu.Lock()
	defer s.bytesMu.Unlock()
	limit := s.config.MaxQueuedBytes
	if !control {
		limit -= 1 << 20
	}
	if n < 0 || s.queuedBytes+n > limit {
		return false
	}
	s.queuedBytes += n
	return true
}
func (s *Server) releaseBytes(n int) { s.bytesMu.Lock(); s.queuedBytes -= n; s.bytesMu.Unlock() }
func isControl(kind string, raw json.RawMessage) bool {
	if kind == "receipt_lookup" {
		return true
	}
	if kind != "command" {
		return false
	}
	var c api.Command
	if api.Decode(raw, &c) != nil {
		return false
	}
	for _, suffix := range []string{".cancel", ".pause", ".revoke", ".close", ".stop", ".takeover", ".deactivate", ".billing_reconcile"} {
		if strings.HasSuffix(c.Method, suffix) {
			return true
		}
	}
	return false
}
func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	select {
	case s.connections <- struct{}{}:
	default:
		problem(w, api.E("overloaded", "gateway_connection_limit"))
		return
	}
	defer func() { <-s.connections }()
	connectionID := api.NewID("connection")
	if e = s.config.Identity.AcquireConnection(r.Context(), a, s.config.OwnerID, connectionID); e != nil {
		problem(w, e)
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := s.config.Identity.ReleaseConnection(ctx, a, s.config.OwnerID, connectionID); e != nil {
			s.logger.Warn("connection quota release pending")
		}
	}()
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{"harness-wss.v1"}})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	d := s.manifest(a)
	ready := harness.WSReady{Type: "ready", ConnectionID: connectionID, LogicalServiceID: s.config.OwnerID, Profile: api.Profile, TransportProfile: "harness-wss/1", MethodsDigest: d.MethodsDigest, Limits: d.Limits}
	if e = conn.Write(ctx, websocket.MessageText, api.Raw(ready)); e != nil {
		return
	}
	normal := make(chan queued, 28)
	control := make(chan queued, 4)
	ordinarySlots := make(chan struct{}, 28)
	controlSlots := make(chan struct{}, 4)
	var pending sync.WaitGroup
	var writers sync.WaitGroup
	var connMu sync.Mutex
	connBytes := 0
	enqueue := func(value any, priority bool) bool {
		b := api.Raw(value)
		n := len(b)
		connMu.Lock()
		limit := 3 << 20
		if priority {
			limit = 4 << 20
		}
		if connBytes+n > limit {
			connMu.Unlock()
			return false
		}
		if !s.reserveBytes(n, priority) {
			connMu.Unlock()
			return false
		}
		connBytes += n
		connMu.Unlock()
		item := queued{b, priority}
		queue := normal
		if priority {
			queue = control
		}
		select {
		case queue <- item:
			return true
		default:
			connMu.Lock()
			connBytes -= n
			connMu.Unlock()
			s.releaseBytes(n)
			return false
		}
	}
	release := func(item queued) {
		connMu.Lock()
		connBytes -= len(item.bytes)
		connMu.Unlock()
		s.releaseBytes(len(item.bytes))
	}
	writers.Add(1)
	go func() {
		defer writers.Done()
		heartbeat := time.NewTicker(30 * time.Second)
		defer heartbeat.Stop()
		for {
			var item queued
			select {
			case item = <-control:
			default:
				select {
				case <-ctx.Done():
					return
				case item = <-control:
				case item = <-normal:
				case <-heartbeat.C:
					if e := s.config.Identity.CheckCurrent(ctx, a); e != nil {
						cancel()
						return
					}
					if e := s.config.Identity.RefreshConnection(ctx, a, s.config.OwnerID, connectionID); e != nil {
						cancel()
						return
					}
					pingCtx, stop := context.WithTimeout(ctx, 90*time.Second)
					e := conn.Ping(pingCtx)
					stop()
					if e != nil {
						cancel()
						return
					}
					continue
				}
			}
			writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			e := conn.Write(writeCtx, websocket.MessageText, item.bytes)
			stop()
			release(item)
			if e != nil {
				cancel()
				return
			}
		}
	}()
	defer func() {
		cancel()
		conn.CloseNow()
		pending.Wait()
		writers.Wait()
		for {
			select {
			case item := <-normal:
				release(item)
			case item := <-control:
				release(item)
			default:
				return
			}
		}
	}()
	var last uint64
	for {
		message, b, e := conn.Read(ctx)
		if e != nil {
			return
		}
		if message != websocket.MessageText {
			conn.Close(websocket.StatusUnsupportedData, "text frames required")
			return
		}
		var frame harness.WSRequest
		if e = api.DecodeLimit(b, &frame, 1<<20); e != nil || frame.Type != "request" || frame.RequestSeq <= last || frame.RequestSeq > api.MaxSafeInteger {
			conn.Close(websocket.StatusPolicyViolation, "invalid frame or sequence")
			return
		}
		last = frame.RequestSeq
		if e = s.config.Identity.CheckCurrent(ctx, a); e != nil {
			return
		}
		if e = s.config.Identity.RefreshConnection(ctx, a, s.config.OwnerID, connectionID); e != nil {
			return
		}
		priority := isControl(frame.Kind, frame.Payload)
		slots := ordinarySlots
		if priority {
			slots = controlSlots
		}
		reserved := false
		select {
		case slots <- struct{}{}:
			reserved = true
		default:
		}
		if !reserved {
			if !enqueue(harness.WSResponse{"response", frame.RequestSeq, "error", api.Raw(api.E("overloaded", "pending_request_limit"))}, true) {
				return
			}
			continue
		}
		pending.Add(1)
		go func(frame harness.WSRequest, priority bool, slots chan struct{}) {
			defer pending.Done()
			defer func() { <-slots }()
			callCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			kind, body, e := s.config.Processor.Call(callCtx, a, frame.Kind, frame.Payload)
			if e != nil {
				kind = "error"
				body = api.Raw(publicError(e))
			}
			if !enqueue(harness.WSResponse{"response", frame.RequestSeq, kind, body}, priority) {
				cancel()
				conn.CloseNow()
			}
		}(frame, priority, slots)
	}
}
