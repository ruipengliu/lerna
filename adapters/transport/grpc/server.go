package grpc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	executionlocal "lerna/adapters/execution/local"
	"lerna/adapters/transport/nodetls"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"net"
	"slices"
	"sync"
)

const configurationKey = "harness-configuration"

var capabilities = []string{"capability.invoke.v1", "capability.query.v1", "capability.cancel.v1", "invocation.snapshots.v1"}

// Resolve is a trusted deployment directory. It must select locally constructed
// services; request metadata never supplies a credential or backend address.
type Resolve func(context.Context, authorization.GrantPresentation) (*execution.Service, error)
type session struct {
	p        authorization.GrantPresentation
	selected *wire.Negotiated
}
type Server struct {
	wire.UnimplementedCapabilityServiceServer
	wire.UnimplementedConnectionServiceServer
	endpoint *nodetls.Endpoint
	config   Config
	clock    authorization.Clock
	resolve  Resolve
	journal  *Journal
	mu       sync.Mutex
	sessions map[string]session
	streams  chan struct{}
	rpc      *grpc.Server
}

func NewServer(endpoint *nodetls.Endpoint, clock authorization.Clock, c Config, resolve Resolve, journal *Journal) (*Server, error) {
	if endpoint == nil || clock == nil || resolve == nil || journal == nil {
		return nil, failure(authorization.Invalid)
	}
	if e := c.Validate(); e != nil {
		return nil, e
	}
	s := &Server{endpoint: endpoint, clock: clock, config: c, resolve: resolve, journal: journal, sessions: map[string]session{}, streams: make(chan struct{}, c.MaxStreams)}
	s.rpc = grpc.NewServer(grpc.StaticStreamWindowSize(65536), grpc.StaticConnWindowSize(65536), grpc.ReadBufferSize(4096), grpc.WriteBufferSize(4096), grpc.MaxHeaderListSize(8192), grpc.Creds(credentials.NewTLS(endpoint.ServerConfig())), grpc.MaxRecvMsgSize(c.MaxMessageBytes), grpc.MaxSendMsgSize(c.MaxMessageBytes), grpc.MaxConcurrentStreams(uint32(c.MaxStreams+1)), grpc.ConnectionTimeout(c.IOTimeout), grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: c.SessionTTL, MaxConnectionAge: c.SessionTTL, MaxConnectionAgeGrace: c.IOTimeout}), grpc.UnaryInterceptor(s.unary))
	wire.RegisterCapabilityServiceServer(s.rpc, s)
	wire.RegisterConnectionServiceServer(s.rpc, s)
	return s, nil
}
func (s *Server) Serve(l net.Listener) error {
	return s.rpc.Serve(netutil.LimitListener(l, s.config.MaxConnections))
}
func (s *Server) Stop() { s.rpc.Stop() }
func tlsState(ctx context.Context) (tls.ConnectionState, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return tls.ConnectionState{}, failure(authorization.Unauthenticated)
	}
	v, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return tls.ConnectionState{}, failure(authorization.Unauthenticated)
	}
	return v.State, nil
}
func (s *Server) presented(ctx context.Context, subject string) (authorization.GrantPresentation, error) {
	state, e := tlsState(ctx)
	if e != nil {
		return authorization.GrantPresentation{}, e
	}
	return s.endpoint.Presentation(ctx, state, subject, "", "")
}
func peerKey(p authorization.GrantPresentation) string {
	p.OperationID = ""
	p.SemanticSHA256 = ""
	b, _ := json.Marshal(p)
	return string(b)
}
func (s *Server) Negotiate(ctx context.Context, in *wire.NegotiateRequest) (*wire.Negotiated, error) {
	if in == nil || !taskwire.Known(in.ProtoReflect()) || in.Bootstrap != 1 || len(in.Versions) > 8 || !slices.Contains(in.Versions, uint32(1)) || len(in.Required) > 16 || len(in.Optional) > 16 || in.MaxMessageBytes < 4096 || in.MaxMessageBytes > 1<<20 || in.PendingWindow != 1 || len(in.Subject) == 0 || len(in.Subject) > 128 {
		return nil, status.Error(codes.InvalidArgument, "incompatible bootstrap or limits")
	}
	selected := []string{}
	for _, name := range in.Required {
		if !slices.Contains(capabilities, name) {
			return nil, status.Error(codes.Unimplemented, "missing required capability")
		}
		if !slices.Contains(selected, name) {
			selected = append(selected, name)
		}
	}
	for _, name := range in.Optional {
		if slices.Contains(capabilities, name) && !slices.Contains(selected, name) {
			selected = append(selected, name)
		}
	}
	p, e := s.presented(ctx, in.Subject)
	if e != nil {
		return nil, rpcError(e)
	}
	svc, e := s.resolve(ctx, p)
	if e != nil {
		return nil, rpcError(e)
	}
	if svc == nil {
		return nil, rpcError(failure(authorization.Denied))
	}
	if e = svc.MatchPeer(p); e != nil {
		return nil, rpcError(e)
	}
	now, e := s.clock.Now()
	if e != nil {
		return nil, rpcError(e)
	}
	id, e := randomid.New()
	if e != nil {
		return nil, rpcError(e)
	}
	out := &wire.Negotiated{Configuration: id, Version: 1, Capabilities: selected, MaxMessageBytes: uint32(min(s.config.MaxMessageBytes, int(in.MaxMessageBytes))), PendingWindow: 1, ExpiresUnixNano: now.Add(s.config.SessionTTL).UnixNano()}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if now.UnixNano() >= v.selected.ExpiresUnixNano {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= s.config.MaxSessions {
		return nil, status.Error(codes.ResourceExhausted, "configuration capacity")
	}
	s.sessions[id] = session{p, proto.Clone(out).(*wire.Negotiated)}
	return out, nil
}
func (s *Server) current(ctx context.Context, need string) (session, *execution.Service, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	ids := md.Get(configurationKey)
	if len(ids) != 1 {
		return session{}, nil, status.Error(codes.FailedPrecondition, "negotiate required")
	}
	s.mu.Lock()
	v, ok := s.sessions[ids[0]]
	s.mu.Unlock()
	now, e := s.clock.Now()
	if e != nil {
		return v, nil, rpcError(e)
	}
	if !ok || now.UnixNano() >= v.selected.ExpiresUnixNano {
		return v, nil, status.Error(codes.FailedPrecondition, "configuration expired")
	}
	if !slices.Contains(v.selected.Capabilities, need) {
		return v, nil, status.Error(codes.Unimplemented, "capability not negotiated")
	}
	p, e := s.presented(ctx, v.p.Subject)
	if e != nil {
		return v, nil, rpcError(e)
	}
	if p != v.p {
		return v, nil, status.Error(codes.PermissionDenied, "configuration peer mismatch")
	}
	svc, e := s.resolve(ctx, p)
	if e != nil {
		return v, nil, rpcError(e)
	}
	if svc == nil {
		return v, nil, rpcError(failure(authorization.Denied))
	}
	if e = svc.MatchPeer(p); e != nil {
		return v, nil, rpcError(e)
	}
	return v, svc, nil
}

// The interceptor covers unsupported generated methods too. Implemented methods
// check their exact payload branch inside the shared binding below.
func (s *Server) unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	defer cancel()
	return handler(ctx, req)
}
func rpcError(e error) error {
	if status.Code(e) != codes.Unknown {
		return e
	}
	var a *authorization.Error
	if errors.As(e, &a) {
		switch a.Code {
		case authorization.Denied:
			return status.Error(codes.PermissionDenied, string(a.Code))
		case authorization.Unauthenticated:
			return status.Error(codes.Unauthenticated, string(a.Code))
		case authorization.Invalid:
			return status.Error(codes.InvalidArgument, string(a.Code))
		case authorization.Unsupported:
			return status.Error(codes.Unimplemented, string(a.Code))
		}
	}
	return status.Error(codes.Unavailable, "authority unavailable")
}
func businessFailure(in *wire.CapabilityRequest, e error) *wire.CapabilityResponse {
	code := authorization.Unavailable
	var a *authorization.Error
	if errors.As(e, &a) {
		code = a.Code
	}
	id, _ := randomid.New()
	return &wire.CapabilityResponse{MessageId: id, ReplyTo: in.MessageId, Namespace: in.Namespace, Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: string(code)}}}
}
func (s *Server) call(ctx context.Context, in *wire.CapabilityRequest, branch int, need string) (*wire.CapabilityResponse, error) {
	v, svc, e := s.current(ctx, need)
	if e != nil {
		return nil, e
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "missing request")
	}
	field := in.ProtoReflect().WhichOneof(in.ProtoReflect().Descriptor().Oneofs().ByName("body"))
	if field == nil || int(field.Number()) != branch || in.Namespace != v.p.Namespace || !taskwire.Known(in.ProtoReflect()) || proto.Size(in) > int(v.selected.MaxMessageBytes) {
		return businessFailure(in, failure(authorization.Invalid)), nil
	}
	raw, e := proto.Marshal(in)
	if e != nil {
		return nil, rpcError(e)
	}
	raw, e = executionlocal.Bind(remoteService{svc}, v.p.Namespace).Exchange(ctx, raw)
	if e != nil {
		return businessFailure(in, e), nil
	}
	out := new(wire.CapabilityResponse)
	if proto.Unmarshal(raw, out) != nil {
		return nil, status.Error(codes.Internal, "invalid domain response")
	}
	if _, _, e = s.current(ctx, need); e != nil {
		return nil, e
	}
	if proto.Size(out) > int(v.selected.MaxMessageBytes) {
		return nil, status.Error(codes.ResourceExhausted, "response limit")
	}
	return out, nil
}

type remoteService struct{ *execution.Service }

func (r remoteService) GetInvocation(ctx context.Context, op string) (execution.Record, error) {
	return r.ReadRemoteInvocation(ctx, op)
}
func (r remoteService) Invoke(ctx context.Context, in execution.Request, material string) (execution.Receipt, error) {
	out, e := r.Service.Invoke(ctx, in, material)
	if e != nil {
		return out, e
	}
	_, e = r.ReadRemoteInvocation(ctx, in.OperationID)
	return out, e
}
func (s *Server) Invoke(c context.Context, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	return s.call(c, r, 10, "capability.invoke.v1")
}
func (s *Server) GetInvocation(c context.Context, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	return s.call(c, r, 11, "capability.query.v1")
}
func (s *Server) Reconcile(c context.Context, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	return s.call(c, r, 12, "capability.query.v1")
}
func (s *Server) RequestCancel(c context.Context, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	return s.call(c, r, 13, "capability.cancel.v1")
}
func (s *Server) GetCancel(c context.Context, r *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	return s.call(c, r, 14, "capability.cancel.v1")
}

func (r remoteService) Reconcile(ctx context.Context, op string) (execution.Record, error) {
	if _, e := r.ReadRemoteInvocation(ctx, op); e != nil {
		return execution.Record{}, e
	}
	return r.Service.Reconcile(ctx, op)
}
func (r remoteService) RequestCancel(ctx context.Context, in execution.CancelRequest) (execution.Receipt, error) {
	if _, e := r.ReadRemoteInvocation(ctx, in.Invocation); e != nil {
		return execution.Receipt{}, e
	}
	return r.Service.RequestCancel(ctx, in)
}
func (r remoteService) GetCancel(ctx context.Context, op string) (execution.CancelRecord, error) {
	out, e := r.Service.GetCancel(ctx, op)
	if e != nil {
		return out, e
	}
	_, e = r.ReadRemoteInvocation(ctx, out.Request.Invocation)
	return out, e
}

// Local binds the same remote-disclosure contract for conformance checks and
// trusted in-process callers. The host supplies its authenticated service.
func Local(s *execution.Service, namespace string) *executionlocal.Binding {
	return executionlocal.Bind(remoteService{s}, namespace)
}
