// Package authorization implements the local authority, independent of storage and transport.
package authorization

import (
	"context"
	"errors"
	wire "lerna/gen/harness/v1"
	"time"
)

type Code string

const (
	Unauthenticated  Code = "UNAUTHENTICATED"
	Denied           Code = "PERMISSION_DENIED"
	Invalid          Code = "INVALID_ARGUMENT"
	Unsupported      Code = "UNSUPPORTED"
	Conflict         Code = "VERSION_CONFLICT"
	IdentityConflict Code = "IDENTITY_CONFLICT"
	Expired          Code = "ADMISSION_EXPIRED"
	NotFound         Code = "NOT_FOUND"
	TimeUntrusted    Code = "TIME_UNTRUSTED"
	Unavailable      Code = "UNAVAILABLE"
	OutcomeUnknown   Code = "OUTCOME_UNKNOWN"
	ResultOnly       Code = "OPERATION_RESULT_ONLY"
)

type Error struct{ Code Code }

func (e *Error) Error() string  { return string(e.Code) }
func fail(c Code) error         { return &Error{c} }
func Is(err error, c Code) bool { var e *Error; return errors.As(err, &e) && e.Code == c }

// Config must be supplied explicitly; reference values live in the executable profile.
type Config struct {
	CredentialTTL, GrantTTL, WindowTTL, ReceiptRetention time.Duration
	MaxRules, MaxResources, MaxDepth, MaxWork            int
	EvaluationTimeout                                    time.Duration
}

func (c Config) validate() error {
	if c.CredentialTTL < time.Second || c.GrantTTL < time.Second || c.WindowTTL < time.Second || c.ReceiptRetention < c.WindowTTL || c.MaxRules <= 0 || c.MaxResources <= 0 || c.MaxDepth <= 0 || c.MaxWork <= 0 || c.EvaluationTimeout <= 0 {
		return fail(Invalid)
	}
	if c.CredentialTTL > 365*24*time.Hour || c.GrantTTL > c.CredentialTTL || c.WindowTTL > c.GrantTTL || c.ReceiptRetention > 365*24*time.Hour || c.MaxRules > 10000 || c.MaxResources > 10000 || c.MaxDepth > 128 || c.MaxWork > 1000000 || c.EvaluationTimeout > time.Second {
		return fail(Invalid)
	}
	return nil
}

type Clock interface{ Now() (time.Time, error) }
type SystemClock struct{}

func (SystemClock) Now() (time.Time, error) { return time.Now(), nil }

type Identity struct{ Subject, Namespace string }
type Principal struct {
	Subject                 string
	Expires                 int64
	Disabled, Administrator bool
}
type OperationRecord struct {
	Subject     string
	Command     *wire.AuthorizationCommand
	Receipt     *wire.AuthorizationReceipt
	Window      uint64
	RetainUntil int64
}

// State belongs to the trusted authorization storage seam, not to the public SDK.
// Store implementations must return isolated snapshots and commit them atomically.
type State struct {
	Nodes                 *NodeJournal
	Uses                  map[string]UseRecord
	MemoryOperations      map[string]MemoryAdmission
	ExecutionData         []byte
	ExecutionOperations   map[string]string
	ExecutionReservations map[string]bool // Issued for a durable proposal; not yet bound to an Invocation.
	ContentData           []byte
	ContentOperations     map[string]string
	Signed                *GrantJournal
	RuntimeData           []byte            // Trusted local runtime partition; retained independently of management receipts.
	DeliveryData          []byte            // Reliable delivery shares the runtime atomic commit.
	RuntimeOperations     map[string]string // operation_id -> original subject; no cleanup while runtime recovery is needed.
	Format                int
	Namespace, Authority  string
	Secret                []byte
	Config                Config
	Principals            map[string]Principal // keyed by credential digest, never plaintext
	Resources             map[string]string
	Rules                 []*wire.PolicyRule
	Grants                map[string]*wire.LocalGrant
	Operations            map[string]OperationRecord
	Changes               []*wire.AuthorizationReceipt
	Revision              uint64
	Window, ClosedThrough uint64
	WindowExpires         int64
	LastTime              int64
}
type Snapshot struct {
	Version uint64
	State   State
}
type Store interface {
	Load(context.Context) (Snapshot, error)
	Commit(context.Context, uint64, State) error
}
