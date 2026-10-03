package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// ForeignReference 由消费方在任何远端登记前冻结；所有恢复和收尾沿原身份。
type ForeignReference struct {
	ContentRef         api.ContentRef `json:"content_ref"`
	CopyID             string         `json:"copy_id"`
	RegisterCommandID  string         `json:"register_command_id"`
	ReleaseCommandID   string         `json:"release_command_id"`
	ReferenceIntentRef api.ObjectRef  `json:"reference_intent_ref"`
	HolderRef          api.ObjectRef  `json:"holder_ref"`
	Purpose            string         `json:"purpose"`
	Location           string         `json:"location"`
	RetainUntil        string         `json:"retain_until"`
}

// ForeignProof 与源 owner 的当前 Content/CopyHolder 同义，不重写引用的 owner。
// Proof 是原 authority 的准确签名；宿主只装配固定 issuer/key/数据库与 audience。
type ForeignProof struct {
	ContentRef         api.ContentRef   `json:"content_ref"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	PolicyValues       PolicyValues     `json:"policy_values"`
	SourceDatabaseID   string           `json:"source_database_id"`
	ControlRevision    uint64           `json:"control_revision"`
	RetainUntil        string           `json:"retain_until"`
	ProcessedSources   []api.ContentRef `json:"processed_sources"`
	DisclosedSources   []api.ContentRef `json:"disclosed_sources"`
	IssuedAt           string           `json:"issued_at"`
	StartBefore        string           `json:"start_before"`
	SubjectRef         api.ObjectRef    `json:"subject_ref"`
	HolderRef          api.ObjectRef    `json:"holder_ref"`
	Purpose            string           `json:"purpose"`
	Location           string           `json:"location"`
	CopyID             string           `json:"copy_id"`
	ReferenceIntentRef api.ObjectRef    `json:"reference_intent_ref"`
	SourceState        string           `json:"source_state"`
	ClosureKind        string           `json:"closure_kind,omitempty"`
	UseState           string           `json:"use_state"`
	CleanupState       string           `json:"cleanup_state"`
	EvidenceRefs       []api.ContentRef `json:"evidence_refs"`
	Continuous         bool             `json:"continuous"`
	IndependentDerived bool             `json:"independent_derived"`
	Proof              string           `json:"proof"`
}

// ForeignUse 仅在本次有界请求中传递新取得的准确证明；磁盘镜像不是新读取许可。
type ForeignUse struct {
	Reference ForeignReference `json:"reference"`
	Proof     ForeignProof     `json:"proof"`
}

// ForeignContentPort 的所有 RPC/字节 IO 都在 Tx 外；VerifyTx 不得出站。
// Register/Release 适配原 content.register_copy/release_copy 合同，不能另造授权。
type ForeignContentPort interface {
	RegisterCopy(context.Context, runtime.Scope, runtime.Auth, ForeignReference) (ForeignProof, error)
	Current(context.Context, runtime.Scope, runtime.Auth, ForeignReference) (ForeignProof, error)
	Read(context.Context, runtime.Scope, runtime.Auth, ForeignReference, ForeignProof) ([]byte, error)
	Control(context.Context, runtime.Scope, runtime.Auth, ForeignReference) (ForeignProof, error)
	Release(context.Context, runtime.Scope, runtime.Auth, ForeignReference, ReleaseCopyInput) (ForeignProof, error)
	VerifyTx(context.Context, runtime.Tx, runtime.Auth, ForeignReference, ForeignProof) error
}
