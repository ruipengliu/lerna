package api

// Snapshot 是 Orchestrator 发布、Brain 消费的准确输入，不是 Brain 的 Task 副本。
// 字段冻结依据 docs/architecture/data/module-records.md §3。
type Snapshot struct {
	SnapshotID            string              `json:"snapshot_id"`
	Revision              uint64              `json:"revision"`
	TaskRef               ObjectRef           `json:"task_ref"`
	GoalRevision          uint64              `json:"goal_revision"`
	ControlRevision       uint64              `json:"control_revision"`
	GoalRef               ContentRef          `json:"goal_ref"`
	Requirements          []Requirement       `json:"requirements"`
	RequirementsDigest    string              `json:"requirements_digest"`
	CoverageRef           *ObjectRef          `json:"coverage_ref,omitempty"`
	RequirementsState     string              `json:"requirements_state"`
	Purpose               string              `json:"purpose"`
	FactRefs              []ObjectRef         `json:"fact_refs"`
	UnresolvedCollections []CollectionSummary `json:"unresolved_collections"`
	PolicyRef             ComponentRef        `json:"policy_ref"`
	InstallLockRef        ComponentRef        `json:"install_lock_ref"`
	ModelProfileRef       ComponentRef        `json:"model_profile_ref"`
	CapabilityRefs        []ComponentRef      `json:"capability_refs"`
	BindingRefs           []ObjectRef         `json:"binding_refs"`
	MaterialRefs          []ContentRef        `json:"material_refs"`
	SelectionReportRef    ContentRef          `json:"selection_report_ref"`
	ProcessedSources      []ContentRef        `json:"processed_sources"`
	InputTokens           uint64              `json:"input_tokens"`
	ReservedOutputTokens  uint64              `json:"reserved_output_tokens"`
	SafetyMarginTokens    uint64              `json:"safety_margin_tokens"`
	CountMode             string              `json:"count_mode"`
	TokenizerRef          ComponentRef        `json:"tokenizer_ref"`
	EncodedDigest         string              `json:"encoded_digest"`
}
