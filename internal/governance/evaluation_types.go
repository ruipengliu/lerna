package governance

import "github.com/ruipengliu/lerna/api"

type ImprovementPolicy struct {
	ID                 string           `json:"id"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	LineageID          string           `json:"lineage_id"`
	FormalAttemptLimit uint64           `json:"formal_attempt_limit"`
	FormalAttemptsUsed uint64           `json:"formal_attempts_used"`
	Revision           uint64           `json:"revision"`
	Stopped            bool             `json:"stopped"`
}
type EvaluationThresholds struct {
	MinimumTargetRate    string `json:"minimum_target_rate"`
	MinimumImprovement   string `json:"minimum_improvement"`
	MaximumCostPerSample string `json:"maximum_cost_per_sample"`
	MaximumLatencyMillis uint64 `json:"maximum_latency_millis"`
	ConfidenceLevel      string `json:"confidence_level"`
	StatisticalMethod    string `json:"statistical_method"`
	MinimumSamples       uint64 `json:"minimum_samples"`
	CostUnit             string `json:"cost_unit"`
}
type EvaluationSample struct {
	SampleID string         `json:"sample_id"`
	InputRef api.ContentRef `json:"input_ref"`
	TruthRef api.ContentRef `json:"truth_ref"`
	Class    string         `json:"class"`
}
type EvaluationPlan struct {
	PlanID               string               `json:"plan_id"`
	Revision             uint64               `json:"revision"`
	ManifestRef          api.ContentRef       `json:"manifest_ref"`
	ManifestDigest       string               `json:"manifest_digest"`
	CandidateRef         api.ComponentRef     `json:"candidate_ref"`
	BaselineRef          api.ComponentRef     `json:"baseline_ref"`
	PartitionRef         api.ObjectRef        `json:"partition_ref"`
	SourceGroup          string               `json:"source_group"`
	Seed                 uint64               `json:"seed"`
	Budget               []api.Amount         `json:"budget"`
	Thresholds           EvaluationThresholds `json:"thresholds"`
	MaximumAttempts      uint64               `json:"maximum_attempts"`
	ObservationCutoff    string               `json:"observation_cutoff"`
	UnknownOutcomePolicy string               `json:"unknown_outcome_policy"`
	CostUnknownPolicy    string               `json:"cost_unknown_policy"`
	ReleaseRequestID     string               `json:"release_request_id,omitempty"`
	FormalAttemptIndex   uint64               `json:"formal_attempt_index,omitempty"`
	ImprovementPolicyRef *api.ObjectRef       `json:"improvement_policy_ref,omitempty"`
	Purpose              string               `json:"purpose"`
	Frozen               bool                 `json:"frozen"`
	FullDenominator      uint64               `json:"full_denominator"`
	ExposureRevision     uint64               `json:"exposure_revision"`
	FormalEligible       bool                 `json:"formal_eligible"`
}
type PlanCreate struct {
	Plan EvaluationPlan `json:"plan"`
}
type EvaluationRun struct {
	RunID              string                `json:"run_id"`
	PlanID             string                `json:"plan_id"`
	Revision           uint64                `json:"revision"`
	State              string                `json:"state"`
	GateOpen           bool                  `json:"gate_open"`
	Samples            api.CollectionSummary `json:"samples"`
	Environments       api.CollectionSummary `json:"environments"`
	ReportRef          *api.ObjectRef        `json:"report_ref,omitempty"`
	Cursor             string                `json:"cursor,omitempty"`
	CancellationCursor string                `json:"cancellation_cursor,omitempty"`
	BlockedReason      string                `json:"blocked_reason,omitempty"`
	FormalEligible     bool                  `json:"formal_eligible"`
	CreatedAt          string                `json:"created_at"`
}
type SampleRun struct {
	SampleRunID          string          `json:"sample_run_id"`
	PlanID               string          `json:"plan_id"`
	RunID                string          `json:"run_id"`
	SampleID             string          `json:"sample_id"`
	Arm                  string          `json:"arm"`
	Revision             uint64          `json:"revision"`
	EnvironmentKey       string          `json:"environment_key"`
	Outcome              string          `json:"outcome"`
	Attempts             uint64          `json:"attempts"`
	Usage                []api.Amount    `json:"usage"`
	UsageFinal           bool            `json:"usage_final"`
	CostUpperBound       []api.Amount    `json:"cost_upper_bound"`
	LatencyMillis        uint64          `json:"latency_millis"`
	ModelClaimedComplete bool            `json:"model_claimed_complete"`
	SystemAccepted       bool            `json:"system_accepted"`
	StopConfirmed        bool            `json:"stop_confirmed"`
	MayApplyLater        bool            `json:"may_apply_later"`
	EnvironmentDestroyed bool            `json:"environment_destroyed"`
	SafeRetry            bool            `json:"safe_retry"`
	ObservedAt           string          `json:"observed_at,omitempty"`
	TruthRef             *api.ContentRef `json:"truth_ref,omitempty"`
	OperationRefs        []api.ObjectRef `json:"operation_refs"`
	AttemptRefs          []api.ObjectRef `json:"attempt_refs"`
}
type RunRequest struct {
	PlanID string `json:"plan_id"`
	RunID  string `json:"run_id"`
}
type RunnerPair struct {
	RunID                   string           `json:"run_id"`
	Plan                    EvaluationPlan   `json:"plan"`
	Sample                  EvaluationSample `json:"sample"`
	CandidateEnvironmentKey string           `json:"candidate_environment_key"`
	BaselineEnvironmentKey  string           `json:"baseline_environment_key"`
	StartBefore             string           `json:"start_before"`
	Permit                  string           `json:"permit,omitempty"`
}
type PairEvidence struct {
	CandidatePrepared       bool           `json:"candidate_prepared"`
	BaselinePrepared        bool           `json:"baseline_prepared"`
	CandidateEnvironmentRef api.ObjectRef  `json:"candidate_environment_ref"`
	BaselineEnvironmentRef  api.ObjectRef  `json:"baseline_environment_ref"`
	ProofRef                api.ContentRef `json:"proof_ref"`
}
type RunnerAttempt struct {
	RunID             string           `json:"run_id"`
	PlanID            string           `json:"plan_id"`
	SampleID          string           `json:"sample_id"`
	Arm               string           `json:"arm"`
	AttemptID         string           `json:"attempt_id"`
	EnvironmentKey    string           `json:"environment_key"`
	ImplementationRef api.ComponentRef `json:"implementation_ref"`
	InputRef          api.ContentRef   `json:"input_ref"`
	TruthRef          api.ContentRef   `json:"truth_ref"`
	StartBefore       string           `json:"start_before"`
	Seed              uint64           `json:"seed"`
	Permit            string           `json:"permit,omitempty"`
	Budget            []api.Amount     `json:"budget"`
}
type AttemptObservation struct {
	Outcome              string         `json:"outcome"`
	TruthRef             api.ContentRef `json:"truth_ref"`
	Usage                []api.Amount   `json:"usage"`
	UsageFinal           bool           `json:"usage_final"`
	CostUpperBound       []api.Amount   `json:"cost_upper_bound"`
	LatencyMillis        uint64         `json:"latency_millis"`
	ModelClaimedComplete bool           `json:"model_claimed_complete"`
	SystemAccepted       bool           `json:"system_accepted"`
	MayApplyLater        bool           `json:"may_apply_later"`
	OperationRef         api.ObjectRef  `json:"operation_ref"`
	ProofRef             api.ContentRef `json:"proof_ref"`
	SafeRetry            bool           `json:"safe_retry"`
	ObservedAt           string         `json:"observed_at"`
}
type SampleAttempt struct {
	AttemptID   string              `json:"attempt_id"`
	Revision    uint64              `json:"revision"`
	SampleRunID string              `json:"sample_run_id"`
	Index       uint64              `json:"index"`
	State       string              `json:"state"`
	Request     RunnerAttempt       `json:"request"`
	Observation *AttemptObservation `json:"observation,omitempty"`
}
type PairStopEvidence struct {
	CandidateStopped   bool           `json:"candidate_stopped"`
	BaselineStopped    bool           `json:"baseline_stopped"`
	CandidateDestroyed bool           `json:"candidate_destroyed"`
	BaselineDestroyed  bool           `json:"baseline_destroyed"`
	MayApplyLater      bool           `json:"may_apply_later"`
	ProofRef           api.ContentRef `json:"proof_ref"`
}
type SamplePageRequest struct {
	RunID  string `json:"run_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  uint64 `json:"limit"`
}
type ExposureGate struct {
	ID                 string `json:"id"`
	Revision           uint64 `json:"revision"`
	SourceGroup        string `json:"source_group"`
	Cursor             uint64 `json:"cursor"`
	KnownExposure      bool   `json:"known_exposure"`
	EarliestOccurredAt string `json:"earliest_occurred_at,omitempty"`
	UnknownTime        bool   `json:"unknown_time"`
}
type Exposure struct {
	ExposureID  string         `json:"exposure_id"`
	SourceGroup string         `json:"source_group"`
	ActorRef    api.ObjectRef  `json:"actor_ref"`
	Kind        string         `json:"kind"`
	OccurredAt  string         `json:"occurred_at,omitempty"`
	RecordedAt  string         `json:"recorded_at"`
	ReportRef   *api.ObjectRef `json:"report_ref,omitempty"`
	Cursor      uint64         `json:"cursor"`
}
type ExposureRegister struct {
	ExposureID  string         `json:"exposure_id"`
	SourceGroup string         `json:"source_group"`
	Kind        string         `json:"kind"`
	OccurredAt  string         `json:"occurred_at,omitempty"`
	ReportRef   *api.ObjectRef `json:"report_ref,omitempty"`
}
type EvaluationReport struct {
	ReportID                string       `json:"report_id"`
	Revision                uint64       `json:"revision"`
	PlanID                  string       `json:"plan_id"`
	RunID                   string       `json:"run_id"`
	Purpose                 string       `json:"purpose"`
	FullDenominator         uint64       `json:"full_denominator"`
	CandidateSuccess        uint64       `json:"candidate_success"`
	BaselineSuccess         uint64       `json:"baseline_success"`
	CandidateModelComplete  uint64       `json:"candidate_model_complete"`
	CandidateSystemAccepted uint64       `json:"candidate_system_accepted"`
	FalseAccept             uint64       `json:"false_accept"`
	FalseReject             uint64       `json:"false_reject"`
	TimeoutUnknownNotRun    uint64       `json:"timeout_unknown_not_run"`
	CandidateCost           []api.Amount `json:"candidate_cost"`
	BaselineCost            []api.Amount `json:"baseline_cost"`
	CandidateWorstCaseCost  []api.Amount `json:"candidate_worst_case_cost"`
	CostGate                string       `json:"cost_gate"`
	TargetAttainment        string       `json:"target_attainment"`
	StatisticalGate         string       `json:"statistical_gate"`
	ImprovementGate         string       `json:"improvement_gate"`
	ConfidenceLower         string       `json:"confidence_lower"`
	ConfidenceUpper         string       `json:"confidence_upper"`
	PairWins                uint64       `json:"pair_wins"`
	PairLosses              uint64       `json:"pair_losses"`
	MissingCount            uint64       `json:"missing_count"`
	SealedAt                string       `json:"sealed_at"`
	InputsDigest            string       `json:"inputs_digest"`
	FormalEligible          bool         `json:"formal_eligible"`
	Qualifications          []string     `json:"qualifications"`
}
type ReportQualification struct {
	ReportID     string `json:"report_id"`
	Revision     uint64 `json:"revision"`
	Eligible     bool   `json:"eligible"`
	Reason       string `json:"reason,omitempty"`
	GateRevision uint64 `json:"gate_revision"`
	UpdatedAt    string `json:"updated_at"`
}
type EvaluationRead struct {
	Run           EvaluationRun        `json:"run"`
	Plan          EvaluationPlan       `json:"plan"`
	Report        *EvaluationReport    `json:"report,omitempty"`
	Qualification *ReportQualification `json:"qualification,omitempty"`
}
type FeedbackOpen struct {
	ReportRef  api.ObjectRef `json:"report_ref"`
	ExposureID string        `json:"exposure_id"`
}
type LateObservation struct {
	SampleRunRef   api.ObjectRef      `json:"sample_run_ref"`
	Observation    AttemptObservation `json:"observation"`
	SourceRevision uint64             `json:"source_revision"`
	SourceDigest   string             `json:"source_digest"`
}
