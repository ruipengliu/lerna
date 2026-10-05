package content

import (
	"context"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

// FixturePolicy is an explicit durable test policy, never a production Grant.
// Installation is a trusted management seam outside all public payloads.
type FixturePolicy struct {
	Ref         v.ContentRef     `json:"content_ref"`
	Subject     v.SubjectBinding `json:"subject"`
	Purpose     string           `json:"purpose"`
	Revision    int64            `json:"revision"`
	ValidUntil  time.Time        `json:"valid_until"`
	RetainUntil time.Time        `json:"retain_until"`
	Read        bool             `json:"read"`
	Process     bool             `json:"process"`
	Save        bool             `json:"save"`
	Sync        bool             `json:"sync"`
	Disclose    bool             `json:"disclose"`
}

type Objects interface {
	Put(context.Context, string, string, string, int64, []byte) error
	Read(context.Context, string, string, int64) ([]byte, error)
}

// Limits are installed explicitly by the host; missing finite limits fail closed.
type Limits struct {
	MaxPreparingVersions int
	MaxStagingBytes      int64
	Lease                time.Duration
	WorkTimeout          time.Duration
}

type Record struct {
	Ref                    v.ContentRef     `json:"content_ref"`
	Sources                []v.ContentRef   `json:"sources"`
	Purpose                string           `json:"purpose"`
	Subject                v.SubjectBinding `json:"subject"`
	TupleDigest            string           `json:"tuple_digest"`
	ObjectID               string           `json:"object_id"`
	ObjectKey              string           `json:"object_key"`
	RequestedRetainUntil   v.Time           `json:"requested_retain_until"`
	EffectiveRetainUntil   v.Time           `json:"effective_retain_until"`
	CurrentRetainUntil     v.Time           `json:"current_retain_until"`
	AdmittedAt             v.Time           `json:"admitted_at"`
	PublishDeadline        v.Time           `json:"publish_deadline"`
	IODeadline             v.Time           `json:"io_deadline,omitempty"`
	Publication            string           `json:"publication"`
	Failure                v.ErrorCode      `json:"failure,omitempty"`
	Revision               int64            `json:"revision"`
	Attempts               int              `json:"attempts"`
	MaxPublicationAttempts int              `json:"max_publication_attempts"`
	AttemptKey             string           `json:"attempt_key,omitempty"`
	StagingHolder          bool             `json:"staging_holder"`
	ObjectHolder           bool             `json:"object_holder"`
	CleanupPending         bool             `json:"cleanup_pending"`
	Bytes                  []byte           `json:"-"`
}
type CommandRecord struct {
	Digest  string           `json:"digest"`
	Subject v.SubjectBinding `json:"subject"`
	Ref     v.ContentRef     `json:"content_ref"`
	Purpose string           `json:"purpose"`
	Receipt v.CommandReceipt `json:"receipt"`
}
type Repository interface {
	runtime.TxRunner
	runtime.Clock
	LockCommand(context.Context, runtime.Tx, v.CommandRef) (*CommandRecord, error)
	ReadCommandRecord(context.Context, runtime.Tx, v.CommandRef) (*CommandRecord, error)
	SaveCommand(context.Context, runtime.Tx, v.CommandRef, CommandRecord) error
	LockVersion(context.Context, runtime.Tx, v.ContentRef) (*Record, error)
	LockObject(context.Context, runtime.Tx, string) (*Record, error)
	SaveVersion(context.Context, runtime.Tx, Record) error
	CheckPolicy(context.Context, runtime.Tx, v.SubjectBinding, v.ContentRef, string, string, time.Time) (*FixturePolicy, error)
	CheckCommandReader(context.Context, runtime.Tx, v.SubjectBinding, time.Time) (bool, error)
	CheckCapacity(context.Context, runtime.Tx, Limits, int64) (bool, error)
	runtime.JobStore
	runtime.ClaimStore
	ValidateClaim(context.Context, runtime.Tx, runtime.Claim, time.Time) error
	DeferClaim(context.Context, runtime.Tx, runtime.Claim, time.Time, time.Time) error
}
