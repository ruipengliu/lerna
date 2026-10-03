package providers

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	InformationSearch  = "information.search"
	InformationBody    = "information.body"
	InformationPurpose = "information.source"
)

// InformationSourceDescriptor 冻结一个授权的信息源。配置改变必须产生另一准确版本。
// 本参考协议只接公开、不收费的 HTTP 源；收费供应商需要自己的 usage 合同。
type InformationSourceDescriptor struct {
	SourceRef          api.ComponentRef `json:"source_ref"`
	Origin             string           `json:"origin"`
	SearchPath         string           `json:"search_path,omitempty"`
	FetchPathPrefixes  []string         `json:"fetch_path_prefixes"`
	AllowedCIDRs       []string         `json:"allowed_cidrs"`
	Receiver           string           `json:"receiver"`
	Location           string           `json:"location"`
	CredentialID       string           `json:"credential_id,omitempty"`
	CredentialRequired bool             `json:"credential_required"`
	PublicUnbilled     bool             `json:"public_unbilled"`
	MaxQueryBytes      uint64           `json:"max_query_bytes"`
	MaxItems           uint64           `json:"max_items"`
	MaxResponseBytes   uint64           `json:"max_response_bytes"`
	TimeoutMillis      uint64           `json:"timeout_millis"`
	MaxConcurrent      uint64           `json:"max_concurrent"`
}

type InformationConfig struct {
	Store                runtime.Store
	Scope                runtime.Scope
	Source               InformationSourceDescriptor
	Content              InformationContent
	Egress               InformationEgress
	Participants         []string
	Credential           string
	TLSRootPEM           []byte
	AllowHTTPForLoopback bool
}

// ReceivedPublication 的字节只进入受 Content 许可和 holder 管理的介质。
// 恢复沿该准确 ref 的原上传身份，不能重新取得源或刷新上传期限。
type ReceivedPublication struct {
	ContentRef       api.ContentRef   `json:"content_ref"`
	ObtainedAt       string           `json:"obtained_at"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
	DisclosedSources []api.ContentRef `json:"disclosed_sources"`
	SourceRef        api.ComponentRef `json:"source_ref"`
	AttemptID        string           `json:"attempt_id"`
	UseRefs          []api.ObjectRef  `json:"use_refs"`
	RetainUntil      string           `json:"retain_until"`
}

type InformationContent interface {
	ReadBytes(context.Context, runtime.Scope, runtime.Auth, api.ContentRef, string, string) ([]byte, error)
	PublishReceived(context.Context, runtime.Scope, runtime.Auth, ReceivedPublication, []byte) (api.ContentRef, error)
	RecoverReceived(context.Context, runtime.Scope, runtime.Auth, ReceivedPublication) (api.ContentRef, error)
}

// Check 在真实出口前重新核当前主体、原 GrantUse 和所有实际出站资料的许可。
// ActualIPs 是已解析并将在 Dial 中准确使用的地址；不存在另一次隐式 DNS。
type HTTPOutRequest struct {
	SourceRef        api.ComponentRef `json:"source_ref"`
	Action           string           `json:"action"`
	URL              string           `json:"url"`
	Method           string           `json:"method"`
	Receiver         string           `json:"receiver"`
	Location         string           `json:"location"`
	ActualIPs        []string         `json:"actual_ips"`
	RequestDigest    string           `json:"request_digest"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
	DisclosedSources []api.ContentRef `json:"disclosed_sources"`
}
type InformationPermit struct {
	StartBefore    string `json:"start_before"`
	PolicyRevision uint64 `json:"policy_revision"`
	RequestDigest  string `json:"request_digest"`
	RetainUntil    string `json:"retain_until"`
}
type InformationEgress interface {
	Check(context.Context, execution.AttemptRequest, HTTPOutRequest) (InformationPermit, error)
}

type SearchArguments struct {
	QueryRef api.ContentRef `json:"query_ref"`
	Limit    uint64         `json:"limit"`
	Cursor   *string        `json:"cursor,omitempty"`
}
type BodyArguments struct {
	URL string `json:"url"`
}

// SearchResponse 是可替换参考源的闭合 wire 合同；命中不证明事实正确或全网穷尽。
type SearchItem struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	Snippet    string `json:"snippet"`
	ObservedAt string `json:"observed_at,omitempty"`
}
type SearchResponse struct {
	Protocol  string       `json:"protocol"`
	RequestID string       `json:"request_id"`
	Items     []SearchItem `json:"items"`
	Exhausted bool         `json:"exhausted"`
	Cursor    *string      `json:"cursor,omitempty"`
	Coverage  string       `json:"coverage"`
}
type SearchRequest struct {
	Protocol  string  `json:"protocol"`
	RequestID string  `json:"request_id"`
	Query     string  `json:"query"`
	Limit     uint64  `json:"limit"`
	Cursor    *string `json:"cursor,omitempty"`
}

// InformationObservation 只表达原 HTTP 与已获字节；Task 仍独立选择并核验条件。
type InformationObservation struct {
	SourceRef   api.ComponentRef `json:"source_ref"`
	AttemptID   string           `json:"attempt_id"`
	Action      string           `json:"action"`
	URL         string           `json:"url"`
	ObtainedAt  string           `json:"obtained_at"`
	ObservedAt  string           `json:"observed_at,omitempty"`
	HTTPStatus  uint64           `json:"http_status"`
	BodyRef     *api.ContentRef  `json:"body_ref,omitempty"`
	MediaType   string           `json:"media_type"`
	Cache       string           `json:"cache"`
	PartialRead bool             `json:"partial_read"`
	Truncated   bool             `json:"truncated"`
	Items       []SearchItem     `json:"items"`
	Exhausted   bool             `json:"exhausted"`
	Cursor      *string          `json:"cursor,omitempty"`
	Coverage    string           `json:"coverage"`
	Gaps        []string         `json:"gaps"`
}
