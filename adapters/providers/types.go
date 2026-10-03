// Package providers 实现显式供应商出口；构造和原调用恢复不发送新请求。
package providers

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

// TokenCounter 的实现必须在本机计数；其 Ref 固定算法与适用模型合同。
type TokenCounter interface {
	Ref() api.ComponentRef
	Count(context.Context, []byte) (uint64, string, error)
}

// UTF8UpperBound 只适用于每个 token 消耗至少一个 UTF-8 字节、两条消息
// 的额外 framing 不超过 64 token 的明确模型合同；不是精确 tokenizer。
type UTF8UpperBound struct{}

func (UTF8UpperBound) Ref() api.ComponentRef {
	return api.ComponentRef{ComponentID: "tokenizer_f89dd86ed6f1b3da4f3b734b800d0157", Version: "1", Digest: api.Hash([]byte("byte-token-model;two-message-framing<=64;whole-wire-utf8-bytes+64;v1"))}
}
func (UTF8UpperBound) Count(ctx context.Context, b []byte) (uint64, string, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	return uint64(len(b)) + 64, "upper_bound", nil
}

// OpenAIConfig 是宿主冻结的出口、模型、计数器及计费合同。APIKey 仅由
// 平台凭据装配提供，不写入请求体、原调用账或日志。未配置凭据不开放能力。
type OpenAIConfig struct {
	Store                                                             runtime.Store
	Scope                                                             runtime.Scope
	Profile                                                           brain.Profile
	Endpoint, Model, Receiver, Location                               string
	APIKey, CredentialID                                              string
	Tokenizer                                                         TokenCounter
	InputUSDPerMillion, OutputUSDPerMillion, CachedInputUSDPerMillion string
	// BillingFinal 仅在宿主确认冻结费率和供应商 usage 是最终账单合同后启用。
	// 默认 false 保留未结费用，不能凭 token 猜测结清。
	BillingFinal         bool
	Guidance             string
	MaxResponseBytes     uint64
	MaxConcurrent        int
	AllowHTTPForLoopback bool
}

type TokenUsage struct {
	Prompt      uint64 `json:"prompt"`
	Completion  uint64 `json:"completion"`
	CachedInput uint64 `json:"cached_input"`
	Total       uint64 `json:"total"`
}

type CallView struct {
	Ref                api.ObjectRef    `json:"ref"`
	CallID             string           `json:"call_id"`
	ProfileRef         api.ComponentRef `json:"profile_ref"`
	EncodingDigest     string           `json:"encoding_digest"`
	Status             string           `json:"status"`
	SentAt             string           `json:"sent_at"`
	ProviderResponseID string           `json:"provider_response_id,omitempty"`
	ResponseDigest     string           `json:"response_digest,omitempty"`
	Tokens             TokenUsage       `json:"tokens"`
	FailureReason      string           `json:"failure_reason,omitempty"`
}

type callRecord struct {
	Revision           uint64   `json:"revision"`
	View               CallView `json:"view"`
	ReplyChunks        uint64   `json:"reply_chunks"`
	HTTPStatus         uint64   `json:"http_status"`
	GeneratedAvailable bool     `json:"generated_available"`
}

// 每个片段均小于 Runtime 记录字节上限；原字节不经 JSON 字段转码。
type responseChunk struct {
	CallID string `json:"call_id"`
	Index  uint64 `json:"index"`
	Body   []byte `json:"body"`
}

type frozenConfig struct {
	Endpoint, Model, Receiver, Location, CredentialID, CredentialDigest        string
	TokenizerRef                                                               api.ComponentRef
	InputUSDPerMillion, OutputUSDPerMillion, CachedInputUSDPerMillion          string
	BillingFinal                                                               bool
	Guidance, OutputSchemaDigest                                               string
	WireContractDigest                                                         string
	ContextLimit, MaxInputTokens, MaxOutputTokens, SafetyMargin, MaxInputBytes uint64
	RequestTimeout                                                             time.Duration
	MaxResponseBytes                                                           uint64
	MaxConcurrent                                                              int
}
