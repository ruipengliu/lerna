// Package memory defines the governed long-term memory boundary.
package memory

import "context"

// Ref identifies a record independently from task or connection identities.
type Ref struct{ Namespace, Collection, Key string }

// Revision is the Store's immutable encoded record. Only the trusted Memory
// service may supply Document after schema, source and residency validation.
type Revision struct {
	Ref      Ref
	Revision uint64
	Document []byte
}
type Change struct {
	OperationID, Subject, SemanticSHA256 string
	Expected                             uint64
	Record                               Revision
}

// Receipt contains no memory body; disclosure always requires current policy.
type Receipt struct {
	OperationID, Subject, SemanticSHA256 string
	Ref                                  Ref
	Revision, Position                   uint64
}

// Store is an internal trusted seam, not a plugin access or authorization API.
// Commit atomically writes a revision, original receipt and collection position.
// An unavailable commit is unknown; callers must LookupOperation before retry.
type Store interface {
	Read(context.Context, Ref, uint64) (Revision, error)
	Commit(context.Context, Change) (Receipt, error)
	LookupOperation(context.Context, string, string) (Receipt, error)
	ReadChanges(context.Context, string, string, uint64, int) ([]Receipt, error)
}
type Error string

func (e Error) Error() string { return string(e) }

const (
	Invalid          Error = "INVALID_ARGUMENT"
	Conflict         Error = "VERSION_CONFLICT"
	IdentityConflict Error = "IDENTITY_CONFLICT"
	Unavailable      Error = "UNAVAILABLE"
	Missing          Error = "MEMORY_UNAVAILABLE"
	Capacity         Error = "CAPACITY_EXCEEDED"
	Quarantined      Error = "RESTORE_QUARANTINED"
)

// ReadBinding fixes the result of an authorized disclosure before any body is
// released. ID is a grant-bound read identity, not a new mutation operation.
// Same identity + semantic intent returns its original selection, including an
// empty selection; inability to resolve it must never cause a new query.
type ReadBinding struct {
	Namespace, ID, Subject, SemanticSHA256, PermitID, Coverage string
	Results                                                    []VersionRef
	PartialWitness, BudgetWitness                              *VersionRef
}
type VersionRef struct {
	Ref      Ref
	Revision uint64
}
type DisclosureStore interface {
	BindRead(context.Context, ReadBinding) (ReadBinding, error)
	LookupRead(context.Context, string, string) (ReadBinding, error)
}

// QueryStore scans one bounded collection snapshot. The reference deployment
// bounds all retained revisions to 512; scanning never limits unauthorized
// records into a user-visible result budget. Policy filtering is Memory's job.
type QueryStore interface {
	Store
	DisclosureStore
	Scan(context.Context, string, string) ([]Revision, error)
	// ValidateVersions checks up to 34 exact historical revisions in one
	// storage snapshot, without reading bodies. Deleted/missing versions fail.
	// This is the release linearization point for storage liveness only;
	// it does not grant permission or promise validity after returning.
	ValidateVersions(context.Context, []VersionRef) error
}

// CoverageReferences returns the immutable sources of derived coverage facts.
// Missing witnesses fail closed, including bindings made by older implementations.
func (b ReadBinding) CoverageReferences() ([]VersionRef, error) {
	partial := b.Coverage == "partial_unavailable" || b.Coverage == "partial_and_budget_exhausted"
	budget := b.Coverage == "budget_exhausted" || b.Coverage == "partial_and_budget_exhausted"
	if b.Coverage != "complete" && !partial && !budget || partial != (b.PartialWitness != nil) || budget != (b.BudgetWitness != nil) {
		return nil, Irrecoverable
	}
	var refs []VersionRef
	if partial {
		refs = append(refs, *b.PartialWitness)
	}
	if budget {
		refs = append(refs, *b.BudgetWitness)
	}
	return refs, nil
}

// Deletion is admitted by the trusted Memory service, never directly by a peer.
// The semantic digest covers deletion metadata only, not the deleted body.
type Deletion struct {
	OperationID, Subject, SemanticSHA256 string
	Ref                                  Ref
	Expected                             uint64
}

// DeletionStore atomically replaces retained revisions with a permanent minimal
// tombstone, erases old payload comparison digests, and appends the change.
// Deletion does not attest to cleanup of external derived stores or backups.
type DeletionStore interface {
	Store
	Delete(context.Context, Deletion) (Receipt, error)
}

// ReplayUnavailable requires querying the original result: the payload needed
// for a reliable replay comparison has been erased and must not be recreated.
const ReplayUnavailable Error = "OPERATION_RESULT_ONLY"

// ErasedOperationStore identifies original writes under a proven deletion
// revision for trusted metadata cleanup. No payload comparison hash remains.
type ErasedOperationStore interface {
	ErasedOperations(context.Context, Ref, uint64) ([]Receipt, error)
}
