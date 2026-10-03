package runtime

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
)

// QueryBindingStore 是短期查询身份端口。实现只保存摘要，查询仍必须执行当前披露门禁。
// TTL 只在首次 Bind 固定，1 秒至 5 分钟；过期身份可以重新用于查询。
type QueryBindingStore interface {
	BindQuery(context.Context, Scope, QueryBindingInput) (QueryBinding, CommitStatus, error)
	SealQuery(context.Context, Scope, QueryBinding, string) (QueryBinding, CommitStatus, error)
	PruneQueries(context.Context, Scope, int) (int, CommitStatus, error)
}

type queryBindingContextKey struct{}

// WithQueryBinding 由 Dispatcher 在当前查询方法调用前固定；这里只传元数据，不授予披露资格。
func WithQueryBinding(ctx context.Context, binding QueryBinding) context.Context {
	return context.WithValue(ctx, queryBindingContextKey{}, binding)
}

// QueryBindingExpiry 让同 query_id 的临时游标保持原期限。当前门禁仍各自读取数据库时间。
func QueryBindingExpiry(ctx context.Context) (time.Time, bool) {
	binding, ok := ctx.Value(queryBindingContextKey{}).(QueryBinding)
	if !ok {
		return time.Time{}, false
	}
	expiry, err := api.ParseTime(binding.ExpiresAt)
	return expiry, err == nil
}

type QueryBindingInput struct {
	QueryID              string
	PrincipalID          string
	CredentialGeneration uint64
	RolesDigest          string
	QueryDigest          string
	TTL                  time.Duration
}

// BindingID 区分同 QueryID 在期限后的新生命周期；旧查询不能写入新的绑定。
type QueryBinding struct {
	QueryID              string `json:"query_id"`
	BindingID            string `json:"binding_id"`
	PrincipalID          string `json:"principal_id"`
	CredentialGeneration uint64 `json:"credential_generation"`
	RolesDigest          string `json:"roles_digest"`
	QueryDigest          string `json:"query_digest"`
	ResultDigest         string `json:"result_digest,omitempty"`
	ExpiresAt            string `json:"expires_at"`
}
