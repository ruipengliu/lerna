package extraction

import "context"

// CandidateRecord is trusted, already-authorized retention input. OperationID
// is the original extraction operation, also the candidate's stable identity.
// A retained candidate is not yet a committed long-term Memory record.
type CandidateRecord struct {
	// InvocationSHA256 binds the original execution request metadata, not source
	// body contents. Empty legacy bindings cannot prove an execution result.
	InvocationSHA256                string
	Namespace, Subject, OperationID string
	Candidate                       Candidate
	Restrictions                    Restrictions
}

// CandidateStore is a trusted persistence seam, never a user-facing read API.
// Callers check current authorization, source liveness and retention before
// writing or releasing data. An unavailable Commit is unknown: Lookup the same
// operation; never allocate another operation to replace the uncertain one.
type CandidateStore interface {
	Commit(context.Context, CandidateRecord) error
	Lookup(context.Context, string, string) (CandidateRecord, error)
}

// CandidateState retains only the original identity and effect fact. Retired
// before Commit and unsupported attempts both have Committed=false; a fence
// must not invent a saved effect. Unsupported carries only an allowlisted reason.
type CandidateState struct {
	InvocationSHA256                       string
	Namespace, Subject, OperationID, State string
	Committed                              bool
	// Reason is an allowlisted local outcome, never source or model text.
	Reason string
}

// DeclineStore is trusted host persistence, not an authorization API. It
// durably records an unsupported attempt without a candidate.
// It fences the same operation against later candidate commits. Unknown
// writes must be inspected under the original invocation, never replaced.
type DeclineStore interface {
	Decline(context.Context, CandidateState) error
}

// CandidateLifecycle preserves a permanent original-operation fence while
// clearing the controlled body. Retire is trusted cleanup, not public deletion.
type CandidateLifecycle interface {
	CandidateStore
	Retire(context.Context, string, string, string) error
	Inspect(context.Context, string, string) (CandidateState, error)
}
