// Package credentials controls third-party secret use at a trusted driver exit.
// It has no plaintext read interface for consumers of a bound Driver.
package credentials

import (
	"context"
	"time"
)

type Error string

func (e Error) Error() string { return string(e) }

const (
	Invalid           Error = "CREDENTIAL_INVALID"
	Denied            Error = "CREDENTIAL_DENIED"
	Missing           Error = "CREDENTIAL_MISSING"
	Expired           Error = "CREDENTIAL_EXPIRED"
	Unavailable       Error = "CREDENTIAL_UNAVAILABLE"
	Conflict          Error = "CREDENTIAL_CONFLICT"
	KeyUnavailable    Error = "CREDENTIAL_KEY_UNAVAILABLE"
	Exhausted         Error = "CREDENTIAL_KEY_EXHAUSTED"
	TargetUnavailable Error = "CREDENTIAL_TARGET_UNAVAILABLE"
)

// Binding is fixed by the trusted host, never by a model's invocation payload.
type Binding struct{ Namespace, Subject, Driver, Service, Account, Purpose, Location string }
type Record struct {
	Ref               string
	Binding           Binding
	Format            uint32
	Revision          uint64
	ExpiresUnix       int64
	KeyVersion        string
	Nonce, Ciphertext []byte
}

// Store holds ciphertext only. Swap must compare and replace atomically and
// retain its configured record/size limits across reopening.
type Store interface {
	Get(context.Context, string) (Record, error)
	Swap(context.Context, uint64, Record) error
	List(context.Context) ([]Record, error)
}

// KeySource is a privileged host port. Reserve persists nonce consumption before
// returning key material. Failed subsequent writes must never reclaim its nonce.
type KeySource interface {
	Reserve(context.Context) (version string, key, nonce []byte, err error)
	Read(context.Context, string) ([]byte, error)
}
type Authority interface {
	Check(context.Context, string, Binding, string) error
}
type Clock interface{ Now() (time.Time, error) }
type Call struct {
	OperationID string
	Payload     []byte
}

// Result intentionally cannot carry target-controlled strings or secret bytes.
type Result struct{ Accepted bool }
type Exit interface {
	Target() Binding
	Send(context.Context, Call, []byte) (Result, error)
}
type Config struct {
	Timeout       time.Duration
	MaxConcurrent int
}

// Metadata projects only fields explicitly allowed in management receipts.
func (r Record) Metadata() Record {
	return Record{Ref: r.Ref, Binding: r.Binding, Format: r.Format, Revision: r.Revision, ExpiresUnix: r.ExpiresUnix, KeyVersion: r.KeyVersion}
}
