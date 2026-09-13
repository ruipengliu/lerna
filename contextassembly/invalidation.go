package contextassembly

import "context"

// SourceInvalidation is trusted maintenance input. Missing selects absence
// dependencies; otherwise it selects body-derived states, including trimmed and
// inapplicable sources. ThroughRevision is inclusive.
type SourceInvalidation struct {
	Namespace, Collection, Key string
	ThroughRevision            uint64
	Missing                    bool
}

// InvalidationStore atomically persists the source watermark and erases whole
// affected snapshots, preserving their decision identities against rebinding.
// The returned count covers newly erased snapshots in this call only.
type InvalidationStore interface {
	InvalidateSource(context.Context, SourceInvalidation) (int, error)
}

func ValidInvalidation(in SourceInvalidation) bool {
	return label(in.Namespace) && label(in.Collection) && label(in.Key) && in.ThroughRevision > 0
}

// SnapshotAffected interprets the assembler's private document for trusted
// storage adapters. It is not a disclosure API and conveys no authorization.
// Invalid documents fail closed rather than being declared unrelated.
func SnapshotAffected(raw []byte, in SourceInvalidation) (bool, error) {
	return snapshotAffected(raw, in, false)
}

// RevisionInvalidationStore erases only dependencies on one exact Memory
// revision. It never converts an erasure into a through-revision watermark.
type RevisionInvalidationStore interface {
	InvalidateRevision(context.Context, Reference) (int, error)
}

func SnapshotUsesRevision(raw []byte, ref Reference) (bool, error) {
	if !validReference(ref) {
		return false, Invalid
	}
	return snapshotAffected(raw, SourceInvalidation{Namespace: ref.Namespace, Collection: ref.Collection, Key: ref.Key, ThroughRevision: ref.Revision}, true)
}
func snapshotAffected(raw []byte, in SourceInvalidation, exact bool) (bool, error) {
	if !ValidInvalidation(in) {
		return false, Invalid
	}
	doc, err := parse(raw)
	if err != nil || len(doc.Dependencies) > 16 {
		return false, Invalidated
	}
	matched := false
	for _, dep := range doc.Dependencies {
		ref := dep.Candidate.Reference
		if !validReference(ref) {
			return false, Invalidated
		}
		switch dep.State {
		case "missing", "selected", "inapplicable", "trimmed":
		default:
			return false, Invalidated
		}
		if ref.Namespace == in.Namespace && ref.Collection == in.Collection && ref.Key == in.Key && ref.Revision <= in.ThroughRevision && (!exact || ref.Revision == in.ThroughRevision) && (dep.State == "missing") == in.Missing {
			matched = true
		}
	}
	return matched, nil
}
