package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Caller is supplied by an authenticated embedding host. Request payloads never
// choose tenant, actor or the authenticated source owner.
type Caller struct{ TenantID, ActorID, SourceOwnerID, OnBehalfOf string }
type Command struct {
	CommandID        string          `json:"command_id"`
	Method           string          `json:"method"`
	TargetID         string          `json:"target_id"`
	ExpiresAt        string          `json:"expires_at"`
	ExpectedRevision *int64          `json:"expected_revision,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}
type CommandResult struct {
	CommandID, Stage, ResourceID string
	Revision                     *int64
	Output                       json.RawMessage
	Failure                      *Error
}
type Query struct {
	Method, TargetID string
	Payload          json.RawMessage
}
type QueryResult struct {
	Value json.RawMessage
	Gaps  []string
}

// Orchestrator owns task/budget decisions. Queries never initiate actions.
// CommitUnknown requires CommandStatus on the original service and command;
// it must not be interpreted as a rejection or retried with another identity.
type Orchestrator interface {
	Execute(context.Context, Caller, Command) (CommandResult, error)
	CommandStatus(context.Context, Caller, string) (CommandResult, error)
	Query(context.Context, Caller, Query) (QueryResult, error)
}

var ErrCommitUnknown = errors.New("orchestrator: commit outcome unknown; query original command")

type Failure struct{ Detail Error }

func (e *Failure) Error() string { return e.Detail.Code + ": " + e.Detail.Message }

// Access is an exact use/disclosure request. Current is a local bounded check;
// adapters must do remote proof acquisition before this callback. It runs in a
// transaction and must not perform network I/O or start another transaction.
type Access struct {
	Method, TargetID, TaskID, IntentHash string
	PolicyRef                            *ComponentRef
	ContentRefs                          []ContentRef
	AuthorizationRefs                    []AuthorizationRef
	Components                           []ComponentRef
	Gate                                 *TaskGate
}
type AuthorityPort interface {
	AdmissionReady() bool
	Current(context.Context, Caller, Access, time.Time) error
	DisclosureGeneration(context.Context, Caller) (string, error)
	ControlProof(context.Context, Caller, TaskGate, string, time.Time) (ControlSnapshot, error)
}

// Ready covers the original identity, install/approval, writer isolation and
// bounded clock required by this assembly. A job lease does not replace it.
type ClockPort interface {
	Ready() bool
	Now() time.Time
}
type ContentPort interface {
	Read(context.Context, Caller, ContentRef) ([]byte, error)
	// writeID is persisted before the first call and returns the original exact
	// publication after retry or a lost response.
	Write(context.Context, Caller, string, string, []byte) (ContentRef, error)
}
type TaskPolicy struct {
	CoverageBinding                                                                              VerificationBinding
	VerificationBindings                                                                         []VerificationBinding
	BrainProviderID                                                                              string
	ExtractionOwnerID                                                                            string
	ExtractionAuthorizationRefs                                                                  []AuthorizationRef
	Ref                                                                                          ComponentRef
	BrainID                                                                                      string
	ModelProfileRef                                                                              ComponentRef
	Requirements                                                                                 []Requirement
	Capabilities                                                                                 []CapabilityFixture
	UsageAuthorizationRefs                                                                       []AuthorizationRef
	DecisionUpperBound                                                                           []Amount
	BudgetMode                                                                                   string
	MaxContinuations, MaxRepairs, MaxNoProgress, MaxActions, MaxContextRequests, MaxOutputTokens int64
	MaxPolls                                                                                     int64
	InputTimeout                                                                                 time.Duration
}

// Each rule has one fixed implementation in the default profile. Selection
// precedes evidence acquisition and cannot depend on a favorable report.
type VerificationBinding struct {
	RuleRef, EvaluatorRef ComponentRef
	Basis                 string
}
type PolicyPort interface {
	Resolve(context.Context, Caller, ComponentRef, ContentRef, []string) (TaskPolicy, error)
}
type OriginalCall struct {
	Command Command
	OwnerID string
}
type BrainPort interface {
	Decide(context.Context, Caller, OriginalCall, DecisionRequest) (DecisionRecord, error)
	Read(context.Context, Caller, string, string) (DecisionRecord, error)
	Cancel(context.Context, Caller, OriginalCall, string) (DecisionRecord, error)
}
type SourceKey struct{ OwnerID, Kind, ID string }
type ActionPreparation struct {
	Action                                          ActionInvoke
	ExecutorID, ProviderID, ResourceID, EffectClass string
	UpperBound                                      []Amount
	AuthorizationRefs                               []AuthorizationRef
	// Configuration includes only immutable exact references used to authorize
	// this binding. Current qualification is still checked at actual entry.
	Configuration []ComponentRef
}
type ActionPort interface {
	Prepare(context.Context, Caller, Task, ActionInvoke) (ActionPreparation, error)
}
type ExecutorPort interface {
	Invoke(context.Context, Caller, OriginalCall, Invoke) (Operation, error)
	Read(context.Context, Caller, string, string) (Operation, error)
	Cancel(context.Context, Caller, OriginalCall, string) (Operation, error)
	Control(context.Context, Caller, OriginalCall, ControlSnapshot) (ControlObservation, error)
	AttachEvidence(context.Context, Caller, OriginalCall, string, []ContentRef) error
}
type ControlObservation struct {
	ExecutorID, TaskID            string
	GoalRevision, ControlRevision int64
	Enforced                      bool
}
type BillingQuery struct {
	TaskRef                             TaskRef
	ObjectOwnerID, ObjectKind, ObjectID string
	Source                              *SourceKey
}

// Read returns the original owner's cumulative bill, not a notification amount.
// Source identifies one physical billing item even if several modules project it.
type Billing struct {
	Source   SourceKey
	Revision int64
	Digest   string
	Usage    []Amount
	Final    bool
	ProofRef ContentRef
}
type BillingPort interface {
	Read(context.Context, Caller, BillingQuery) (Billing, error)
	Allocation(context.Context, Caller, string, string) (RuntimeBudgetAllocation, error)
	Closure(context.Context, Caller, string, string) (RuntimeBudgetClosure, error)
	Reconcile(context.Context, Caller, OriginalCall) (JobAck, error)
}

// A verifier consumes complete exact inputs and original reports. Local
// deterministic checks may run here; a new model/tool/effect must be returned as
// an Action and admitted normally. This port does not grant action permission.
type VerificationInput struct {
	InputDigest string
	Task        Task
	Constraints []string
	Artifacts   []ContentRef
	Operations  []Operation
	Checks      []CheckEvidence
}
type CoverageEvidence struct {
	InputDigest           string
	TaskID                string
	GoalRevision          int64
	GoalRef               ContentRef
	RequirementsDigest    string
	RuleRef, EvaluatorRef ComponentRef
	ReportRef             ContentRef
	Verdict               string
	Limitations           []string
}
type CheckEvidence struct {
	InputDigest       string
	CheckID, TaskID   string
	GoalRevision      int64
	RuleRef           ComponentRef
	Result            ConditionResult
	ReportRef         ContentRef
	ComponentCheckIDs []string
	// AcceptanceCommandID is populated only by this owner after a trusted input
	// transaction. A provider cannot turn a plain answer into user acceptance.
	AcceptanceCommandID string
}
type VerificationBundle struct {
	Coverage               *CoverageEvidence
	Checks                 []CheckEvidence
	Actions                []ActionInvoke
	Limitations            []string
	AcceptanceRequirements []string
}

// EvidenceDefect is supplied only by authenticated maintenance assembly with
// exact version/scope proof. Registration and completion share a local gate.
type EvidenceDefect struct {
	ID                    string
	RuleRef, EvaluatorRef ComponentRef
	TaskID                string
	GoalRevision          int64
	ArtifactHash          string
	ProofRef              ContentRef
}
type VerificationPort interface {
	Prepare(context.Context, Caller, VerificationInput) (VerificationBundle, error)
}
type ConfirmationPort interface {
	Read(context.Context, Caller, Command, ObjectRef) (ConfirmationRecord, error)
}
type ContextPort interface {
	Prepare(context.Context, Caller, Task, []ProposalRequestsItem) ([]BrainContextMaterialsItem, error)
}
type ExtractionPort interface {
	Submit(context.Context, Caller, OriginalCall, ContentRef) (string, error)
}
type DelegationPreparation struct {
	Action     ActionDelegate
	ReceiverID string
	Submit     TaskSubmitInput
	// Same-owner children share the parent's actual local transaction. A remote
	// child remains an original-command handoff with independent closure facts.
	Internal bool
}
type DelegationObservation struct {
	OwnerID, DelegationID, ChildTaskID string
	Revision                           int64
	Status                             string
	GoalClosed, EffectsClosed          bool
	Result                             *Result
	ProofRef                           ContentRef
}
type CollaborationPort interface {
	Prepare(context.Context, Caller, Task, ActionDelegate) (DelegationPreparation, error)
	Submit(context.Context, Caller, OriginalCall, DelegationRequest) (DelegationObservation, error)
	Read(context.Context, Caller, string, string) (DelegationObservation, error)
	Control(context.Context, Caller, OriginalCall, string, TaskGate) (DelegationObservation, error)
}
type OrchestratorPorts struct {
	Authority     AuthorityPort
	Clock         ClockPort
	Content       ContentPort
	Policies      PolicyPort
	Brain         BrainPort
	Actions       ActionPort
	Executor      ExecutorPort
	Billing       BillingPort
	Verification  VerificationPort
	Confirmation  ConfirmationPort
	Context       ContextPort
	Extraction    ExtractionPort
	Collaboration CollaborationPort
}
