package runtime

import (
	"context"
	"time"
)

// QueryBindingStore 是短期查询身份端口。实现只保存摘要，查询仍必须执行当前披露门禁。
// TTL 只在首次 Bind 固定，1 秒至 5 分钟；过期身份可以重新用于查询。
type QueryBindingStore interface {
	BindQuery(context.Context, Scope, QueryBindingInput) (QueryBinding, CommitStatus, error)
	SealQuery(context.Context, Scope, QueryBinding, string) (QueryBinding, CommitStatus, error)
	PruneQueries(context.Context, Scope, int) (int, CommitStatus, error)
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
