package execution

import (
	"encoding/json"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

const Namespace = "execution"
const RunJob = "execution.run"
const ReconcileJob = "execution.reconcile"
const StopJob = "execution.stop"

type InvokeInput struct {
	OperationID     string              `json:"operation_id"`
	TaskRef         api.ObjectRef       `json:"task_ref"`
	GoalRevision    uint64              `json:"goal_revision"`
	ControlRevision uint64              `json:"control_revision"`
	CapabilityRef   api.ComponentRef    `json:"capability_ref"`
	BindingRef      api.ObjectRef       `json:"binding_ref"`
	IntentRef       api.ContentRef      `json:"intent_ref"`
	IntentHash      string              `json:"intent_hash"`
	UseRefs         []api.ObjectRef     `json:"use_refs"`
	Deadline        string              `json:"deadline"`
	ControlSnapshot api.ControlSnapshot `json:"control_snapshot"`
	ReservationRef  api.ObjectRef       `json:"reservation_ref"`
}

// ExecutionIntent 是发送前封存的 Content 正文，不包含刷新参数的入口。
// 参数和资源另以准确 Content 版本引用；Capability 对参数实施闭合 Schema。
type ExecutionIntent struct {
	OperationID         string               `json:"operation_id"`
	TaskRef             api.ObjectRef        `json:"task_ref"`
	GoalRevision        uint64               `json:"goal_revision"`
	ControlRevision     uint64               `json:"control_revision"`
	AdmissionSourceKind string               `json:"admission_source_kind"`
	AdmissionSourceRef  api.ObjectRef        `json:"admission_source_ref"`
	SourcePosition      string               `json:"source_position"`
	AdmissionPurpose    string               `json:"admission_purpose"`
	CapabilityRef       api.ComponentRef     `json:"capability_ref"`
	BindingRef          api.ObjectRef        `json:"binding_ref"`
	InstallLockRef      api.ComponentRef     `json:"install_lock_ref"`
	ArgumentsRef        api.ContentRef       `json:"arguments_ref"`
	ResourceRefs        []api.ObjectRef      `json:"resource_refs"`
	RequirementRefs     []api.RequirementRef `json:"requirement_refs"`
	CostBound           []api.Amount         `json:"cost_bound"`
	ExecutorID          string               `json:"executor_id"`
	Deadline            string               `json:"deadline"`
	TaskDeadline        string               `json:"task_deadline"`
	LogicalStepKey      string               `json:"logical_step_key"`
	RetryOfOperationRef *api.ObjectRef       `json:"retry_of_operation_ref,omitempty"`
	ProcessedSourceRefs []api.ContentRef     `json:"processed_source_refs"`
	DisclosedSourceRefs []api.ContentRef     `json:"disclosed_source_refs"`
}
type CancelInput struct {
	OperationID    string        `json:"operation_id"`
	Reason         string        `json:"reason"`
	TaskRef        api.ObjectRef `json:"task_ref"`
	OrchestratorID string        `json:"orchestrator_id"`
}
type OperationIDInput struct {
	OperationID string `json:"operation_id"`
}
type ReconcileInput struct {
	OperationID    string `json:"operation_id"`
	SourceRevision uint64 `json:"source_revision,omitempty"`
}
type ControlInput struct {
	TaskRef  api.ObjectRef       `json:"task_ref"`
	Snapshot api.ControlSnapshot `json:"control_snapshot"`
}
type ControlGetInput struct {
	TaskID string `json:"task_id"`
}
type OperationOutput struct {
	OperationRef      api.ObjectRef `json:"operation_ref"`
	ExecutionState    string        `json:"execution_state"`
	Effect            string        `json:"effect"`
	MayApplyLater     any           `json:"may_apply_later"`
	NewAttemptsClosed bool          `json:"new_attempts_closed"`
	ActuallyStopped   bool          `json:"actually_stopped"`
}
type OperationView struct {
	Operation         api.Operation         `json:"operation"`
	NewAttemptsClosed bool                  `json:"new_attempts_closed"`
	ActuallyStopped   bool                  `json:"actually_stopped"`
	EffectDisputed    bool                  `json:"effect_disputed"`
	Attempts          api.Page[AttemptView] `json:"attempts"`
}
type TaskGate struct {
	TaskRef         api.ObjectRef `json:"task_ref"`
	Revision        uint64        `json:"revision"`
	GoalRevision    uint64        `json:"goal_revision"`
	ControlRevision uint64        `json:"control_revision"`
	Status          string        `json:"status"`
	Control         string        `json:"control"`
	ControlDigest   string        `json:"control_digest"`
}
type ControlView struct {
	Gate    TaskGate              `json:"gate"`
	Windows []api.ControlSnapshot `json:"windows"`
}
type gateFact struct {
	OrchestratorID  string `json:"orchestrator_id"`
	TaskID          string `json:"task_id"`
	GoalRevision    uint64 `json:"goal_revision"`
	ControlRevision uint64 `json:"control_revision"`
	Status          string `json:"status"`
	Control         string `json:"control"`
}
type Attempt struct {
	AttemptID         string           `json:"attempt_id"`
	OperationID       string           `json:"operation_id"`
	Revision          uint64           `json:"revision"`
	AttemptNo         uint64           `json:"attempt_no"`
	Phase             string           `json:"phase"`
	Prepared          PreparedRequest  `json:"prepared"`
	Permit            StartPermit      `json:"permit"`
	PreparedAuthority PreparedStart    `json:"prepared_authority"`
	ControlWindowID   string           `json:"control_window_id"`
	Effect            string           `json:"effect"`
	MayApplyLater     any              `json:"may_apply_later"`
	StartedAt         string           `json:"started_at,omitempty"`
	ObservedAt        string           `json:"observed_at,omitempty"`
	FactRevision      uint64           `json:"fact_revision"`
	FactDigest        string           `json:"fact_digest,omitempty"`
	EvidenceRefs      []api.ContentRef `json:"evidence_refs"`
	ResultRef         *api.ContentRef  `json:"result_ref,omitempty"`
	Usage             []api.Amount     `json:"usage"`
	UsageFinal        bool             `json:"usage_final"`
	EffectDisputed    bool             `json:"effect_disputed"`
	ActuallyStopped   bool             `json:"actually_stopped"`
	CellCommitted     bool             `json:"cell_committed"`
}
type operationRecord struct {
	Operation         api.Operation    `json:"operation"`
	Invoke            InvokeInput      `json:"invoke"`
	Intent            *ExecutionIntent `json:"intent,omitempty"`
	Revision          uint64           `json:"revision"`
	InputDigest       string           `json:"input_digest"`
	Principal         rt.Auth          `json:"principal"`
	NewAttemptsClosed bool             `json:"new_attempts_closed"`
	ActuallyStopped   bool             `json:"actually_stopped"`
	EffectDisputed    bool             `json:"effect_disputed"`
	CancelReason      string           `json:"cancel_reason,omitempty"`
	Tombstone         bool             `json:"tombstone"`
	AttemptIDs        []string         `json:"attempt_ids"`
}
type ResourceLease struct {
	ResourceID        string   `json:"resource_id"`
	Revision          uint64   `json:"revision"`
	OwnerID           string   `json:"owner_id"`
	HolderID          string   `json:"holder_id"`
	InstanceID        string   `json:"instance_id"`
	ControlEpoch      uint64   `json:"control_epoch"`
	LeaseUntil        string   `json:"lease_until"`
	State             string   `json:"state"`
	OperationIDs      []string `json:"operation_ids"`
	InflightWrite     string   `json:"inflight_write,omitempty"`
	TakeoverRequested bool     `json:"takeover_requested"`
	ActuallyStopped   bool     `json:"actually_stopped"`
}
type Observation struct {
	ObservationID string           `json:"observation_id"`
	ResourceRef   api.ObjectRef    `json:"resource_ref"`
	InstanceID    string           `json:"instance_id"`
	ControlEpoch  uint64           `json:"control_epoch"`
	ContentRefs   []api.ContentRef `json:"content_refs"`
	TargetVersion string           `json:"target_version"`
	ObservedAt    string           `json:"observed_at"`
	ActionBefore  string           `json:"action_before"`
	Data          json.RawMessage  `json:"data"`
}

// AttemptView 披露真实阶段与原证据；不披露内部编码正文、凭据/签名缓存。
type AttemptView struct {
	AttemptID        string           `json:"attempt_id"`
	OperationID      string           `json:"operation_id"`
	Revision         uint64           `json:"revision"`
	AttemptNo        uint64           `json:"attempt_no"`
	Phase            string           `json:"phase"`
	RequestDigest    string           `json:"request_digest"`
	TargetRequestKey string           `json:"target_request_key,omitempty"`
	ResourceID       string           `json:"resource_id,omitempty"`
	ResourceEpoch    uint64           `json:"resource_epoch,omitempty"`
	ControlWindowID  string           `json:"control_window_id"`
	Effect           string           `json:"effect"`
	MayApplyLater    any              `json:"may_apply_later"`
	StartedAt        string           `json:"started_at,omitempty"`
	ObservedAt       string           `json:"observed_at,omitempty"`
	FactRevision     uint64           `json:"fact_revision"`
	EvidenceRefs     []api.ContentRef `json:"evidence_refs"`
	ResultRef        *api.ContentRef  `json:"result_ref,omitempty"`
	Usage            []api.Amount     `json:"usage"`
	UsageFinal       bool             `json:"usage_final"`
	EffectDisputed   bool             `json:"effect_disputed"`
	ActuallyStopped  bool             `json:"actually_stopped"`
	CellCommitted    bool             `json:"cell_committed"`
}

func publicAttempt(a Attempt) AttemptView {
	return AttemptView{AttemptID: a.AttemptID, OperationID: a.OperationID, Revision: a.Revision, AttemptNo: a.AttemptNo, Phase: a.Phase, RequestDigest: a.Prepared.Digest, TargetRequestKey: a.Prepared.TargetRequestKey, ResourceID: a.Prepared.ResourceID, ResourceEpoch: a.Prepared.ResourceEpoch, ControlWindowID: a.ControlWindowID, Effect: a.Effect, MayApplyLater: a.MayApplyLater, StartedAt: a.StartedAt, ObservedAt: a.ObservedAt, FactRevision: a.FactRevision, EvidenceRefs: a.EvidenceRefs, ResultRef: a.ResultRef, Usage: a.Usage, UsageFinal: a.UsageFinal, EffectDisputed: a.EffectDisputed, ActuallyStopped: a.ActuallyStopped, CellCommitted: a.CellCommitted}
}
