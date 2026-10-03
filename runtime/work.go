package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
)

var ErrClaim = errors.New("claim is invalid, expired or replaced")
var ErrWorkBounds = errors.New("work request exceeds supported bounds")

// Claim binds temporary database authority to an original Job and exact revision.
// It does not fence external effects.
type Claim struct {
	JobID           contract.ID
	Object          contract.ObjectRef
	Phase           string
	Worker          string
	ClaimedRevision int64
	Epoch           int64
	LeaseUntil      time.Time
}

// ClaimStore is separate from admission's JobStore. The consumer locks its
// object and fixes stage input before claiming a candidate. Scan holds no Job
// locks, so all mutation paths keep object/input -> Job lock order.
type ClaimStore interface {
	Scan(context.Context, Tx, time.Time, int) ([]Job, error)
	Claim(context.Context, Tx, Job, string, time.Time, time.Time) (*Claim, error)
	Complete(context.Context, Tx, Claim, time.Time) error
	Renew(context.Context, Tx, Claim, time.Time, time.Time) (Claim, error)
}
