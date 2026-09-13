// Package contextassembly fixes governed context to a task-local decision.
package contextassembly

import "context"

// Key is local to the task. Decision must be allocated by the trusted host;
// connection retries cannot allocate a replacement for an existing decision.
type Key struct {
	Namespace, TaskID string
	Decision          uint64
}

// Snapshot is an internal durable representation, not a public disclosure API.
// Only the assembler may place a policy-approved document in the store.
type Snapshot struct {
	Key                     Key
	Subject, SemanticSHA256 string
	Document                []byte
}

// Store atomically binds the first result to an intent, even when its selection
// is empty. A repeat returns the original document, never the new candidate.
// An unknown Bind must be reconciled with Read or the same Bind identity.
type Store interface {
	Read(context.Context, Key) (Snapshot, error)
	Bind(context.Context, Snapshot) (Snapshot, error)
}

// RetirementStore is a trusted cleanup seam. Retire atomically erases the
// snapshot and permanently fences its decision identity, even if cleanup arrives
// before Bind. An unknown result is reconciled by repeating the same retirement.
type RetirementStore interface {
	Retire(context.Context, Key) error
}
type Error string

func (e Error) Error() string { return string(e) }

// DecisionReason translates context failures into the host's existing decision
// outcomes without coupling the assembler to a particular worker implementation.
// Invalid arguments remain programming failures, rather than resumable waits.
func (e Error) DecisionReason() string {
	switch e {
	case Missing, Invalidated, IdentityConflict:
		return "INPUT_INVALIDATED"
	case Denied:
		return "PROCESSING_DENIED"
	case BudgetExceeded:
		return "INPUT_BUDGET_EXCEEDED"
	case Unavailable, Capacity:
		return "CAPABILITY_UNAVAILABLE"
	default:
		return ""
	}
}

const (
	Invalid          Error = "INVALID_ARGUMENT"
	Missing          Error = "CONTEXT_MISSING"
	Unavailable      Error = "CONTEXT_UNAVAILABLE"
	IdentityConflict Error = "IDENTITY_CONFLICT"
	Capacity         Error = "CAPACITY_EXCEEDED"
)
