package durable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"
)

var (
	ErrScope        = errors.New("durable: invalid or foreign scope")
	ErrTx           = errors.New("durable: expired, concurrent or undeclared transaction access")
	ErrNested       = errors.New("durable: nested transaction")
	ErrLockOrder    = errors.New("durable: undeclared or out-of-order lock")
	ErrClaim        = errors.New("durable: claim is no longer valid")
	ErrConflict     = errors.New("durable: idempotency_conflict")
	ErrGone         = errors.New("durable: gone")
	ErrNotFound     = errors.New("durable: not_found")
	ErrExpired      = errors.New("durable: expired")
	ErrUnsupported  = errors.New("durable: unsupported method or kind")
	ErrPreparation  = errors.New("durable: original command has durable preparation")
	ErrInvariant    = errors.New("durable: invalid state or version relationship")
	ErrPrecondition = errors.New("durable: precondition_failed")
	ErrSteps        = errors.New("durable: work step budget exhausted")
)

type Outcome string

const (
	Committed     Outcome = "committed"
	RolledBack    Outcome = "rolled_back"
	CommitUnknown Outcome = "commit_unknown"
)

type Result struct {
	Outcome  Outcome
	Attempts int
	Err      error
}

// Scope is supplied by trusted assembly/authentication, never a request payload.
type Scope struct{ TenantID, OwnerID string }

func (s Scope) Valid() bool { return validName(s.TenantID) && validName(s.OwnerID) }
func validName(s string) bool {
	return len(s) > 0 && len(s) <= 200 && utf8.ValidString(s) && !containsControl(s)
}
func containsControl(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*_[0-9a-f]{32}$`)

type CommandKey struct{ ServiceID, CommandID string }

func (k CommandKey) valid() bool { return validName(k.ServiceID) && idPattern.MatchString(k.CommandID) }

type Receipt struct {
	CommandID  string          `json:"command_id"`
	Stage      string          `json:"stage"`
	ResourceID string          `json:"resource_id,omitempty"`
	Revision   *int64          `json:"revision,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      json.RawMessage `json:"error,omitempty"`
	AcceptedAt string          `json:"accepted_at,omitempty"`
	DecidedAt  string          `json:"decided_at,omitempty"`
	Redacted   bool            `json:"redacted,omitempty"`
}
type CommandRecord struct {
	Key                                                           CommandKey
	Digest, Method, TargetID, State, PreparationRef, DecisionType string
	ExpiresAt, RetainUntil                                        int64
	Receipt                                                       *Receipt
}
type JobKey struct{ Kind, Responsibility string }

func (k JobKey) valid() bool   { return validName(k.Kind) && validName(k.Responsibility) }
func (k JobKey) order() string { return k.Kind + "\x00" + k.Responsibility }

type Job struct {
	Scope                                       Scope
	Key                                         JobKey
	ID, SourceRef, State, HolderID, WaitReason  string
	WorkRevision, LeaseEpoch, LeaseUntil, DueAt int64
}

// Claim has no public constructor or mutable observation fields.
type Claim struct {
	job      Job
	observed int64
}

func (c Claim) JobID() string           { return c.job.ID }
func (c Claim) Scope() Scope            { return c.job.Scope }
func (c Claim) Key() JobKey             { return c.job.Key }
func (c Claim) SourceRef() string       { return c.job.SourceRef }
func (c Claim) HolderID() string        { return c.job.HolderID }
func (c Claim) Epoch() int64            { return c.job.LeaseEpoch }
func (c Claim) ObservedRevision() int64 { return c.observed }
func (c Claim) Until() time.Time        { return time.UnixMilli(c.job.LeaseUntil).UTC() }

// Snapshot is for storage adapters; handlers use the opaque Claim above.
func (c Claim) Snapshot() Job { return c.job }

type Disposition struct {
	State  string
	DueAt  time.Time
	Reason string
}

func Done() Disposition { return Disposition{State: "done"} }
func Waiting(until time.Time, reason string) Disposition {
	return Disposition{State: "waiting", DueAt: until, Reason: reason}
}

// Session is a storage adapter protocol, never an extension or domain port.
type Session interface {
	Handle(context.Context) any
	Now(context.Context) (int64, error)
	Reserve(context.Context, Scope, CommandRecord) (CommandRecord, bool, error)
	Lookup(context.Context, Scope, CommandKey) (CommandRecord, error)
	Save(context.Context, Scope, CommandRecord) error
	Raise(context.Context, Scope, JobKey, string, string, int64) (Job, error)
	Hint(context.Context, Scope, JobKey, int64) error
	LockJob(context.Context, Scope, string) (Job, error)
	Candidates(context.Context, Scope, string, int) ([]Job, error)
	Lease(context.Context, Job, string, int64) (Job, error)
	UpdateJob(context.Context, Job) error
	DeleteDone(context.Context, Scope, JobKey) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
type Backend interface {
	Begin(context.Context, Scope) (Session, error)
	Retryable(error) bool
	CommitRolledBack(error) bool
	Notify()
	Wake() <-chan struct{}
}
type Options struct {
	TxTimeout      time.Duration
	Attempts       int
	QueryRetention time.Duration
	Kinds          []string
}
