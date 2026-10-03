package harness

import (
	"context"
	"crypto/ecdsa"
	"net/http"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type EndpointProofReader interface {
	ReadDeliveryProof(context.Context, api.ContentRef) ([]byte, error)
}

// EndpointReceiver自己负责原命令准入、效果与当前结果披露；Lookup不得重执行。
// Invoke错误表示结果不明，SDK只保留原责任，不能替它制造成功或失败Reply。
type EndpointReceiver interface {
	Invoke(context.Context, grpcwire.Delivery) (grpcwire.Reply, error)
	Lookup(context.Context, grpcwire.Delivery) (grpcwire.Reply, bool, error)
}
type EndpointConfig struct {
	TenantID, IssuerServiceID, RecipientServiceID string
	EndpointID, InstanceID                        string
	Generation                                    uint64
	IdentityScope                                 string
	IdentityRevision                              uint64
	Profile, SchemaDigest                         string
	Methods                                       []api.MethodContract
	Keys                                          map[string]*ecdsa.PublicKey
	Proofs                                        EndpointProofReader
	Current                                       func(context.Context) error
	Receiver                                      EndpointReceiver
	Journal                                       *ReplyJournal
	// ReceiptDecoder是原接收owner的明确原receipt_lookup输出解码器；缺少时该kind关闭。
	ReceiptDecoder func(context.Context, api.ReceiptLookup, api.Receipt) error
}
type EndpointInvocation struct {
	Sequence           uint64 `json:"sequence"`
	Phase              string `json:"phase"`
	IssuerServiceID    string `json:"issuer_service_id"`
	RecipientServiceID string `json:"recipient_service_id"`
	Protocol           string `json:"protocol"`
	Profile            string `json:"profile"`
	SchemaDigest       string `json:"schema_digest"`
	MethodSchemaDigest string `json:"method_schema_digest"`
	IdentityRevision   uint64 `json:"identity_revision"`
}

func DialWebSocketEndpointWithHTTP(ctx context.Context, address, token string, expected Discovery, allowDev bool, httpClient *http.Client, cfg EndpointConfig) (*WSTransport, error) {
	endpoint, err := newWSEndpoint(cfg, expected)
	if err != nil {
		return nil, err
	}
	if httpClient != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
			return nil, api.E("forbidden", "tls_verification_required")
		}
	}
	return dialWebSocketWithEndpoint(ctx, address, token, expected, allowDev, httpClient, endpoint)
}
