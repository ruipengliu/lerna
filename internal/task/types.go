// Package task 实现固定 Orchestrator 的目标、条件、准入、预算与协作责任。
// 跨 owner 的事实通过有界且可恢复的交接归并，不改写对方记录。
package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const Namespace = "task"
const (
	JobAdvance            = "task.advance"
	JobDispatchDecision   = "task.dispatch_decision"
	JobDispatchOperation  = "task.dispatch_operation"
	JobReconcileOperation = "task.reconcile_operation"
	JobCheck              = "task.check"
	JobCoverage           = "task.coverage"
	JobControl            = "task.control"
	JobBilling            = "task.billing"
	JobPublishResult      = "task.publish_result"
	JobSteer              = "task.steer"
	JobDelegation         = "task.delegation"
	JobAllocation         = "task.allocation"
	JobChildPrepare       = "task.child_prepare"
	JobChildTransfer      = "task.child_transfer"
)

// TaskPolicy 是宿主登记的不可变准确配置；不存在生产默认预算。
type TaskPolicy struct {
	PolicyRef                   api.ComponentRef `json:"policy_ref"`
	ContinuationLimit           uint64           `json:"continuation_limit"`
	RepairLimit                 uint64           `json:"repair_limit"`
	NoProgressLimit             uint64           `json:"no_progress_limit"`
	ContextRoundLimit           uint64           `json:"context_round_limit"`
	SafeAttemptLimit            uint64           `json:"safe_attempt_limit"`
	MaxRequirements             uint64           `json:"max_requirements"`
	MaxDelegations              uint64           `json:"max_delegations"`
	MaxDepth                    uint64           `json:"max_depth"`
	CostMode                    string           `json:"cost_mode"`
	BudgetLimits                []api.Amount     `json:"budget_limits"`
	MaxEvidenceStalenessSeconds uint64           `json:"max_evidence_staleness_seconds"`
	MaxDurationSeconds          uint64           `json:"max_duration_seconds"`
	InputPolicyRef              api.ComponentRef `json:"input_policy_ref"`
	RuleRegistryRef             api.ComponentRef `json:"rule_registry_ref"`
}
type AnswerSchemaDefinition struct {
	Ref    api.ComponentRef
	Schema api.Schema
}
type Config struct {
	AnswerSchemas      []AnswerSchemaDefinition
	Policies           []TaskPolicy
	Rules              []api.RuleDefinition
	MaxTasksPerSubject uint64
	MaxActiveChildren  uint64
	MaxActiveSubtree   uint64
	MaxRelations       uint64
	ControlWindow      time.Duration
	Participants       []string
}

type ContentPort interface {
	Read(context.Context, runtime.Scope, runtime.Auth, api.ContentRef) ([]byte, error)
	Publish(context.Context, runtime.Scope, string, string, []byte) (api.ContentRef, error)
}

// LocalGate 是同库事务内的受信授权及资格核验。实现不得出站或跨库读取。
// 缺少同库装配时由宿主提供已核验的有限凭据，并在这里比较当前门禁。
type LocalGate interface {
	Authorize(context.Context, runtime.Tx, runtime.Auth, string, []api.ContentRef, []api.ObjectRef) error
	Evidence(context.Context, runtime.Tx, api.Task, []api.ObjectRef, []api.ComponentRef) error
}

// EvidenceRegistration 供显式同库宿主从完整报告登记治理准确副本。
// 这些入口只在调用方完成外部取证后运行，不读取网络或Content字节。
type EvidenceRegistration interface {
	RegisterCoverage(context.Context, runtime.Tx, api.Task, api.GoalCoverage) error
	RegisterCheck(context.Context, runtime.Tx, api.Task, api.ConditionResult) error
	BindResult(context.Context, runtime.Tx, api.Task, api.Result, []api.ObjectRef) error
}

type ContextPort interface {
	Prepare(context.Context, runtime.Scope, runtime.Auth, api.Task) (PreparedDecision, error)
}
type BrainPort interface {
	Dispatch(context.Context, runtime.Scope, api.DecisionDispatchIntent, Snapshot) error
	ReadProposal(context.Context, runtime.Scope, api.DecisionDispatchIntent) (Proposal, error)
	Usage(context.Context, runtime.Scope, api.ObjectRef) (api.UsageSnapshot, error)
}
type ExecutionPort interface {
	Dispatch(context.Context, runtime.Scope, OperationIntent, api.ControlSnapshot) error
	Read(context.Context, runtime.Scope, api.ObjectRef) (api.Operation, error)
	Control(context.Context, runtime.Scope, string, api.ControlSnapshot) error
	Usage(context.Context, runtime.Scope, api.ObjectRef) (api.UsageSnapshot, error)
}
type EvidencePort interface {
	ValidateRequirements(context.Context, runtime.Scope, api.Task, api.RequirementDelta) (ValidationReport, error)
	Coverage(context.Context, runtime.Scope, api.Task) (api.GoalCoverage, error)
	Check(context.Context, runtime.Scope, api.Task, CheckRequest) (api.ConditionResult, error)
}
type CollaborationPort interface {
	CreateSession(context.Context, runtime.Scope, ChildHandle) (api.ObjectRef, error)
	Create(context.Context, runtime.Scope, Delegation, Allocation) (DelegationFact, error)
	Read(context.Context, runtime.Scope, Delegation) (DelegationFact, error)
	Control(context.Context, runtime.Scope, Delegation, string) error
	CloseAllocation(context.Context, runtime.Scope, Allocation) error
	ReadAllocation(context.Context, runtime.Scope, api.ObjectRef) (Allocation, error)
	ReadClosure(context.Context, runtime.Scope, api.ObjectRef) (api.AllocationClosure, error)
	Transfer(context.Context, runtime.Scope, Transfer) error
}
type Ports struct {
	Context       ContextPort
	Content       ContentPort
	Gate          LocalGate
	Brain         BrainPort
	Execution     ExecutionPort
	Evidence      EvidencePort
	Collaboration CollaborationPort
}

type SubmitInput struct {
	OrchestratorID        string                     `json:"orchestrator_id"`
	GoalRef               api.ContentRef             `json:"goal_ref"`
	PolicyRef             api.ComponentRef           `json:"policy_ref"`
	Deadline              string                     `json:"deadline"`
	Budget                []api.Amount               `json:"budget"`
	DelegationContext     *api.DelegationContext     `json:"delegation_context,omitempty"`
	AcceptanceRef         *api.ObjectRef             `json:"acceptance_ref,omitempty"`
	RequirementCandidates []api.RequirementCandidate `json:"requirement_candidates,omitempty"`
	SourceSubmissionRef   *api.ObjectRef             `json:"source_submission_ref,omitempty"`
}
type TaskOutput struct {
	TaskRef           api.ObjectRef         `json:"task_ref"`
	GoalRevision      uint64                `json:"goal_revision"`
	ControlRevision   uint64                `json:"control_revision"`
	RequirementsState string                `json:"requirements_state"`
	Status            string                `json:"status"`
	ControlTargets    api.CollectionSummary `json:"control_targets"`
	Budget            []api.BudgetBalance   `json:"budget"`
}
type ControlInput struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}
type ReviseInput struct {
	TaskID           string         `json:"task_id"`
	BaseGoalRevision uint64         `json:"base_goal_revision"`
	GoalRef          api.ContentRef `json:"goal_ref"`
	SourceRef        api.ObjectRef  `json:"source_ref"`
}
type SteerInput struct {
	TaskID              string         `json:"task_id"`
	BaseGoalRevision    uint64         `json:"base_goal_revision"`
	AmendmentRef        api.ContentRef `json:"amendment_ref"`
	SourceSubmissionRef api.ObjectRef  `json:"source_submission_ref"`
	PrepareDeadline     string         `json:"prepare_deadline"`
}
type InputAnswer struct {
	TaskID       string         `json:"task_id"`
	RequestRef   api.ObjectRef  `json:"request_ref"`
	GoalRevision uint64         `json:"goal_revision"`
	AnswerRef    api.ContentRef `json:"answer_ref"`
}
type InputOutput struct {
	TaskRef            api.ObjectRef  `json:"task_ref"`
	RequestRef         api.ObjectRef  `json:"request_ref"`
	ConsumedRequestRef *api.ObjectRef `json:"consumed_request_ref,omitempty"`
	State              string         `json:"state"`
}
type AcceptInput struct {
	TaskID         string         `json:"task_id"`
	RequestRef     api.ObjectRef  `json:"request_ref"`
	GoalRevision   uint64         `json:"goal_revision"`
	CandidateRef   api.ContentRef `json:"candidate_ref"`
	LimitationsRef api.ContentRef `json:"limitations_ref"`
}
type AcceptOutput struct {
	TaskRef  api.ObjectRef `json:"task_ref"`
	CheckRef api.ObjectRef `json:"check_ref"`
}
type BudgetInput struct {
	TaskID    string         `json:"task_id"`
	Limits    []api.Amount   `json:"limits"`
	ReasonRef api.ContentRef `json:"reason_ref"`
}
type AttachInput struct {
	TaskID         string             `json:"task_id"`
	GoalRevision   uint64             `json:"goal_revision"`
	RequirementRef api.RequirementRef `json:"requirement_ref"`
	ArtifactRef    api.ContentRef     `json:"artifact_ref"`
	EvidenceRefs   []api.ContentRef   `json:"evidence_refs"`
}
type AttachOutput struct {
	TaskRef         api.ObjectRef `json:"task_ref"`
	CheckRequestRef api.ObjectRef `json:"check_request_ref"`
}
type BillingInput struct {
	TaskID        string        `json:"task_id"`
	SourceKind    string        `json:"source_kind"`
	SourceRef     api.ObjectRef `json:"source_ref"`
	UsageRevision uint64        `json:"usage_revision"`
	UsageDigest   string        `json:"usage_digest"`
}
type BillingOutput struct {
	SourceRef    api.ObjectRef `json:"source_ref"`
	WorkRevision uint64        `json:"work_revision"`
}
type ReadInput struct {
	Revision uint64 `json:"revision,omitempty"`
}
type ResultInput struct {
	ResultRef *api.ObjectRef `json:"result_ref,omitempty"`
}
type ResultOutput struct {
	Result      api.Result      `json:"result"`
	Publication string          `json:"publication"`
	ContentRef  *api.ContentRef `json:"content_ref,omitempty"`
	Notices     []string        `json:"notices"`
}
type TaskListInput struct {
	Limit  uint64 `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
	Status string `json:"status,omitempty"`
}
type ControlWindowInput struct {
	TaskID                  string        `json:"task_id"`
	ExpectedGoalRevision    uint64        `json:"expected_goal_revision"`
	ExpectedControlRevision uint64        `json:"expected_control_revision"`
	ReceiverID              string        `json:"receiver_id"`
	OperationRef            api.ObjectRef `json:"operation_ref"`
}

type Snapshot = api.Snapshot
type PreparedDecision struct {
	Snapshot     Snapshot
	SnapshotRef  api.ContentRef
	BrainOwnerID string
	CostBound    []api.Amount
	DecisionID   string
	CommandID    string
}
type PreparedAction struct {
	OperationID          string               `json:"operation_id"`
	ExecutorID           string               `json:"executor_id"`
	CapabilityRef        api.ComponentRef     `json:"capability_ref"`
	BindingRef           api.ObjectRef        `json:"binding_ref"`
	InstallLockRef       api.ComponentRef     `json:"install_lock_ref"`
	ArgumentsRef         api.ContentRef       `json:"arguments_ref"`
	ResourcesRef         api.ContentRef       `json:"resources_ref"`
	RequirementRefs      []api.RequirementRef `json:"requirement_refs"`
	UseIntentRefs        []api.ObjectRef      `json:"use_intent_refs"`
	CostBound            []api.Amount         `json:"cost_bound"`
	LogicalStepKey       string               `json:"logical_step_key"`
	ProcessedSourceRefs  []api.ContentRef     `json:"processed_source_refs"`
	DisclosedSourceRefs  []api.ContentRef     `json:"disclosed_source_refs"`
	ResourceKeys         []string             `json:"resource_keys"`
	Independent          bool                 `json:"independent"`
	SafeRequirementCheck bool                 `json:"safe_requirement_check"`
	CommandID            string               `json:"command_id"`
}
type OperationIntent struct {
	PreparedAction
	TaskRef             api.ObjectRef `json:"task_ref"`
	GoalRevision        uint64        `json:"goal_revision"`
	ControlRevision     uint64        `json:"control_revision"`
	AdmissionSourceKind string        `json:"admission_source_kind"`
	AdmissionSourceRef  api.ObjectRef `json:"admission_source_ref"`
	SourcePosition      string        `json:"source_position"`
	AdmissionPurpose    string        `json:"admission_purpose"`
	IntentHash          string        `json:"intent_hash"`
	ReservationRef      api.ObjectRef `json:"reservation_ref"`
	CommandRef          api.ObjectRef `json:"command_ref"`
	Deadline            string        `json:"deadline"`
}
type Proposal struct {
	DecisionID       string                `json:"decision_id"`
	Kind             string                `json:"kind"`
	ReasonRef        api.ContentRef        `json:"reason_ref"`
	RequirementDelta *api.RequirementDelta `json:"requirement_delta,omitempty"`
	Actions          []PreparedAction      `json:"actions,omitempty"`
	ArtifactRefs     []api.ContentRef      `json:"artifact_refs,omitempty"`
	Limitations      []string              `json:"limitations,omitempty"`
	FailureReason    string                `json:"failure_reason,omitempty"`
	InputRequest     *api.InputRequest     `json:"input_request,omitempty"`
	ContextRefs      []api.ContentRef      `json:"context_refs,omitempty"`
}

// ValidationReport 固定可重放语义键；自由文本标准须以准确参数内容引用参与键。
type ValidationReport struct {
	ReportRef    api.ContentRef `json:"report_ref"`
	SemanticKeys []string       `json:"semantic_keys"`
	Valid        bool           `json:"valid"`
	ReasonCodes  []string       `json:"reason_codes"`
}
type Consumption struct {
	TaskID                  string         `json:"task_id"`
	DecisionID              string         `json:"decision_id"`
	SnapshotRef             api.ContentRef `json:"snapshot_ref"`
	ExpectedGoalRevision    uint64         `json:"expected_goal_revision"`
	ExpectedControlRevision uint64         `json:"expected_control_revision"`
	Outcome                 string         `json:"outcome"`
	AdoptionID              string         `json:"adoption_id,omitempty"`
	AdmittedOperationIDs    []string       `json:"admitted_operation_ids"`
	ReasonRef               api.ContentRef `json:"reason_ref"`
	ReasonCodes             []string       `json:"reason_codes"`
	DecidedAt               string         `json:"decided_at"`
}
type CheckRequest struct {
	CheckID   string      `json:"check_id"`
	Revision  uint64      `json:"revision"`
	Input     AttachInput `json:"input"`
	State     string      `json:"state"`
	CreatorID string      `json:"creator_id"`
}
type CompleteInput struct {
	TaskID               string
	ExpectedGoalRevision uint64
	ArtifactRefs         []api.ContentRef
	CheckRefs            []api.ObjectRef
	Limitations          []string
}
type ReservationUnit struct {
	Unit              string `json:"unit"`
	OriginalReserved  string `json:"original_reserved"`
	RemainingReserved string `json:"remaining_reserved"`
	AppliedCumulative string `json:"applied_cumulative"`
}
type Reservation struct {
	ReservationID        string            `json:"reservation_id"`
	Revision             uint64            `json:"revision"`
	TaskID               string            `json:"task_id"`
	SourceKind           string            `json:"source_kind"`
	SourceRef            api.ObjectRef     `json:"source_ref"`
	BindingState         string            `json:"binding_state"`
	AppliedUsageRevision uint64            `json:"applied_usage_revision"`
	UsageDigest          string            `json:"usage_digest"`
	State                string            `json:"state"`
	Units                []ReservationUnit `json:"units"`
	Incidents            []string          `json:"incidents"`
}
type Allocation struct {
	AllocationID  string         `json:"allocation_id"`
	Revision      uint64         `json:"revision"`
	ParentTaskRef api.ObjectRef  `json:"parent_task_ref"`
	ReceiverID    string         `json:"receiver_id"`
	Limits        []api.Amount   `json:"limits"`
	Deadline      string         `json:"deadline"`
	CommandRef    api.ObjectRef  `json:"command_ref"`
	State         string         `json:"state"`
	ClosureRef    *api.ObjectRef `json:"closure_ref,omitempty"`
}
type IncomingAllocation struct {
	AllocationID  string         `json:"allocation_id"`
	Revision      uint64         `json:"revision"`
	ParentOwner   string         `json:"parent_owner"`
	ReceiverID    string         `json:"receiver_id"`
	ParentTaskRef api.ObjectRef  `json:"parent_task_ref"`
	TaskRef       *api.ObjectRef `json:"task_ref,omitempty"`
	Limits        []api.Amount   `json:"limits"`
	Gate          string         `json:"gate"`
	UsageRevision uint64         `json:"usage_revision"`
	Cumulative    []api.Amount   `json:"cumulative"`
	ClosedAt      string         `json:"closed_at,omitempty"`
	ClosureRef    *api.ObjectRef `json:"closure_ref,omitempty"`
}
type AllocateInput struct {
	AllocationID  string        `json:"allocation_id"`
	ParentTaskRef api.ObjectRef `json:"parent_task_ref"`
	ReceiverID    string        `json:"receiver_id"`
	Limits        []api.Amount  `json:"limits"`
	Deadline      string        `json:"deadline"`
}
type AllocationCloseInput struct {
	AllocationRef api.ObjectRef `json:"allocation_ref"`
	ParentTaskRef api.ObjectRef `json:"parent_task_ref"`
	Reason        string        `json:"reason"`
}
type SettleInput struct {
	AllocationRef api.ObjectRef `json:"allocation_ref"`
	ClosureRef    api.ObjectRef `json:"closure_ref"`
}
type AllocationOutput struct {
	AllocationRef api.ObjectRef `json:"allocation_ref"`
	State         string        `json:"state"`
}
type AdjustmentInput struct {
	AdjustmentID          string           `json:"adjustment_id"`
	OriginalSourceRef     api.ObjectRef    `json:"original_source_ref"`
	ProviderAdjustmentKey string           `json:"provider_adjustment_key"`
	Kind                  string           `json:"kind"`
	Unit                  string           `json:"unit"`
	Amount                string           `json:"amount"`
	EvidenceRefs          []api.ContentRef `json:"evidence_refs"`
}
type BillingAdjustment struct {
	AdjustmentInput
	Revision   uint64 `json:"revision"`
	State      string `json:"state"`
	VerifiedBy string `json:"verified_by,omitempty"`
	VerifiedAt string `json:"verified_at,omitempty"`
}
type AdjustmentOutput struct {
	AdjustmentRef api.ObjectRef `json:"adjustment_ref"`
	State         string        `json:"state"`
}
type BudgetReadOutput struct {
	Budget         []api.BudgetBalance `json:"budget"`
	Reservations   []Reservation       `json:"reservations"`
	AccountingOpen bool                `json:"accounting_open"`
	GrossSpent     []api.Amount        `json:"gross_spent"`
	CreditTotal    []api.Amount        `json:"credit_total"`
	NetCost        []api.Amount        `json:"net_cost"`
}

type DelegateInput struct {
	DelegationID       string           `json:"delegation_id"`
	ParentTaskRef      api.ObjectRef    `json:"parent_task_ref"`
	ParentGoalRevision uint64           `json:"parent_goal_revision"`
	GoalRef            api.ContentRef   `json:"goal_ref"`
	InputRefs          []api.ContentRef `json:"input_refs"`
	AgentBindingRef    api.ObjectRef    `json:"agent_binding_ref"`
	PermissionRefs     []api.ObjectRef  `json:"permission_refs"`
	Budget             []api.Amount     `json:"budget"`
	Deadline           string           `json:"deadline"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	ReceiverID         string           `json:"receiver_id"`
	Internal           bool             `json:"internal"`
}
type Delegation struct {
	DelegateInput
	Revision         uint64          `json:"revision"`
	CreationKey      string          `json:"creation_key"`
	CommandRef       api.ObjectRef   `json:"command_ref"`
	AllocationRef    api.ObjectRef   `json:"allocation_ref"`
	AncestorTaskRefs []api.ObjectRef `json:"ancestor_task_refs"`
	ChildTaskRef     *api.ObjectRef  `json:"child_task_ref,omitempty"`
	Phase            string          `json:"phase"`
	GoalWorkClosed   bool            `json:"goal_work_closed"`
	EffectsClosed    bool            `json:"effects_closed"`
	ClosureRef       *api.ObjectRef  `json:"closure_ref,omitempty"`
	CloseRequested   bool            `json:"close_requested"`
	Sent             bool            `json:"sent"`
	SourceRevision   uint64          `json:"source_revision"`
	SourceDigest     string          `json:"source_digest"`
}
type DelegationFact struct {
	DelegationID    string             `json:"delegation_id"`
	Revision        uint64             `json:"revision"`
	ChildTaskRef    *api.ObjectRef     `json:"child_task_ref,omitempty"`
	GoalWorkClosed  bool               `json:"goal_work_closed"`
	EffectsClosed   bool               `json:"effects_closed"`
	TransfersClosed bool               `json:"transfers_closed"`
	UsageFinal      bool               `json:"usage_final"`
	ClosureRef      *api.ObjectRef     `json:"closure_ref,omitempty"`
	Usage           *api.UsageSnapshot `json:"usage,omitempty"`
	Gaps            []string           `json:"gaps"`
}
type DelegationClosure struct {
	DelegationID         string           `json:"delegation_id"`
	Revision             uint64           `json:"revision"`
	GoalWorkClosed       bool             `json:"goal_work_closed"`
	EffectsClosed        bool             `json:"effects_closed"`
	AllocationClosureRef api.ObjectRef    `json:"allocation_closure_ref"`
	TransfersClosed      bool             `json:"transfers_closed"`
	ProofRefs            []api.ContentRef `json:"proof_refs"`
	ClosedAt             string           `json:"closed_at"`
}
type DelegateOutput struct {
	DelegationRef api.ObjectRef  `json:"delegation_ref"`
	AllocationRef api.ObjectRef  `json:"allocation_ref"`
	ChildTaskRef  *api.ObjectRef `json:"child_task_ref,omitempty"`
}

type ChildCreateInput struct {
	ChildID          string           `json:"child_id"`
	SessionOwnerID   string           `json:"session_owner_id"`
	SessionConfigRef api.ComponentRef `json:"session_config_ref"`
	AgentBindingRef  api.ObjectRef    `json:"agent_binding_ref"`
	InstallLockRef   api.ComponentRef `json:"install_lock_ref"`
	AccessScopeRef   api.ContentRef   `json:"access_scope_ref"`
	PrepareDeadline  string           `json:"prepare_deadline"`
}
type ChildHandle struct {
	ChildCreateInput
	Revision            uint64         `json:"revision"`
	SubjectID           string         `json:"subject_id"`
	State               string         `json:"state"`
	ChildSessionRef     *api.ObjectRef `json:"child_session_ref,omitempty"`
	SessionCommandRef   api.ObjectRef  `json:"session_command_ref"`
	ActiveDelegationRef *api.ObjectRef `json:"active_delegation_ref,omitempty"`
	CloseCancelActive   bool           `json:"close_cancel_active"`
}
type ChildSendInput struct {
	ChildID                     string          `json:"child_id"`
	Mode                        string          `json:"mode"`
	ExpectedActiveDelegationRef *api.ObjectRef  `json:"expected_active_delegation_ref,omitempty"`
	Delegation                  *DelegateInput  `json:"delegation,omitempty"`
	HistoryCutoff               uint64          `json:"history_cutoff,omitempty"`
	DelegationRef               *api.ObjectRef  `json:"delegation_ref,omitempty"`
	ParentGoalRevision          uint64          `json:"parent_goal_revision,omitempty"`
	Kind                        string          `json:"kind,omitempty"`
	RequestRef                  *api.ObjectRef  `json:"request_ref,omitempty"`
	AnswerRef                   *api.ContentRef `json:"answer_ref,omitempty"`
	ChildGoalRevision           uint64          `json:"child_goal_revision,omitempty"`
	ContentRef                  *api.ContentRef `json:"content_ref,omitempty"`
}
type ChildOutput struct {
	ChildRef        api.ObjectRef  `json:"child_ref"`
	ChildSessionRef *api.ObjectRef `json:"child_session_ref,omitempty"`
	DelegationRef   *api.ObjectRef `json:"delegation_ref,omitempty"`
}
type ChildWaitInput struct {
	ChildID       string        `json:"child_id"`
	DelegationRef api.ObjectRef `json:"delegation_ref"`
	WaitFor       string        `json:"wait_for"`
	TimeoutMS     uint64        `json:"timeout_ms"`
}
type ChildWaitOutput struct {
	ObservedRevision uint64     `json:"observed_revision"`
	ConditionMet     bool       `json:"condition_met"`
	Delegation       Delegation `json:"delegation"`
	Gaps             []string   `json:"gaps"`
}
type ChildCloseInput struct {
	ChildID      string `json:"child_id"`
	CancelActive bool   `json:"cancel_active"`
	Reason       string `json:"reason"`
}
type CloseTarget struct {
	DelegationRef api.ObjectRef `json:"delegation_ref"`
	State         string        `json:"state"`
}
type ChildCloseOutput struct {
	ChildRef api.ObjectRef `json:"child_ref"`
	Targets  []CloseTarget `json:"targets"`
}
type Transfer struct {
	TransferID   string         `json:"transfer_id"`
	Revision     uint64         `json:"revision"`
	DelegationID string         `json:"delegation_id"`
	Kind         string         `json:"kind"`
	CommandRef   api.ObjectRef  `json:"command_ref"`
	State        string         `json:"state"`
	Input        ChildSendInput `json:"input"`
}

// 只读关系投影保持源的版本和摘要；unknown 不能改成 closed。
type relation struct {
	ID             string        `json:"id"`
	Revision       uint64        `json:"revision"`
	TaskID         string        `json:"task_id"`
	Kind           string        `json:"kind"`
	Ref            api.ObjectRef `json:"ref"`
	Purpose        string        `json:"purpose"`
	Closed         bool          `json:"closed"`
	MayApplyLater  bool          `json:"may_apply_later"`
	Effect         string        `json:"effect"`
	SourceRevision uint64        `json:"source_revision"`
	SourceDigest   string        `json:"source_digest"`
	ResourceKeys   []string      `json:"resource_keys"`
}
type taskState struct {
	Task                 api.Task             `json:"task"`
	SubjectID            string               `json:"subject_id"`
	Policy               TaskPolicy           `json:"policy"`
	SourceRefs           []api.SourceEvidence `json:"source_refs"`
	InitialGoalRef       api.ContentRef       `json:"initial_goal_ref"`
	Amendments           []api.ContentRef     `json:"amendments"`
	PendingGoalCommand   string               `json:"pending_goal_command,omitempty"`
	ParentTaskID         string               `json:"parent_task_id,omitempty"`
	IncomingAllocationID string               `json:"incoming_allocation_id,omitempty"`
	Ancestors            []string             `json:"ancestors"`
	Continuations        uint64               `json:"continuations"`
	Repairs              uint64               `json:"repairs"`
	NoProgress           uint64               `json:"no_progress"`
	DelegationsCreated   uint64               `json:"delegations_created"`
	ContextRounds        uint64               `json:"context_rounds"`
	SemanticKeys         []string             `json:"semantic_keys"`
	CurrentArtifactRefs  []api.ContentRef     `json:"current_artifact_refs"`
	RelationRevision     uint64               `json:"relation_revision"`
	ControlTargets       []string             `json:"control_targets"`
	Credits              []api.Amount         `json:"credits"`
}
type decisionState struct {
	Intent        api.DecisionDispatchIntent `json:"intent"`
	Snapshot      Snapshot                   `json:"snapshot"`
	ReservationID string                     `json:"reservation_id"`
	Sent          bool                       `json:"sent"`
	Consumed      bool                       `json:"consumed"`
	Revision      uint64                     `json:"revision"`
}
type resultPublication struct {
	ResultRef  api.ObjectRef   `json:"result_ref"`
	Revision   uint64          `json:"revision"`
	UploadID   string          `json:"upload_id"`
	State      string          `json:"state"`
	ContentRef *api.ContentRef `json:"content_ref,omitempty"`
	Notices    []string        `json:"notices"`
}
type pendingSteer struct {
	Input     SteerInput `json:"input"`
	Revision  uint64     `json:"revision"`
	CommandID string     `json:"command_id"`
	SubjectID string     `json:"subject_id"`
	UploadID  string     `json:"upload_id"`
	State     string     `json:"state"`
}
type candidatePending struct {
	TaskID     string               `json:"task_id"`
	Revision   uint64               `json:"revision"`
	Source     api.ObjectRef        `json:"source"`
	Delta      api.RequirementDelta `json:"delta"`
	SourceKind string               `json:"source_kind"`
}
type internalDocument struct {
	Data json.RawMessage `json:"data"`
}

// RequestView/Closure 是消费方内部类型化接口，查询不改变原请求或Goal。
type InputRequestView struct {
	RequestRef   api.ObjectRef    `json:"request_ref"`
	Request      api.InputRequest `json:"request"`
	AnswerSchema json.RawMessage  `json:"answer_schema"`
}
type InputRequestListInput struct {
	TaskID string `json:"task_id"`
	Limit  uint64 `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}
type ClosureView struct {
	TaskRef        api.ObjectRef
	GoalWorkClosed bool
	EffectsClosed  bool
	AccountingOpen bool
	ProofRef       api.ContentRef
}
type pendingInput struct {
	Revision  uint64       `json:"revision"`
	CommandID string       `json:"command_id"`
	Input     InputAnswer  `json:"input"`
	Auth      runtime.Auth `json:"auth"`
	State     string       `json:"state"`
}
