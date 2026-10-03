package grpc

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// CredentialSource 只给当前已认证的原主体取受控凭据；不能替换为服务主体。
type CredentialSource func(context.Context, runtime.Auth) (string, error)

type ForwardProcessor struct {
	address          string
	owner            string
	methods          []api.MethodContract
	methodsDigest    string
	credentials      CredentialSource
	allowDevelopment bool
}

func NewForwardProcessor(address, owner string, methods []api.MethodContract, credentials CredentialSource, allowDevelopment bool) (*ForwardProcessor, error) {
	if address == "" || !api.ValidID(owner) || len(methods) == 0 || credentials == nil {
		return nil, api.E("invalid_request", "fixed_application_route_required")
	}
	digest, err := api.DigestLimit(methods, 1<<20)
	if err != nil {
		return nil, err
	}
	return &ForwardProcessor{address: address, owner: owner, methods: append([]api.MethodContract{}, methods...), methodsDigest: digest, credentials: credentials, allowDevelopment: allowDevelopment}, nil
}

// Call 保留原域字节/原命令身份；禁用重试，等待失败由原owner回执恢复。
func (p *ForwardProcessor) Call(ctx context.Context, auth runtime.Auth, kind string, payload json.RawMessage) (string, json.RawMessage, error) {
	var resultKind string
	switch kind {
	case "command", "receipt_lookup":
		resultKind = "receipt"
	case "query":
		resultKind = "query_result"
	case "content_transfer":
		resultKind = "content_transfer"
	default:
		return "", nil, api.E("unsupported", "frame_kind_not_supported")
	}
	token, err := p.credentials(ctx, auth)
	if err != nil {
		return "", nil, err
	}
	scope, err := api.Digest(auth)
	if err != nil {
		return "", nil, err
	}
	manifest := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, SchemaDigest: api.CoreDigest(), LogicalServiceID: p.owner, IdentityScope: scope, IdentityRevision: auth.CredentialGeneration, MethodsDigest: p.methodsDigest, Methods: p.methods}
	transport, err := harness.DialGRPC(ctx, p.address, token, manifest, nil, p.allowDevelopment)
	if err != nil {
		return "", nil, err
	}
	defer transport.Close()
	response, err := transport.Call(ctx, kind, payload)
	return resultKind, response, err
}
