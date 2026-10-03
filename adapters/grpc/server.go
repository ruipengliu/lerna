// Package grpc 实现严格 Protobuf 外壳与已认证的服务间调用。
package grpc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	rt "github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type Processor interface {
	Call(context.Context, rt.Auth, string, json.RawMessage) (string, json.RawMessage, error)
}
type Config struct {
	OwnerID               string
	Identity              platform.IdentityProvider
	Processor             Processor
	AllowInsecureLoopback bool
	Store                 rt.Store
	MethodsDigest         string
	ApplicationInstanceID string
	EndpointAuthority     EndpointAuthority
	GatewayIdentities     []string
}
type Server struct {
	rpcv1.UnimplementedHarnessServiceServer
	cfg               Config
	ordinary          chan struct{}
	control           chan struct{}
	mu                sync.Mutex
	closing           bool
	served            bool
	owned             sync.WaitGroup
	channels          map[string]*channelSession
	gates             map[string]*channelGate
	identityChannels  map[string]int
	channelCount      int
	queuedBytes       int
	tenantQueuedBytes map[string]int
}
type StrictCodec = grpcwire.Codec

func New(cfg Config) (*Server, error) {
	if !api.ValidID(cfg.OwnerID) || cfg.Identity == nil || cfg.Processor == nil {
		return nil, api.E("invalid_request", "invalid_grpc_configuration")
	}
	if cfg.EndpointAuthority != nil && (cfg.Store == nil || !api.ValidID(cfg.ApplicationInstanceID) || len(cfg.MethodsDigest) != 71 || len(cfg.GatewayIdentities) == 0 || len(cfg.GatewayIdentities) > 100) {
		return nil, api.E("invalid_request", "invalid_endpoint_channel_configuration")
	}
	return &Server{cfg: cfg, ordinary: make(chan struct{}, 32), control: make(chan struct{}, 4), channels: map[string]*channelSession{}, gates: map[string]*channelGate{}, identityChannels: map[string]int{}, tenantQueuedBytes: map[string]int{}}, nil
}

// Serve 持有 listener 生命周期；关闭先停止接纳，最多五秒等待实际 RPC 退出。
func (s *Server) Serve(ctx context.Context, l net.Listener, tlsConfig *tls.Config) error {
	if l == nil {
		return api.E("invalid_request", "grpc_listener_missing")
	}
	defer l.Close()
	s.mu.Lock()
	if s.served {
		s.mu.Unlock()
		return api.E("invalid_state", "grpc_server_already_started")
	}
	s.served = true
	s.mu.Unlock()
	opts := []grpcgo.ServerOption{grpcgo.ForceServerCodec(grpcwire.Codec{}), grpcgo.MaxRecvMsgSize(grpcwire.MaxFrameBytes), grpcgo.MaxSendMsgSize(grpcwire.MaxFrameBytes), grpcgo.MaxConcurrentStreams(36)}
	if tlsConfig == nil {
		if !s.cfg.AllowInsecureLoopback || !loopbackAddress(l.Addr()) {
			return api.E("forbidden", "tls_required")
		}
	} else {
		cfg := tlsConfig.Clone()
		if len(cfg.Certificates) == 0 && cfg.GetCertificate == nil {
			return api.E("invalid_request", "grpc_server_certificate_missing")
		}
		if cfg.MinVersion < tls.VersionTLS13 {
			cfg.MinVersion = tls.VersionTLS13
		}
		opts = append(opts, grpcgo.Creds(credentials.NewTLS(cfg)))
	}
	rpc := grpcgo.NewServer(opts...)
	rpcv1.RegisterHarnessServiceServer(rpc, s)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.closing = true
			s.mu.Unlock()
			stopped := make(chan struct{})
			go func() { rpc.GracefulStop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				rpc.Stop()
				<-stopped
			}
		case <-done:
		}
	}()
	err := rpc.Serve(l)
	close(done)
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	ownedDone := make(chan struct{})
	go func() { s.owned.Wait(); close(ownedDone) }()
	select {
	case <-ownedDone:
	case <-time.After(5 * time.Second):
		return api.E("effect_unknown", "grpc_handlers_not_exited")
	}
	if errors.Is(err, grpcgo.ErrServerStopped) {
		return nil
	}
	return err
}
func loopbackAddress(a net.Addr) bool {
	switch a := a.(type) {
	case *net.TCPAddr:
		return a.IP.IsLoopback()
	default:
		return false
	}
}
func (s *Server) authenticate(ctx context.Context) (rt.Auth, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return rt.Auth{}, status.Error(codes.Unauthenticated, "transport_identity_missing")
	}
	tlsInfo, tlsOK := p.AuthInfo.(credentials.TLSInfo)
	secure := tlsOK && tlsInfo.State.HandshakeComplete && tlsInfo.State.Version >= tls.VersionTLS13
	if !secure && !(s.cfg.AllowInsecureLoopback && p.AuthInfo == nil && loopbackAddress(p.Addr)) {
		return rt.Auth{}, status.Error(codes.Unauthenticated, "tls_required")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if !ok || len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) <= 7 || len(values[0]) > 4096 || strings.ContainsAny(values[0], "\r\n") {
		return rt.Auth{}, status.Error(codes.Unauthenticated, "authentication_required")
	}
	r := &http.Request{Header: make(http.Header)}
	r.Header.Set("Authorization", values[0])
	a, err := s.cfg.Identity.Authenticate(ctx, r)
	if err != nil {
		return rt.Auth{}, status.Error(codes.Unauthenticated, "credential_unavailable")
	}
	if !api.ValidID(a.TenantID) || !api.ValidID(a.SubjectID) || a.CredentialGeneration == 0 {
		return rt.Auth{}, status.Error(codes.Unauthenticated, "invalid_identity")
	}
	return a, nil
}
func publicError(err error) *api.Error {
	var problem *api.Error
	if errors.As(err, &problem) {
		v := *problem
		v.Cause = nil
		return &v
	}
	return api.E("dependency_unavailable", "internal_dependency_failure")
}
func errorResponse(err error) *rpcv1.CallResponse {
	return &rpcv1.CallResponse{Result: &rpcv1.CallResponse_ErrorJson{ErrorJson: api.Raw(publicError(err))}}
}
func requestParts(request *rpcv1.CallRequest) (string, json.RawMessage, error) {
	if request == nil {
		return "", nil, api.E("invalid_request", "grpc_request_missing")
	}
	switch v := request.Request.(type) {
	case *rpcv1.CallRequest_CommandJson:
		return "command", v.CommandJson, nil
	case *rpcv1.CallRequest_QueryJson:
		return "query", v.QueryJson, nil
	case *rpcv1.CallRequest_ReceiptLookupJson:
		return "receipt_lookup", v.ReceiptLookupJson, nil
	case *rpcv1.CallRequest_ContentTransferJson:
		return "content_transfer", v.ContentTransferJson, nil
	default:
		return "", nil, api.E("invalid_request", "grpc_request_missing")
	}
}
func transportPayload(owner, kind string, payload []byte) error {
	v, e := api.ParseJSON(payload)
	if e != nil {
		return e
	}
	o, ok := v.(map[string]any)
	if !ok || o["logical_service_id"] != owner {
		return api.E("invalid_request", "wrong_logical_service")
	}
	if kind == "command" || kind == "query" {
		if o["protocol"] != api.Protocol || o["profile"] != api.Profile {
			return api.E("unsupported", "profile_not_supported")
		}
	}
	return nil
}
func isControl(kind string, b []byte) bool {
	if kind == "receipt_lookup" {
		return true
	}
	if kind != "command" {
		return false
	}
	v, e := api.ParseJSON(b)
	if e != nil {
		return false
	}
	o, ok := v.(map[string]any)
	if !ok {
		return false
	}
	m, _ := o["method"].(string)
	return api.IsControlMethod(m)
}
func (s *Server) Call(ctx context.Context, request *rpcv1.CallRequest) (*rpcv1.CallResponse, error) {
	if !s.begin() {
		return nil, status.Error(codes.Unavailable, "grpc_service_closing")
	}
	defer s.owned.Done()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil || request.LogicalServiceId != s.cfg.OwnerID {
		return errorResponse(api.E("invalid_request", "wrong_logical_service")), nil
	}
	kind, payload, err := requestParts(request)
	if err == nil {
		err = transportPayload(s.cfg.OwnerID, kind, payload)
	}
	if err != nil {
		return errorResponse(err), nil
	}
	slots := s.ordinary
	if isControl(kind, payload) {
		slots = s.control
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		return errorResponse(api.E("overloaded", "grpc_call_limit")), nil
	}
	if err = s.cfg.Identity.CheckCurrent(ctx, a); err != nil {
		return nil, status.Error(codes.Unauthenticated, "credential_revoked")
	}
	resultKind, body, err := s.cfg.Processor.Call(ctx, a, kind, payload)
	if err != nil {
		return errorResponse(err), nil
	}
	if err = s.cfg.Identity.CheckCurrent(ctx, a); err != nil {
		return nil, status.Error(codes.Unauthenticated, "credential_revoked")
	}
	if _, err = api.ParseJSON(body); err != nil {
		return errorResponse(api.E("dependency_unavailable", "invalid_processor_response")), nil
	}
	switch resultKind {
	case "receipt":
		if kind != "command" && kind != "receipt_lookup" {
			break
		}
		return &rpcv1.CallResponse{Result: &rpcv1.CallResponse_ReceiptJson{ReceiptJson: body}}, nil
	case "query_result":
		if kind != "query" {
			break
		}
		return &rpcv1.CallResponse{Result: &rpcv1.CallResponse_QueryResultJson{QueryResultJson: body}}, nil
	case "content_transfer":
		if kind != "content_transfer" {
			break
		}
		return &rpcv1.CallResponse{Result: &rpcv1.CallResponse_ContentTransferJson{ContentTransferJson: body}}, nil
	}
	return errorResponse(api.E("dependency_unavailable", "processor_response_kind_mismatch")), nil
}
func (s *Server) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.owned.Add(1)
	return true
}
