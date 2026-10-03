package governance

import "github.com/ruipengliu/lerna/api"

type RefInput struct {
	Ref api.ObjectRef `json:"ref"`
}
type IDInput struct {
	ID string `json:"id"`
}
type Empty struct{}
type StateOutput struct {
	Ref   api.ObjectRef `json:"ref"`
	State string        `json:"state"`
}

type GrantRecord struct {
	Grant        api.Grant    `json:"grant"`
	OnceConsumed bool         `json:"once_consumed"`
	Spent        []api.Amount `json:"spent"`
	Reserved     []api.Amount `json:"reserved"`
}
type GrantUsage struct {
	GrantID      string       `json:"grant_id"`
	Revision     uint64       `json:"revision"`
	OnceConsumed bool         `json:"once_consumed"`
	Spent        []api.Amount `json:"spent"`
	Reserved     []api.Amount `json:"reserved"`
}
type ConfirmationView struct {
	RequestID             string           `json:"request_id"`
	Revision              uint64           `json:"revision"`
	OriginalCommandID     string           `json:"original_command_id"`
	IntentHash            string           `json:"intent_hash"`
	PreviewRefs           []api.ContentRef `json:"preview_refs"`
	ExpiresAt             string           `json:"expires_at"`
	State                 string           `json:"state"`
	Challenge             string           `json:"challenge"`
	TrustedUserSessionRef api.ObjectRef    `json:"trusted_user_session_ref"`
	ConsumedBy            string           `json:"consumed_by,omitempty"`
	ConsumedAt            string           `json:"consumed_at,omitempty"`
	DecidedBy             string           `json:"decided_by,omitempty"`
	DecidedAt             string           `json:"decided_at,omitempty"`
}
type GrantIssue struct {
	Grant                 api.Grant        `json:"grant"`
	PreviewRefs           []api.ContentRef `json:"preview_refs"`
	ConfirmationExpiresAt string           `json:"confirmation_expires_at"`
	ParentGrantRefs       []api.ObjectRef  `json:"parent_grant_refs"`
}
type GrantRevoke struct {
	GrantRef              api.ObjectRef    `json:"grant_ref"`
	PreviewRefs           []api.ContentRef `json:"preview_refs"`
	ConfirmationExpiresAt string           `json:"confirmation_expires_at"`
}
type ConfirmedOutput struct {
	Ref             *api.ObjectRef `json:"ref,omitempty"`
	State           string         `json:"state"`
	ConfirmationRef *api.ObjectRef `json:"confirmation_ref,omitempty"`
	Reason          string         `json:"reason,omitempty"`
}
type ConfirmationRecord struct {
	Confirmation         api.Confirmation `json:"confirmation"`
	SubjectID            string           `json:"subject_id"`
	CredentialGeneration uint64           `json:"credential_generation"`
	PendingCommandID     string           `json:"pending_command_id"`
}
type ConfirmationDecision struct {
	RequestID       string           `json:"request_id"`
	RequestRevision uint64           `json:"request_revision"`
	Decision        string           `json:"decision"`
	Challenge       string           `json:"challenge"`
	PreviewRefs     []api.ContentRef `json:"preview_refs"`
}
type PendingConfirmation struct {
	Command              api.Command `json:"command"`
	SubjectID            string      `json:"subject_id"`
	CredentialGeneration uint64      `json:"credential_generation"`
	Roles                []string    `json:"roles"`
	ConfirmationID       string      `json:"confirmation_id"`
}

type UseRequest struct {
	UseID          string          `json:"use_id"`
	SubjectRef     api.ObjectRef   `json:"subject_ref"`
	TargetRef      api.ObjectRef   `json:"target_ref"`
	TargetKind     string          `json:"target_kind"`
	IntentHash     string          `json:"intent_hash"`
	GrantRefs      []api.ObjectRef `json:"grant_refs"`
	RequestedUnits []api.Amount    `json:"requested_units"`
	Resources      []string        `json:"resources"`
	Actions        []string        `json:"actions"`
	Recipient      string          `json:"recipient"`
	Location       string          `json:"location"`
	Purposes       []string        `json:"purposes"`
	StartBefore    string          `json:"start_before"`
}
type UseReceipt struct {
	UseID         string          `json:"use_id"`
	SubjectRef    api.ObjectRef   `json:"subject_ref"`
	TargetRef     api.ObjectRef   `json:"target_ref"`
	TargetKind    string          `json:"target_kind"`
	IntentHash    string          `json:"intent_hash"`
	RequestDigest string          `json:"request_digest"`
	GrantRefs     []api.ObjectRef `json:"grant_refs"`
	Decision      string          `json:"decision"`
	Reason        string          `json:"reason,omitempty"`
	Reserved      []api.Amount    `json:"reserved"`
	CostBound     []api.Amount    `json:"cost_bound"`
	Recipient     string          `json:"recipient"`
	Location      string          `json:"location"`
	Purposes      []string        `json:"purposes"`
	IssuedAt      string          `json:"issued_at"`
	StartBefore   string          `json:"start_before"`
	Proof         string          `json:"proof,omitempty"`
	ProofRef      *api.ContentRef `json:"proof_ref,omitempty"`
}
type UseSettlement struct {
	UseID             string           `json:"use_id"`
	Revision          uint64           `json:"revision"`
	CumulativeUsage   []api.Amount     `json:"cumulative_usage"`
	SpendingClosed    bool             `json:"spending_closed"`
	UsageFinal        bool             `json:"usage_final"`
	SourceRefs        []api.ObjectRef  `json:"source_refs"`
	EvidenceRefs      []api.ContentRef `json:"evidence_refs"`
	RemainingReserved []api.Amount     `json:"remaining_reserved"`
	SourceRevision    uint64           `json:"source_revision"`
	SourceDigest      string           `json:"source_digest"`
}
type SettleRequest struct {
	UseID string            `json:"use_id"`
	Usage api.UsageSnapshot `json:"usage"`
}
type SettlePending struct {
	ID        string        `json:"id"`
	CommandID string        `json:"command_id"`
	Request   SettleRequest `json:"request"`
}

type AcceptanceScope struct {
	TaskRefs                 []api.ObjectRef    `json:"task_refs"`
	CapabilityRefs           []api.ComponentRef `json:"capability_refs"`
	Units                    []string           `json:"units"`
	ReservationMethod        string             `json:"reservation_method"`
	Budget                   []api.Amount       `json:"budget"`
	NonHardLimitAcknowledged bool               `json:"non_hard_limit_acknowledged"`
}
type PolicyAcceptance struct {
	AcceptanceID    string           `json:"acceptance_id"`
	Revision        uint64           `json:"revision"`
	SubjectRef      api.ObjectRef    `json:"subject_ref"`
	PolicyRef       api.ComponentRef `json:"policy_ref"`
	ScopeRef        api.ContentRef   `json:"scope_ref"`
	Scope           AcceptanceScope  `json:"scope"`
	ExplanationRef  api.ContentRef   `json:"explanation_ref"`
	ConfirmationRef api.ObjectRef    `json:"confirmation_ref"`
	ExpiresAt       string           `json:"expires_at"`
	State           string           `json:"state"`
}
type AcceptanceCreate struct {
	AcceptanceID   string           `json:"acceptance_id"`
	PolicyRef      api.ComponentRef `json:"policy_ref"`
	ScopeRef       api.ContentRef   `json:"scope_ref"`
	Scope          AcceptanceScope  `json:"scope"`
	ExplanationRef api.ContentRef   `json:"explanation_ref"`
	ExpiresAt      string           `json:"expires_at"`
	PreviewRefs    []api.ContentRef `json:"preview_refs"`
}
type AcceptanceCheck struct {
	AcceptanceRef api.ObjectRef    `json:"acceptance_ref"`
	SubjectRef    api.ObjectRef    `json:"subject_ref"`
	PolicyRef     api.ComponentRef `json:"policy_ref"`
	TaskRef       api.ObjectRef    `json:"task_ref"`
	CapabilityRef api.ComponentRef `json:"capability_ref"`
	Requested     []api.Amount     `json:"requested"`
	Mode          string           `json:"mode"`
}
type GrantLease struct {
	LeaseID        string          `json:"lease_id"`
	Revision       uint64          `json:"revision"`
	EndpointID     string          `json:"endpoint_id"`
	InstanceID     string          `json:"instance_id"`
	GrantRefs      []api.ObjectRef `json:"grant_refs"`
	Scope          UseRequest      `json:"scope"`
	Limits         []api.Amount    `json:"limits"`
	ExpiresAt      string          `json:"expires_at"`
	State          string          `json:"state"`
	Cumulative     []api.Amount    `json:"cumulative"`
	Reserved       []api.Amount    `json:"reserved"`
	UsageRevision  uint64          `json:"usage_revision"`
	ClosureRef     *api.ObjectRef  `json:"closure_ref,omitempty"`
	SpendingClosed bool            `json:"spending_closed"`
	UsageFinal     bool            `json:"usage_final"`
}
type LeaseAllocate struct {
	LeaseID    string       `json:"lease_id"`
	EndpointID string       `json:"endpoint_id"`
	InstanceID string       `json:"instance_id"`
	Scope      UseRequest   `json:"scope"`
	Limits     []api.Amount `json:"limits"`
	ExpiresAt  string       `json:"expires_at"`
	CostMode   string       `json:"cost_mode"`
}
type LeaseReport struct {
	LeaseRef   api.ObjectRef     `json:"lease_ref"`
	EndpointID string            `json:"endpoint_id"`
	InstanceID string            `json:"instance_id"`
	Usage      api.UsageSnapshot `json:"usage"`
	ClosureRef api.ObjectRef     `json:"closure_ref"`
}

type RuleDefinition struct {
	ComponentRef             api.ComponentRef `json:"component_ref"`
	Kind                     string           `json:"kind"`
	Predicate                string           `json:"predicate"`
	AllowedBasis             []string         `json:"allowed_basis"`
	AllowUserAcceptance      bool             `json:"allow_user_acceptance"`
	RiskClass                string           `json:"risk_class"`
	MaxObservationAgeSeconds uint64           `json:"max_observation_age_seconds"`
	Calibrated               bool             `json:"calibrated"`
}
type ConditionCheck struct {
	CheckID             string           `json:"check_id"`
	Revision            uint64           `json:"revision"`
	AuthorityID         string           `json:"authority_id"`
	TaskID              string           `json:"task_id"`
	GoalRevision        uint64           `json:"goal_revision"`
	RequirementID       string           `json:"requirement_id"`
	RequirementRevision uint64           `json:"requirement_revision"`
	ArtifactRef         api.ContentRef   `json:"artifact_ref"`
	ScopeRef            api.ContentRef   `json:"scope_ref"`
	ObservedAt          string           `json:"observed_at,omitempty"`
	CheckedAt           string           `json:"checked_at"`
	RuleRef             api.ComponentRef `json:"rule_ref"`
	EvaluatorRef        api.ComponentRef `json:"evaluator_ref"`
	ConfigRef           api.ComponentRef `json:"config_ref"`
	InstallLockRef      api.ComponentRef `json:"install_lock_ref"`
	OperationRef        *api.ObjectRef   `json:"operation_ref,omitempty"`
	DependentCheckRefs  []api.ObjectRef  `json:"dependent_check_refs"`
	Verdict             string           `json:"verdict"`
	Basis               string           `json:"basis"`
	ReportRef           api.ContentRef   `json:"report_ref"`
	EvidenceRefs        []api.ContentRef `json:"evidence_refs"`
}
type EvidenceGate struct {
	ID                string           `json:"id"`
	Revision          uint64           `json:"revision"`
	ImplementationRef api.ComponentRef `json:"implementation_ref"`
	AuthorityEpoch    uint64           `json:"authority_epoch"`
}
type AuthorityHead struct {
	ID         string `json:"id"`
	Revision   uint64 `json:"revision"`
	Epoch      uint64 `json:"epoch"`
	Cursor     uint64 `json:"cursor"`
	HolderHead uint64 `json:"holder_head"`
}
type Defect struct {
	DefectID       string           `json:"defect_id"`
	RuleRef        api.ComponentRef `json:"rule_ref"`
	EvaluatorRef   api.ComponentRef `json:"evaluator_ref"`
	ScopeRef       api.ContentRef   `json:"scope_ref"`
	EvidenceRef    api.ContentRef   `json:"evidence_ref"`
	RegisteredAt   string           `json:"registered_at"`
	Cursor         uint64           `json:"cursor"`
	AuthorityEpoch uint64           `json:"authority_epoch"`
	HolderCutoff   uint64           `json:"holder_cutoff"`
}
type DefectRegister struct {
	DefectID     string           `json:"defect_id"`
	RuleRef      api.ComponentRef `json:"rule_ref"`
	EvaluatorRef api.ComponentRef `json:"evaluator_ref"`
	ScopeRef     api.ContentRef   `json:"scope_ref"`
	EvidenceRef  api.ContentRef   `json:"evidence_ref"`
}
type DefectOutput struct {
	DefectRef    api.ObjectRef `json:"defect_ref"`
	GateRevision uint64        `json:"gate_revision"`
	ChangeHead   uint64        `json:"change_head"`
}
type EvidenceHolder struct {
	HolderID           string          `json:"holder_id"`
	Revision           uint64          `json:"revision"`
	ConsumerTaskRef    api.ObjectRef   `json:"consumer_task_ref"`
	CheckRef           api.ObjectRef   `json:"check_ref"`
	ReportHash         string          `json:"report_hash"`
	DependencyDigest   string          `json:"dependency_digest"`
	DependencyRefs     []api.ObjectRef `json:"dependency_refs"`
	ScopeRef           api.ContentRef  `json:"scope_ref"`
	AuthorityEpoch     uint64          `json:"authority_epoch"`
	RegistrationCursor uint64          `json:"registration_cursor"`
	RegistrationIndex  uint64          `json:"registration_index"`
	LastAckedCursor    uint64          `json:"last_acked_cursor"`
	LastAckDigest      string          `json:"last_ack_digest,omitempty"`
	State              string          `json:"state"`
	ResultRef          *api.ObjectRef  `json:"result_ref,omitempty"`
}
type EligibilityRequest struct {
	CheckRef               api.ObjectRef    `json:"check_ref"`
	ConsumerTaskRef        api.ObjectRef    `json:"consumer_task_ref"`
	RuleRef                api.ComponentRef `json:"rule_ref"`
	EvaluatorRef           api.ComponentRef `json:"evaluator_ref"`
	ReportRef              api.ContentRef   `json:"report_ref"`
	ScopeRef               api.ContentRef   `json:"scope_ref"`
	DependencyDigest       string           `json:"dependency_digest"`
	RequestedMaxAgeSeconds uint64           `json:"requested_max_age_seconds"`
	PrepareDeadline        string           `json:"prepare_deadline"`
}
type EligibilityReceipt struct {
	ReceiptID        string           `json:"receipt_id"`
	CheckRef         api.ObjectRef    `json:"check_ref"`
	ConsumerRef      api.ObjectRef    `json:"consumer_ref"`
	RequestDigest    string           `json:"request_digest"`
	RuleRef          api.ComponentRef `json:"rule_ref"`
	EvaluatorRef     api.ComponentRef `json:"evaluator_ref"`
	ReportRef        api.ContentRef   `json:"report_ref"`
	DependencyDigest string           `json:"dependency_digest"`
	Verdict          string           `json:"verdict"`
	AuthorityEpoch   uint64           `json:"authority_epoch"`
	DefectRevision   uint64           `json:"defect_revision"`
	Cursor           uint64           `json:"cursor"`
	CheckedAt        string           `json:"checked_at"`
	IssuedAt         string           `json:"issued_at"`
	ExpiresAt        string           `json:"expires_at"`
	HolderRef        api.ObjectRef    `json:"holder_ref"`
	Proof            string           `json:"proof,omitempty"`
	ProofRef         *api.ContentRef  `json:"proof_ref,omitempty"`
}
type ChangesRequest struct {
	HolderRef      api.ObjectRef `json:"holder_ref"`
	AuthorityEpoch uint64        `json:"authority_epoch"`
	Cursor         uint64        `json:"cursor"`
	Limit          uint64        `json:"limit"`
}
type ChangesOutput struct {
	Changes        []Defect `json:"changes"`
	Head           uint64   `json:"head"`
	NextCursor     uint64   `json:"next_cursor"`
	AuthorityEpoch uint64   `json:"authority_epoch"`
	Partial        bool     `json:"partial"`
	Digest         string   `json:"digest"`
}
type HolderAck struct {
	HolderRef      api.ObjectRef `json:"holder_ref"`
	AuthorityEpoch uint64        `json:"authority_epoch"`
	ThroughCursor  uint64        `json:"through_cursor"`
	ImportDigest   string        `json:"import_digest"`
}
type EvidenceImport struct {
	ID             string             `json:"id"`
	Revision       uint64             `json:"revision"`
	Receipt        EligibilityReceipt `json:"receipt"`
	ImportedCursor uint64             `json:"imported_cursor"`
	AuthorityEpoch uint64             `json:"authority_epoch"`
	Gap            bool               `json:"gap"`
	Defects        []Defect           `json:"defects"`
	Digest         string             `json:"digest"`
}
type ResultNotice struct {
	NoticeID        string        `json:"notice_id"`
	HolderRef       api.ObjectRef `json:"holder_ref"`
	ResultRef       api.ObjectRef `json:"result_ref"`
	DefectRef       api.ObjectRef `json:"defect_ref"`
	ConsumerTaskRef api.ObjectRef `json:"consumer_task_ref"`
	Reason          string        `json:"reason"`
	RegisteredAt    string        `json:"registered_at"`
}

type Installation struct {
	InstallLockRef api.ComponentRef   `json:"install_lock_ref"`
	Artifacts      []api.ContentRef   `json:"artifacts"`
	DependencyRefs []api.ComponentRef `json:"dependency_refs"`
	ConfigRef      api.ComponentRef   `json:"config_ref"`
	PlatformRef    api.ComponentRef   `json:"platform_ref"`
	ABI            string             `json:"abi"`
	Profile        string             `json:"profile"`
	ReadFormats    []string           `json:"read_formats"`
	WriteFormats   []string           `json:"write_formats"`
	TrustedBuiltin bool               `json:"trusted_builtin"`
	IsolationRefs  []api.ContentRef   `json:"isolation_refs"`
}
type PreparationEvidence struct {
	ArtifactDigest    string         `json:"artifact_digest"`
	ConfigDigest      string         `json:"config_digest"`
	Compatible        bool           `json:"compatible"`
	IsolationVerified bool           `json:"isolation_verified"`
	SelfTestRef       api.ContentRef `json:"self_test_ref"`
	Reason            string         `json:"reason,omitempty"`
}
type PreparedInstall struct {
	ID            string               `json:"id"`
	Revision      uint64               `json:"revision"`
	Installation  Installation         `json:"installation"`
	TargetRef     api.ObjectRef        `json:"target_ref"`
	State         string               `json:"state"`
	Evidence      *PreparationEvidence `json:"evidence,omitempty"`
	BlockedReason string               `json:"blocked_reason,omitempty"`
	Disposing     bool                 `json:"disposing"`
	CommandID     string               `json:"command_id,omitempty"`
}
type PrepareRequest struct {
	Installation Installation  `json:"installation"`
	TargetRef    api.ObjectRef `json:"target_ref"`
}
type BindingHead struct {
	TargetID             string         `json:"target_id"`
	Revision             uint64         `json:"revision"`
	Enabled              bool           `json:"enabled"`
	Generation           uint64         `json:"generation"`
	CurrentActivationRef *api.ObjectRef `json:"current_activation_ref,omitempty"`
	BindingRef           *api.ObjectRef `json:"binding_ref,omitempty"`
	ReadinessRef         *api.ObjectRef `json:"readiness_ref,omitempty"`
	DataFormat           string         `json:"data_format"`
}
type TargetRegister struct {
	TargetID   string `json:"target_id"`
	DataFormat string `json:"data_format"`
}
type Binding struct {
	BindingID      string           `json:"binding_id"`
	Revision       uint64           `json:"revision"`
	TargetRef      api.ObjectRef    `json:"target_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	ConfigRef      api.ComponentRef `json:"config_ref"`
	ReadinessRef   api.ObjectRef    `json:"readiness_ref"`
}
type Activation struct {
	ActivationID         string           `json:"activation_id"`
	Revision             uint64           `json:"revision"`
	TargetID             string           `json:"target_id"`
	InstallLockRef       api.ComponentRef `json:"install_lock_ref"`
	ConfigRef            api.ComponentRef `json:"config_ref"`
	ApprovalRef          api.ObjectRef    `json:"approval_ref"`
	Generation           uint64           `json:"generation"`
	State                string           `json:"state"`
	InstanceID           string           `json:"instance_id"`
	ExpectedHeadRevision uint64           `json:"expected_head_revision"`
	ExpectedGeneration   uint64           `json:"expected_generation"`
	PrepareDeadline      string           `json:"prepare_deadline"`
	CommandID            string           `json:"command_id"`
	Reopen               bool             `json:"reopen"`
	ExpectedInstanceRef  *api.ObjectRef   `json:"expected_instance_ref,omitempty"`
	FenceRef             *api.ObjectRef   `json:"fence_ref,omitempty"`
	ResidualRefs         []api.ObjectRef  `json:"residual_refs"`
	ReadinessRefs        []api.ObjectRef  `json:"readiness_refs"`
}
type InstanceRequest struct {
	TargetID     string           `json:"target_id"`
	ActivationID string           `json:"activation_id"`
	InstanceID   string           `json:"instance_id"`
	Generation   uint64           `json:"generation"`
	Installation Installation     `json:"installation"`
	ConfigRef    api.ComponentRef `json:"config_ref"`
	Deadline     string           `json:"deadline"`
}
type InstanceEvidence struct {
	InstanceID     string         `json:"instance_id"`
	Generation     uint64         `json:"generation"`
	ArtifactDigest string         `json:"artifact_digest"`
	ConfigDigest   string         `json:"config_digest"`
	SelfTestRef    api.ContentRef `json:"self_test_ref"`
	Ready          bool           `json:"ready"`
	ExpiresAt      string         `json:"expires_at"`
}
type InstanceReadiness struct {
	InstanceID     string         `json:"instance_id"`
	Revision       uint64         `json:"revision"`
	TargetID       string         `json:"target_id"`
	ActivationID   string         `json:"activation_id"`
	Generation     uint64         `json:"generation"`
	ConfigDigest   string         `json:"config_digest"`
	ArtifactDigest string         `json:"artifact_digest"`
	SelfTestRef    api.ContentRef `json:"self_test_ref"`
	ApprovalRef    api.ObjectRef  `json:"approval_ref"`
	ExpiresAt      string         `json:"expires_at"`
	State          string         `json:"state"`
}
type FenceEvidence struct {
	InstanceID    string         `json:"instance_id"`
	Generation    uint64         `json:"generation"`
	Exited        bool           `json:"exited"`
	MayApplyLater bool           `json:"may_apply_later"`
	ProofRef      api.ContentRef `json:"proof_ref"`
}
type DisposalEvidence struct {
	Exited       bool            `json:"exited"`
	ResidualRefs []api.ObjectRef `json:"residual_refs"`
}
type ActivateRequest struct {
	TargetID           string           `json:"target_id"`
	ExpectedGeneration uint64           `json:"expected_generation"`
	InstallLockRef     api.ComponentRef `json:"install_lock_ref"`
	ApprovalRef        api.ObjectRef    `json:"approval_ref"`
	ConfigRef          api.ComponentRef `json:"config_ref"`
	PrepareDeadline    string           `json:"prepare_deadline"`
}
type DeactivateRequest struct {
	TargetID           string        `json:"target_id"`
	ActivationRef      api.ObjectRef `json:"activation_ref"`
	ExpectedGeneration uint64        `json:"expected_generation"`
}
type ReopenRequest struct {
	TargetID            string        `json:"target_id"`
	ActivationRef       api.ObjectRef `json:"activation_ref"`
	ExpectedGeneration  uint64        `json:"expected_generation"`
	ExpectedInstanceRef api.ObjectRef `json:"expected_instance_ref"`
	OldInstanceFenceRef api.ObjectRef `json:"old_instance_fence_ref"`
	PrepareDeadline     string        `json:"prepare_deadline"`
}
type DisposeRequest struct {
	InstallLockRef   api.ComponentRef `json:"install_lock_ref"`
	ExpectedRevision uint64           `json:"expected_revision"`
}
type RolloutPolicy struct {
	TargetIDs                 []string `json:"target_ids"`
	BatchSizes                []uint64 `json:"batch_sizes"`
	MinimumSamples            uint64   `json:"minimum_samples"`
	ObservationSeconds        uint64   `json:"observation_seconds"`
	MaxErrorRate              string   `json:"max_error_rate"`
	MaximumStartWindowSeconds uint64   `json:"maximum_start_window_seconds"`
}
type ReleaseApproval struct {
	ApprovalID                string            `json:"approval_id"`
	Revision                  uint64            `json:"revision"`
	ApproverRef               api.ObjectRef     `json:"approver_ref"`
	InstallLockRef            api.ComponentRef  `json:"install_lock_ref"`
	Purpose                   string            `json:"purpose"`
	EvidenceRefs              []api.ObjectRef   `json:"evidence_refs"`
	CompatibilityEvidenceRefs []api.ContentRef  `json:"compatibility_evidence_refs"`
	Rollout                   RolloutPolicy     `json:"rollout"`
	ExpiresAt                 string            `json:"expires_at"`
	State                     string            `json:"state"`
	RollbackInstallLockRef    *api.ComponentRef `json:"rollback_install_lock_ref,omitempty"`
	RollbackApprovalRef       *api.ObjectRef    `json:"rollback_approval_ref,omitempty"`
	ConfirmationRef           api.ObjectRef     `json:"confirmation_ref"`
	BatchIndex                uint64            `json:"batch_index"`
}
type ApprovalCreate struct {
	ApprovalID                string            `json:"approval_id"`
	InstallLockRef            api.ComponentRef  `json:"install_lock_ref"`
	Purpose                   string            `json:"purpose"`
	EvidenceRefs              []api.ObjectRef   `json:"evidence_refs"`
	CompatibilityEvidenceRefs []api.ContentRef  `json:"compatibility_evidence_refs"`
	Rollout                   RolloutPolicy     `json:"rollout"`
	ExpiresAt                 string            `json:"expires_at"`
	RollbackInstallLockRef    *api.ComponentRef `json:"rollback_install_lock_ref,omitempty"`
	RollbackApprovalRef       *api.ObjectRef    `json:"rollback_approval_ref,omitempty"`
	PreviewRefs               []api.ContentRef  `json:"preview_refs"`
}
type ApprovalUse struct {
	UseID          string           `json:"use_id"`
	ApprovalRef    api.ObjectRef    `json:"approval_ref"`
	TargetRef      api.ObjectRef    `json:"target_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	IssuedAt       string           `json:"issued_at"`
	StartBefore    string           `json:"start_before"`
	Proof          string           `json:"proof,omitempty"`
	RequestDigest  string           `json:"request_digest"`
}
type ApprovalUseRequest struct {
	UseID          string           `json:"use_id"`
	ApprovalRef    api.ObjectRef    `json:"approval_ref"`
	TargetRef      api.ObjectRef    `json:"target_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	StartBefore    string           `json:"start_before"`
}
type InstallHolder struct {
	HolderID       string           `json:"holder_id"`
	Revision       uint64           `json:"revision"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	ConsumerRef    api.ObjectRef    `json:"consumer_ref"`
	State          string           `json:"state"`
}
type ExtensionRead struct {
	Head         *BindingHead       `json:"head,omitempty"`
	Activation   *Activation        `json:"activation,omitempty"`
	Installation *PreparedInstall   `json:"installation,omitempty"`
	Readiness    *InstanceReadiness `json:"readiness,omitempty"`
}
