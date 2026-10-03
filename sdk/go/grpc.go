package harness

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type GRPCTransport struct {
	conn         *grpcgo.ClientConn
	client       rpcv1.HarnessServiceClient
	owner, token string
}

// DialGRPC 使用 HTTPS 发现得到的准确服务合同；不让 DNS、retry 或重连换业务 owner。
func DialGRPC(ctx context.Context, address, token string, expected Discovery, tlsConfig *tls.Config, allowDev bool) (*GRPCTransport, error) {
	u, e := url.Parse(address)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Host == "" || u.Port() == "" || len(token) == 0 || len(token) > 4089 || strings.ContainsAny(token, "\r\n") {
		return nil, api.E("invalid_request", "invalid_grpc_endpoint")
	}
	if expected.Protocol != api.Protocol || expected.Profile != api.Profile || expected.SchemaDigest != api.CoreDigest() || !api.ValidID(expected.LogicalServiceID) || expected.IdentityScope == "" {
		return nil, api.E("unsupported", "discovery_mismatch")
	}
	digest, e := api.Digest(expected.Methods)
	if e != nil || digest != expected.MethodsDigest {
		return nil, api.E("unsupported", "method_schema_mismatch")
	}
	var credential credentials.TransportCredentials
	switch u.Scheme {
	case "grpcs":
		cfg := &tls.Config{MinVersion: tls.VersionTLS13}
		if tlsConfig != nil {
			cfg = tlsConfig.Clone()
		}
		if cfg.InsecureSkipVerify {
			return nil, api.E("forbidden", "server_verification_required")
		}
		if cfg.MinVersion < tls.VersionTLS13 {
			cfg.MinVersion = tls.VersionTLS13
		}
		credential = credentials.NewTLS(cfg)
	case "grpc":
		if !allowDev || !loopback(u) {
			return nil, api.E("forbidden", "tls_required")
		}
		credential = insecure.NewCredentials()
	default:
		return nil, api.E("forbidden", "tls_required")
	}
	dialer := func(ctx context.Context, target string) (net.Conn, error) {
		var d net.Dialer
		conn, e := d.DialContext(ctx, "tcp", target)
		if e != nil {
			return nil, e
		}
		if u.Scheme == "grpc" {
			addr, ok := conn.RemoteAddr().(*net.TCPAddr)
			if !ok || !addr.IP.IsLoopback() {
				conn.Close()
				return nil, api.E("forbidden", "tls_required")
			}
		}
		return conn, nil
	}
	conn, e := grpcgo.NewClient(u.Host, grpcgo.WithTransportCredentials(credential), grpcgo.WithContextDialer(dialer), grpcgo.WithDisableRetry(), grpcgo.WithDisableServiceConfig(), grpcgo.WithDefaultCallOptions(grpcgo.ForceCodec(grpcwire.Codec{}), grpcgo.MaxCallRecvMsgSize(grpcwire.MaxFrameBytes), grpcgo.MaxCallSendMsgSize(grpcwire.MaxFrameBytes)))
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		conn.Close()
		return nil, e
	}
	return &GRPCTransport{conn: conn, client: rpcv1.NewHarnessServiceClient(conn), owner: expected.LogicalServiceID, token: token}, nil
}
func (t *GRPCTransport) Close() error { return t.conn.Close() }
func (t *GRPCTransport) Call(ctx context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	v, e := api.ParseJSON(payload)
	if e != nil {
		return nil, e
	}
	o, ok := v.(map[string]any)
	if !ok || o["logical_service_id"] != t.owner {
		return nil, api.E("invalid_request", "wrong_logical_service")
	}
	request := &rpcv1.CallRequest{LogicalServiceId: t.owner}
	switch kind {
	case "command":
		request.Request = &rpcv1.CallRequest_CommandJson{CommandJson: payload}
	case "query":
		request.Request = &rpcv1.CallRequest_QueryJson{QueryJson: payload}
	case "receipt_lookup":
		request.Request = &rpcv1.CallRequest_ReceiptLookupJson{ReceiptLookupJson: payload}
	case "content_transfer":
		request.Request = &rpcv1.CallRequest_ContentTransferJson{ContentTransferJson: payload}
	default:
		return nil, api.E("unsupported", "frame_kind_not_supported")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+t.token)
	response, e := t.client.Call(ctx, request)
	if e != nil {
		return nil, grpcError(e)
	}
	if b := response.GetErrorJson(); len(b) > 0 {
		var problem api.Error
		if e = api.Decode(b, &problem); e != nil {
			return nil, e
		}
		if problem.Code == "" || problem.Reason == "" {
			return nil, api.E("invalid_request", "invalid_remote_error")
		}
		return nil, &problem
	}
	if kind == "command" || kind == "receipt_lookup" {
		if b := response.GetReceiptJson(); len(b) > 0 {
			return b, nil
		}
	}
	if kind == "query" {
		if b := response.GetQueryResultJson(); len(b) > 0 {
			return b, nil
		}
	}
	if kind == "content_transfer" {
		if b := response.GetContentTransferJson(); len(b) > 0 {
			return b, nil
		}
	}
	return nil, api.E("invalid_request", "grpc_response_kind_mismatch")
}
func grpcError(e error) error {
	switch status.Code(e) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return api.E("forbidden", "grpc_authentication_failed")
	case codes.InvalidArgument:
		return api.E("invalid_request", "invalid_grpc_envelope")
	case codes.ResourceExhausted:
		return api.E("overloaded", "grpc_transport_limit")
	case codes.Unimplemented:
		return api.E("unsupported", "grpc_method_not_supported")
	default:
		return &api.Error{Code: "dependency_unavailable", Scope: "transport", Reason: "grpc_wait_failed", Retry: "query_original"}
	}
}

var _ Transport = (*GRPCTransport)(nil)
