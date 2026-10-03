// Package execution 保存原行动的接纳、真实尝试、效果及恢复责任。
package execution

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// Publication 是消费方的小内容端口。实现必须保存准确字节与完整来源；它在 Tx 外调用。
type Publication struct {
	ContentID        string
	MediaType        string
	Purpose          string
	Location         string
	ProcessedSources []api.ContentRef
	DisclosedSources []api.ContentRef
}
type ContentPort interface {
	ReadBytes(context.Context, rt.Scope, rt.Auth, api.ContentRef, string, string) ([]byte, error)
	Publish(context.Context, rt.Scope, rt.Auth, Publication, []byte) (api.ContentRef, error)
}

// PreparedStart 是 Tx 外取得的原授权窗口。它不声明永久当前 allowed。
type PreparedStart struct {
	OperationID       string          `json:"operation_id"`
	IntentHash        string          `json:"intent_hash"`
	Recipient         string          `json:"recipient"`
	UseRefs           []api.ObjectRef `json:"use_refs"`
	ApprovalRefs      []api.ObjectRef `json:"approval_refs"`
	AuthorityRevision uint64          `json:"authority_revision"`
	StartBefore       string          `json:"start_before"`
	ProofRef          api.ContentRef  `json:"proof_ref"`
}
type StartRequest struct {
	ControlWindow api.ControlSnapshot
	Invoke        InvokeInput
	Intent        ExecutionIntent
	AttemptID     string
	Auth          rt.Auth
}
type StartPermit struct {
	StartBefore string           `json:"start_before"`
	ProofRefs   []api.ContentRef `json:"proof_refs"`
}

// AuthorityPort.Verify* 禁止网络 IO。云端只可读显式共库 namespace；
// 设备在 Tx 内只验准确签名、原绑定、截止与本机已知撤权，不能裁决云端 Task。
type AuthorityPort interface {
	VerifyControl(context.Context, rt.Tx, rt.Auth, api.ControlSnapshot) error
	PrepareStart(context.Context, rt.Scope, StartRequest) (PreparedStart, error)
	VerifyStart(context.Context, rt.Tx, StartRequest, PreparedStart) (StartPermit, error)
}

type Capability struct {
	Ref          api.ComponentRef
	EffectClass  string
	MaxAttempts  uint64
	InputSchema  api.Schema
	OutputSchema api.Schema
}
type CellPreparation struct {
	EnvironmentRef            api.ObjectRef    `json:"environment_ref"`
	InstanceID                string           `json:"instance_id"`
	ExpectedGeneration        uint64           `json:"expected_generation"`
	ExpectedNamespaceRevision uint64           `json:"expected_namespace_revision"`
	Namespace                 PassiveNamespace `json:"namespace"`
	Sources                   []api.ContentRef `json:"sources"`
	DynamicNamespace          bool             `json:"dynamic_namespace,omitempty"`
	NamespaceByteLimit        uint64           `json:"namespace_byte_limit,omitempty"`
}
type PreparedRequest struct {
	Encoded           json.RawMessage  `json:"encoded"`
	Cell              *CellPreparation `json:"cell,omitempty"`
	Digest            string           `json:"digest"`
	TargetRequestKey  string           `json:"target_request_key,omitempty"`
	IdempotencyUntil  string           `json:"idempotency_until,omitempty"`
	ResourceID        string           `json:"resource_id,omitempty"`
	ResourceEpoch     uint64           `json:"resource_epoch,omitempty"`
	ObservationID     string           `json:"observation_id,omitempty"`
	ObservationBefore string           `json:"observation_before,omitempty"`
}
type AttemptRequest struct {
	Scope   rt.Scope
	Invoke  InvokeInput
	Intent  ExecutionIntent
	Attempt Attempt
	Auth    rt.Auth
}

// Fact 只能来自登记的受信驱动/独立目标核对，公开 reconcile 不接受调用者自报 effect。
// Revision 是该 Attempt 的累计可信观察修订；同修订异事实拒绝。
type Fact struct {
	Revision      uint64
	Effect        string
	MayApplyLater any
	Output        []byte
	MediaType     string
	Evidence      []api.ContentRef
	Usage         []api.Amount
	UsageFinal    bool
	Disputed      bool
	Namespace     *PassiveNamespace
}
type StopFact struct {
	ActuallyStopped bool
	MayApplyLater   any
}

// Start 必须在真实出口内、持有目标入口串行权时恰好调用一次 barrier；
// barrier 确认持久提交才允许目标 IO。不得在构造、Prepare 或 Reconcile 时发新动作。
type Driver interface {
	Capability() Capability
	Prepare(context.Context, rt.Scope, rt.Auth, InvokeInput, ExecutionIntent, []byte) (PreparedRequest, error)
	Start(context.Context, AttemptRequest, func(context.Context) error) (Fact, error)
	Reconcile(context.Context, AttemptRequest) (Fact, error)
	Stop(context.Context, AttemptRequest) (StopFact, error)
}

// EnvironmentAdmission 只接受宿主登记的准确运行时。Check 不做 IO；Prepare 在 Tx 外实际探针。
// 输入中的隔离布尔或任意 ComponentRef 不能替代此受信端口。
type EnvironmentAdmission interface {
	Check(api.ComponentRef, api.ComponentRef, []api.Amount) (EnvironmentIsolation, error)
	Prepare(context.Context, rt.Scope, rt.Auth, Environment) error
}
type EnvironmentIsolation struct {
	RuntimeKind     string
	IsolationDigest string
}
type Config struct {
	OwnerID               string
	Content               ContentPort
	Authority             AuthorityPort
	AuthorityParticipants []string
	Drivers               []Driver
	Location              string
	ResourceDriver        ResourceDriver
	HostCalls             HostCallPort
	EnvironmentAdmission  EnvironmentAdmission
}
