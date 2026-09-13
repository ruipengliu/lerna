package fetch

import (
	"context"
	"errors"
	"lerna/tasks"
)

var (
	Missing          = errors.New("fetch record missing")
	IdentityConflict = errors.New("fetch identity conflict")
	Capacity         = errors.New("fetch store capacity")
)

// ControlledAcquisition carries a confirmed acquisition fact whose content is
// withheld by current task control. It is not a recovered Outcome and carries
// no Content identity; callers must not retry acquisition or disclose content.
type ControlledAcquisition struct {
	Mode     string
	Requests uint32
}

func (*ControlledAcquisition) Error() string {
	return "acquisition completed; content withheld by task control"
}

// AttemptIntent is supplied by qualified execution. No URL or response body is
// stored in the budget ledger. Fingerprint binds the original execution input.
type AttemptIntent struct {
	Task                              tasks.Ref
	Subject, OperationID, Fingerprint string
	MaxRequests, TaskLimit            uint32
	// EvidenceOperation is allocated by authorization before acquisition.
	// Empty legacy reservations cannot recover a retained acquisition.
	EvidenceOperation string
}

// Charged is the sum of original upper-bound reservations, not measured HTTP
// usage. Reservations are not refunded by this ledger, including unknown or
// failed attempts. The host must report actual usage separately when known.
type TaskBudget struct {
	Subject        string
	Limit, Charged uint32
}

type AttemptStore interface {
	// Begin returns true only to the caller that first durably reserved this
	// operation. False is reconciliation, never permission to dispatch again.
	// A task-budget rejection retains the original identity and zero-request
	// limit_exceeded outcome; it does not charge a reservation.
	Begin(context.Context, AttemptIntent) (bool, error)
	Inspect(context.Context, string, string) (AttemptIntent, error)
	Budget(context.Context, tasks.Ref) (TaskBudget, error)
}

// Outcome contains immutable known facts only. Reference is an opaque governed
// Content reference, never response bytes or a URL. A stored reference does not
// authorize its disclosure or prove that the underlying content is still live.
type Outcome struct {
	Mode      string
	Status    string
	Requests  uint32
	Reference string
}

type OutcomeStore interface {
	AttemptStore
	Complete(context.Context, AttemptIntent, Outcome) error
	// known=false means an original attempt exists but no terminal facts were
	// committed. It never authorizes a replacement request.
	Outcome(context.Context, string, string) (Outcome, bool, error)
}

// IsFailureStatus recognizes finite acquisition facts safe to project without
// response details. It does not authorize access to those facts.
func IsFailureStatus(status string) bool {
	switch status {
	case "invalid", "denied", "unavailable", "timed_out", "cancelled", "too_large", "unsupported", "limit_exceeded", "expired":
		return true
	}
	return false
}
