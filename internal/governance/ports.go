// Package governance 保存授权、证据资格、发布和评测的原责任。
// 运行时对象、模型输出和 UI 缓存不是这些事实的权威。
package governance

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const Namespace = "governance"

// ContentPort 只读取准确版本；实现必须核验当前来源、用途和主体。
// 所有调用在数据库事务之外。
type ContentPort interface {
	Read(context.Context, runtime.Scope, runtime.Auth, api.ContentRef, string) ([]byte, error)
}

// UsageVerifier 从实际计费源核验累计账单，不接受调用方自报零费用。
// Verify 在 Tx 外运行，其结果由原 use/source/revision 绑定后提交。
type UsageVerifier interface {
	Verify(context.Context, runtime.Scope, api.ObjectRef, api.UsageSnapshot) error
}

// ProofPort 的签名计算只使用已登记的本地密钥，禁止在 Tx 内网络 KMS。
// 返回 JWS 由 receipt 同事务保存；平台适配另将这些准确字节保存为 Content。
// Verify 必须核验 kid/alg/issuer/audience/tenant/object/digest 和有限窗口。
type ProofPort interface {
	SignLocal(ProofStatement) (string, error)
	VerifyLocal(string, ProofStatement, time.Time) error
}

type ProofStatement struct {
	TenantID    string        `json:"tenant_id"`
	IssuerID    string        `json:"issuer_id"`
	AudienceID  string        `json:"audience_id"`
	Purpose     string        `json:"purpose"`
	ObjectRef   api.ObjectRef `json:"object_ref"`
	Digest      string        `json:"digest"`
	IssuedAt    string        `json:"issued_at"`
	StartBefore string        `json:"start_before"`
}

// LifecyclePort 对内置受信组件或经过验证的隔离宿主工作。构造、准备和
// 自检不得发送模型或目标动作；实例句柄严格绑定原 instance/generation。
type LifecyclePort interface {
	Prepare(context.Context, Installation) (PreparationEvidence, error)
	Initialize(context.Context, InstanceRequest) (InstanceEvidence, error)
	Fence(context.Context, InstanceRequest) (FenceEvidence, error)
	Dispose(context.Context, Installation) (DisposalEvidence, error)
}

// InstallationAdmission 只核宿主静态 allowlist，不得 IO 或从业务输入升级信任。
// 宿主实现此口时，准入在创建准备对象和 Job 之前完成。
type InstallationAdmission interface {
	CheckInstallation(Installation) error
}

// EvaluationPlanAdmission 只核已配置的准确实现与预算单位；不授予正式资格。
type EvaluationPlanAdmission interface {
	CheckEvaluationPlan(EvaluationPlan) error
}

// EvaluationRunner 的两臂独立环境和目标真值不受 candidate 写权控制。
// 一个完整样本先预检两臂，再按原环境键运行；重复原键必须查原事实。
// 未配备该端口不会产生虚假的 pass，只保留明确 blocked/not_run。
type EvaluationRunner interface {
	PreparePair(context.Context, RunnerPair) (PairEvidence, error)
	Run(context.Context, RunnerAttempt) (AttemptObservation, error)
	Lookup(context.Context, RunnerAttempt) (AttemptObservation, bool, error)
	Seal(context.Context, RunnerPair) (PairStopEvidence, error)
}

// PreviewGate 同库核验准确预览的当前披露，不证明用户已阅读。
type PreviewGate interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, []api.ContentRef) error
}

// OfflineGate 仅核设备本库已知撤权、原 Orchestrator 准入、Control /
// TaskGate / 资源代次，返回这些依据的最紧截止，不能在 Tx 内 RPC。
type OfflineGate interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, GrantLease, UseRequest) (string, error)
}

// CalibrationGate 核验高影响规则当前独立校准依据；布尔自述不能替代它。
type CalibrationGate interface {
	CheckTx(context.Context, runtime.Tx, RuleDefinition) error
}

// FormalPlanGate 从独立受信登记核验原数据谱系、partition、candidate 谱系、
// 准确 manifest 和预冻结政策；提供 ContentRef 或 evaluation_authority
// 身份本身不能证明未暴露保留集。这里只能同库读事实，不可 Tx 内 RPC。
type FormalPlanGate interface {
	CheckTx(context.Context, runtime.Tx, EvaluationPlan) error
}

// ResultNoticeSink 由宿主显式声明同库参与者，机械转交已核准的原通知。
// 只能同 Tx 保存原事实，不得 RPC、修改最终 Result 或吞掉失败。
type ResultNoticeSink interface {
	RecordNoticeTx(context.Context, runtime.Tx, ResultNotice) error
}

// GrantMetadataGate checks current credentials and metadata visibility in the
// caller's database transaction. Its digest covers the current authority state;
// it must not perform network or Content I/O.
type GrantMetadataGate interface {
	VisibilityTx(context.Context, runtime.Tx, runtime.Auth) (string, error)
}

type Options struct {
	GrantMetadataGate GrantMetadataGate
	KnowledgeGate     KnowledgeGate
	ResultNotices     ResultNoticeSink
	PreviewGate       PreviewGate
	Participants      []string
	OfflineGate       OfflineGate
	CalibrationGate   CalibrationGate
	FormalPlanGate    FormalPlanGate
	EndpointID        string
	InstanceID        string
	Content           ContentPort
	UsageVerifier     UsageVerifier
	Proof             ProofPort
	Lifecycle         LifecyclePort
	Runner            EvaluationRunner
}

type Service struct {
	Store    runtime.Store
	Ports    Options
	registry *runtime.Registry
}

func New(store runtime.Store, options Options) *Service {
	return &Service{Store: store, Ports: options}
}

type CheckReference struct {
	CheckRef api.ObjectRef `json:"check_ref"`
}

type EvidenceCompletion struct {
	ConsumerTaskRef     api.ObjectRef    `json:"consumer_task_ref"`
	Checks              []CheckReference `json:"checks"`
	MaxStalenessSeconds uint64           `json:"max_staleness_seconds"`
	ResultRef           *api.ObjectRef   `json:"result_ref,omitempty"`
}

type EvidenceDecision struct {
	Eligible    bool            `json:"eligible"`
	ExpiresAt   string          `json:"expires_at"`
	GateRefs    []api.ObjectRef `json:"gate_refs"`
	HolderRefs  []api.ObjectRef `json:"holder_refs"`
	Limitations []string        `json:"limitations"`
}

// tx 调用者必须由宿主显式声明同数据库、同租户/owner及 governance
// participant；这些入口不会偷偷跨域读取或 RPC。

func (s *Service) participants() []string {
	out := []string{Namespace}
	for _, part := range s.Ports.Participants {
		if !contains(out, part) {
			out = append(out, part)
		}
	}
	return out
}
