// Package runtimespec fixes the local runtime ports for the detailed design.
// It contains contract declarations only, not a runtime implementation or SDK.
package runtimespec

import (
	"context"
	"time"
)

type TaskRef struct{ Namespace, Key string }
type OperationRef struct{ Namespace, ID string }
type WorkRef struct {
	Task TaskRef
	Key  string
}
type Version int64
type Epoch int64
type Digest [32]byte

// Document is an immutable, validated payload from a locally registered schema.
// Bytes contain the exact canonical UTF-8 representation fixed at acceptance.
type Document struct {
	Schema string
	Bytes  []byte
	Digest Digest
}

// Access is produced by a trusted ingress, not deserialized from user JSON.
type Access struct {
	Namespace, Principal, DelegatingPrincipal string
	Proof                                     Document
}

type OwnerFence struct {
	Owner string
	Epoch Epoch
}
type ClaimMode string

const (
	ExecuteClaim ClaimMode = "EXECUTE"
	RecoverClaim ClaimMode = "RECOVER"
)

type Lease struct {
	Work      WorkRef
	Worker    string
	Claim     Epoch
	Boot      Epoch
	Owner     OwnerFence
	Mode      ClaimMode
	ExpiresAt time.Time
}

type TaskState string

const (
	Queued    TaskState = "QUEUED"
	Running   TaskState = "RUNNING"
	Waiting   TaskState = "WAITING"
	Completed TaskState = "COMPLETED"
	Failed    TaskState = "FAILED"
	Cancelled TaskState = "CANCELLED"
)

type WorkKind string

const (
	Decide           WorkKind = "DECIDE"
	Dispatch         WorkKind = "DISPATCH"
	ApplyInbox       WorkKind = "APPLY_INBOX"
	Reconcile        WorkKind = "RECONCILE"
	PropagateControl WorkKind = "PROPAGATE_CONTROL"
	Deliver          WorkKind = "DELIVER"
	Recover          WorkKind = "RECOVER"
)

type Lane string

const (
	TargetLane      Lane = "TARGET"
	ControlLane     Lane = "CONTROL"
	DispositionLane Lane = "DISPOSITION"
)

// A view has one consistent task version; collection fields may be paged only
// through the same snapshot token and bounded read session.
type TaskView struct {
	Task                                               TaskRef
	Version                                            Version
	GoalRevision                                       Version
	Owner                                              OwnerFence
	OwnerPhase                                         string
	State                                              TaskState
	EffectiveControl                                   string
	Deadline                                           time.Time
	NextDecisionSequence                               int64
	DecisionNeeded                                     bool
	Goal, Control, Outcome                             Document
	Operations, Work, Decisions, Budgets, Reservations []Document
	Waits, Dependencies, Handoffs                      []Document
	SnapshotToken                                      string
}

type CommandKind string
type Command struct {
	Access    Access
	Task      TaskRef
	Operation OperationRef
	Expected  *Version // nil only for methods whose contract permits it
	Kind      CommandKind
	Body      Document
}
type Receipt struct {
	Task      TaskRef
	Operation OperationRef
	Version   Version
	Result    Document
}
type Core interface {
	Handle(context.Context, Command) (Receipt, error)
	Get(context.Context, Access, TaskRef) (TaskView, error)
	LookupOperation(context.Context, Access, OperationRef) (OperationLookup, error)
}
type OperationLookup struct {
	State   string // FOUND, NOT_OBSERVED, CLOSED, EXPIRED
	Receipt *Receipt
}

// Proofs are prepared outside transactions; validity and local revisions are
// checked inside the committing transaction. Remote authority is not atomic.
type AdmissionProof struct {
	Purpose  string
	Subject  OperationRef
	Revision string
	NotAfter time.Time
	Evidence Document
}
type Guards struct {
	Expected         *Version // nil means task must not exist, only for CREATE
	Owner            OwnerFence
	Boot             Epoch
	Lease            *Lease
	DecisionSequence *int64
	GoalRevision     *Version
	Proofs           []AdmissionProof
}

// MutationKind has a closed registry documented with the storage contract.
// There are no SQL fragments, arbitrary column names, or executable callbacks.
type MutationKind string
type Mutation struct {
	Kind             MutationKind
	Key              string
	ExpectedRevision *Version
	Value            Document
}
type Change struct {
	Access         Access
	Task           TaskRef
	ID             string
	Kind           string
	SemanticDigest Digest
	Guards         Guards
	Mutations      []Mutation
	Result         Receipt
}
type CommitState string

const (
	Committed CommitState = "COMMITTED"
	Rejected  CommitState = "REJECTED"
	Unknown   CommitState = "UNKNOWN"
)

type CommitResult struct {
	State    CommitState
	ChangeID string
	Receipt  *Receipt
	Failure  *Fault
}
type CommitLookup struct {
	State          string // COMMITTED, NOT_OBSERVED, CLOSED_NOT_COMMITTED, EXPIRED
	SemanticDigest Digest
	Receipt        *Receipt
}
type Page struct {
	Cursor string
	Limit  int
}
type RecoverablePage struct {
	Tasks []TaskRef
	Next  string
}
type RunStore interface {
	Load(context.Context, Access, TaskRef) (TaskView, error)
	Commit(context.Context, Change) (CommitResult, error)
	LookupCommit(context.Context, Access, TaskRef, string) (CommitLookup, error)
	ListRecoverable(context.Context, Access, Page) (RecoverablePage, error)
}

type Candidate struct {
	Work       WorkRef
	Kind       WorkKind
	Lane       Lane
	DueAt      time.Time
	EnqueuedAt time.Time
}
type ScanRequest struct {
	Access Access
	Lane   Lane
	Page   Page
}
type CandidatePage struct {
	Items []Candidate
	Next  string
}

// SchedulerScope is a trusted host-issued scope for the namespaces assigned
// to this persistence unit. It is not a user-supplied wildcard namespace.
type SchedulerScope struct {
	Principal  string
	Assignment Document
}
type NamespacePage struct {
	Namespaces []string
	Next       string
}
type ClaimRequest struct {
	Access              Access
	Candidate           WorkRef
	ChangeID            string
	Worker              string
	Owner               OwnerFence
	Boot                Epoch
	Mode                ClaimMode
	Proofs              []AdmissionProof
	MaxLease            time.Duration
	DecisionReservation []BudgetAmount // nonempty only for DECIDE
	Binding             Document       // exact Brain/model/context policy versions for DECIDE
}
type BudgetAmount struct {
	Dimension string
	Amount    int64 // base units, never floating point
}
type ClaimResult struct {
	Commit           CommitResult
	Lease            *Lease
	DecisionSequence *int64
	View             *TaskView // post-commit version used by a new decision
}
type Renewal struct {
	Access       Access
	Lease        Lease
	ChangeID     string
	MaxExtension time.Duration
}
type WorkStore interface {
	ListNamespaces(context.Context, SchedulerScope, Page) (NamespacePage, error)
	ScanDue(context.Context, ScanRequest) (CandidatePage, error)
	Claim(context.Context, ClaimRequest) (ClaimResult, error)
	Renew(context.Context, Renewal) (ClaimResult, error)
	// Completion/release and next responsibility are in RunStore.Commit.
}
type SchedulerPolicy interface {
	Select([]Candidate, int) []WorkRef
}

// Local reliable-message ingress and egress share the runtime transaction unit.
type Incoming struct {
	Access                            Access
	Task                              TaskRef
	MessageID, Source, SourceRevision string
	Body                              Document
}
type PersistedMessage struct {
	MessageID string
	Digest    Digest
}
type Outgoing struct {
	Task        TaskRef
	MessageID   string
	Operation   OperationRef
	Destination string
	Body        Document
}
type RuntimeMailbox interface {
	Persist(context.Context, Incoming) (PersistedMessage, error)
	// ClaimedInbox returns the original authenticated, immutable messages for
	// an APPLY_INBOX lease. Consumption remains part of RunStore.Commit.
	ClaimedInbox(context.Context, Access, Lease) ([]Incoming, error)
	// ClaimedOutbox checks lease and READY state; it does not prepare dispatch.
	ClaimedOutbox(context.Context, Access, Lease) ([]Outgoing, error)
}

type ReviewChoice string

const (
	Keep ReviewChoice = "KEEP"
	Drop ReviewChoice = "DROP"
)

type ReviewedOperation struct {
	Operation            OperationRef
	Choice               ReviewChoice
	OriginalDigest       Digest
	OriginalGoalRevision Version
}
type ProposalReview struct {
	Task               TaskRef
	DecisionSequence   int64
	BaseVersion        Version
	ReviewedOperations []ReviewedOperation
	Proposal           Document // existing mutually exclusive proposal main type
}

// Error code is stable; Message is diagnostic and must not contain secrets.
type Fault struct {
	Code       string
	Message    string
	RetryAfter time.Duration
	Reference  string
}

func (f *Fault) Error() string { return f.Code + ": " + f.Message }
