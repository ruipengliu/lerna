// Package decision_engine proposes bounded next steps; it owns no Task state.
package decision_engine

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

var ErrInputLimit = errors.New("decision input exceeds remaining byte limit")
var ErrUnavailable = errors.New("decision dependency unavailable")
var ErrForbidden = errors.New("decision authorization denied")
var ErrPublicationConflict = errors.New("fixture publication identity conflict")

// Permission is a current, trusted fixture authorization observation. It is
// checked against database time after locks; it is not a production Grant.
type Permission struct {
	Subject         v.SubjectBinding
	DecisionOwner   v.OwnerRef
	TaskRef         v.TaskObjectRef
	ComponentRef    v.ComponentRef
	UseRefs         []v.UseRef
	ValidUntil      time.Time
	ChargeBasis     string   `json:"ChargeBasis,omitempty"`
	RuleStartCharge v.Amount `json:"RuleStartCharge,omitzero"`
	RuleVersion     string   `json:"RuleVersion,omitempty"`
}
type Authority interface {
	Authorize(context.Context, v.SubjectBinding, v.DecisionRef, string, *v.DecisionDecidePayload) (Permission, error)
}

// ControlAccess grants current control/read access, never execution or content
// publication. A nil DecisionRef is an explicit owner command-read scope.
type ControlAccess struct {
	Subject       v.SubjectBinding `json:"subject"`
	DecisionOwner v.OwnerRef       `json:"decision_owner"`
	DecisionRef   *v.DecisionRef   `json:"decision_ref,omitempty"`
	Purposes      []string         `json:"purposes"`
	ValidUntil    time.Time        `json:"valid_until"`
}
type ControlAuthority interface {
	AuthorizeControl(context.Context, v.SubjectBinding, v.DecisionRef, string) (ControlAccess, error)
	VerifyControl(context.Context, v.SubjectBinding, v.DecisionCancelPayload, *v.DecisionDecidePayload) (*v.Revision, error)
}
type CapabilityBinding struct {
	CapabilityRef v.CapabilityRef `json:"capability_ref"`
	BindingRef    v.BindingRef    `json:"binding_ref"`
	ArgumentsRef  v.ContentRef    `json:"arguments_ref"`
	Purpose       string          `json:"purpose"`
}

// Snapshot is explicitly a fixture manifest, not an Orchestrator Snapshot.
type Snapshot struct {
	Ref                v.SnapshotRef       `json:"snapshot_ref"`
	Raw                []byte              `json:"-"`
	TaskRef            v.TaskObjectRef     `json:"task_ref"`
	GoalRevision       v.Revision          `json:"goal_revision"`
	ControlRevision    v.Revision          `json:"control_revision"`
	ComponentRef       v.ComponentRef      `json:"component_ref"`
	MaterialRefs       []v.ContentRef      `json:"material_refs"`
	RequirementRefs    []v.RequirementRef  `json:"requirement_refs"`
	CapabilityBindings []CapabilityBinding `json:"capability_bindings"`
	AnswerSchemaRefs   []v.ContentRef      `json:"answer_schema_refs"`
	UseRefs            []v.UseRef          `json:"use_refs"`
	Rule               string              `json:"rule"`
}
type FixtureLock struct {
	Raw             []byte
	ManifestRef     v.ContentRef
	ManifestRaw     []byte
	ComponentRef    v.ComponentRef
	RuleVersion     string   `json:"RuleVersion,omitempty"`
	ChargeBasis     string   `json:"ChargeBasis,omitempty"`
	RuleStartCharge v.Amount `json:"RuleStartCharge,omitzero"`
}
type Source interface {
	ReadSnapshot(context.Context, v.SnapshotRef, Permission, int64) (Snapshot, error)
	ReadMaterial(context.Context, v.ContentRef, string, Permission, int64) ([]byte, error)
	ReadFixtureLock(context.Context, v.InstallLockRef, Permission, int64) (FixtureLock, error)
}
type Publisher interface {
	PlanPublication(context.Context, string, []byte, []v.ContentRef, Permission) (v.ContentRef, error)
	Publish(context.Context, string, []byte, []v.ContentRef, Permission) (v.ContentRef, error)
	ReadPublished(context.Context, v.ContentRef, Permission) ([]byte, error)
}
type Prepared struct {
	StartSequence int64          `json:"start_sequence"`
	InputDigest   string         `json:"input_digest"`
	ArtifactKey   string         `json:"artifact_key"`
	ArtifactRef   v.ContentRef   `json:"artifact_ref"`
	ArtifactBytes []byte         `json:"artifact_bytes"`
	ProposalKey   string         `json:"proposal_key"`
	ProposalRef   v.ContentRef   `json:"proposal_ref"`
	Proposal      v.Proposal     `json:"proposal"`
	ProposalBytes []byte         `json:"proposal_bytes"`
	Sources       []v.ContentRef `json:"sources"`
	Digest        string         `json:"digest"`
}
type Record struct {
	Ref                    v.DecisionRef            `json:"decision_ref"`
	Input                  *v.DecisionDecidePayload `json:"input,omitempty"`
	Subject                v.SubjectBinding         `json:"subject"`
	InputDigest            string                   `json:"input_digest"`
	Revision               int64                    `json:"revision"`
	Status                 string                   `json:"status"`
	Proposal               *v.Proposal              `json:"proposal,omitempty"`
	ProposalRef            *v.ContentRef            `json:"proposal_ref,omitempty"`
	ArtifactRefs           []v.ContentRef           `json:"artifact_refs"`
	Usage                  v.DecisionUsage          `json:"usage"`
	Failure                *v.DecisionFailure       `json:"failure,omitempty"`
	ControlBasis           *v.ControlBasis          `json:"control_basis,omitempty"`
	Reason                 string                   `json:"reason,omitempty"`
	CloseTaskRef           *v.TaskObjectRef         `json:"close_task_ref,omitempty"`
	StartedEpoch           int64                    `json:"started_epoch"`
	StartSequence          int64                    `json:"start_sequence"`
	MeasurementPending     bool                     `json:"measurement_pending"`
	MeasurementUnknown     bool                     `json:"measurement_unknown"`
	Prepared               *Prepared                `json:"prepared,omitempty"`
	OriginalPermission     *Permission              `json:"original_permission,omitempty"`
	PublicationAttempts    int                      `json:"publication_attempts"`
	WakeAt                 v.Time                   `json:"wake_at,omitempty"`
	LegacyUnaccountedStart bool                     `json:"legacy_unaccounted_start,omitempty"`
	LegacyBillingRetired   bool                     `json:"legacy_billing_retired,omitempty"`
}
type CommandRecord struct {
	MetadataVersion string                   `json:"metadata_version,omitempty"`
	Method          string                   `json:"method,omitempty"`
	Digest          string                   `json:"digest"`
	Request         *v.DecisionDecideRequest `json:"request,omitempty"`
	CancelRequest   *v.DecisionCancelRequest `json:"cancel_request,omitempty"`
	Subject         v.SubjectBinding         `json:"subject"`
	Receipt         v.CommandReceipt         `json:"receipt"`
}
type DecisionStop struct {
	Ref           v.DecisionRef    `json:"decision_ref"`
	TaskRef       v.TaskObjectRef  `json:"task_ref"`
	InputDigest   v.SchemaDigest   `json:"input_digest"`
	ControlBasis  v.ControlBasis   `json:"control_basis"`
	BindingDigest string           `json:"binding_digest"`
	Subject       v.SubjectBinding `json:"subject"`
	CommandRef    v.CommandRef     `json:"command_ref"`
}
type Repository interface {
	LockCommand(context.Context, runtime.Tx, v.CommandRef) (*CommandRecord, error)
	SaveCommand(context.Context, runtime.Tx, v.CommandRef, CommandRecord) error
	LockDecision(context.Context, runtime.Tx, v.DecisionRef) (*Record, error)
	SaveDecision(context.Context, runtime.Tx, Record) error
	ReadDecision(context.Context, runtime.Tx, v.DecisionRef) (*Record, error)
	MaintenanceCandidates(context.Context, runtime.Tx, time.Time, int) ([]runtime.Job, error)
	ReadCommandRecord(context.Context, runtime.Tx, v.CommandRef) (*CommandRecord, error)
	ReadStop(context.Context, runtime.Tx, v.DecisionRef) (*DecisionStop, error)
	SaveStop(context.Context, runtime.Tx, DecisionStop) error
	LockDecisionJob(context.Context, runtime.Tx, v.DecisionRef) (*runtime.Job, error)
}

// Pool ports remain owned by this consumer. Only the shared finite values and
// pure FIFO calculations live in workpool.
type Pools interface {
	LockPool(context.Context, runtime.Tx) (workpool.State, error)
	InstallPool(context.Context, runtime.Tx, workpool.Config, int64, time.Time) error
	PoolScope(context.Context, runtime.Tx) (string, error)
	SavePoolCursor(context.Context, runtime.Tx, workpool.State, string, workpool.Cursor) error
	PoolCounts(context.Context, runtime.Tx, workpool.State, string, contract.ID, time.Time) (int64, int64, int64, error)
	PoolQueue(context.Context, runtime.Tx, workpool.State, contract.ObjectRef, string, string) (string, bool, error)
	PoolReadyTenants(context.Context, runtime.Tx, workpool.State, string, time.Time) (map[contract.ID]time.Time, error)
	PoolPage(context.Context, runtime.Tx, workpool.State, string, contract.ID, string, string, time.Time) ([]runtime.Job, string, error)
	RegisterPoolClaim(context.Context, runtime.Tx, workpool.State, runtime.Claim) error
	ValidatePoolClaim(context.Context, runtime.Tx, workpool.State, runtime.Claim, time.Time) error
	PoolNextWake(context.Context, runtime.Tx, workpool.State, string, time.Time, time.Time) (time.Time, error)
}
type Store interface {
	runtime.TxRunner
	runtime.Clock
	runtime.JobStore
	runtime.ClaimStore
	runtime.ScheduleStore
	Repository
	Pools
}
