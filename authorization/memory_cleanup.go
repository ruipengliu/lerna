package authorization

import (
	"context"
	"time"
)

// MemoryComparison identifies an original admission whose payload comparison
// basis the trusted Memory authority has already erased.
type MemoryComparison struct{ OperationID, Subject string }

// EraseMemoryComparisons is a trusted maintenance port, never a peer command.
// It removes comparison digests atomically but retains identities and actions
// needed for current authorization and historical result lookup. It does not
// establish whether a business operation committed; the caller proves that from
// Memory's deletion state before invoking this port.
func (s *Service) EraseMemoryComparisons(ctx context.Context, namespace string, entries []MemoryComparison) error {
	if namespace == "" || len(namespace) > 256 || len(entries) == 0 || len(entries) > 512 {
		return fail(Invalid)
	}
	entries = append([]MemoryComparison(nil), entries...)
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.OperationID == "" || len(entry.OperationID) > 512 || entry.Subject == "" || len(entry.Subject) > 256 || seen[entry.OperationID] {
			return fail(Invalid)
		}
		seen[entry.OperationID] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.update(ctx, func(st *State, now time.Time) error {
		if st.Namespace != namespace {
			return fail(Denied)
		}
		for _, entry := range entries {
			if _, err := windowOf(st, entry.OperationID); err != nil {
				return err
			}
			old, ok := st.MemoryOperations[entry.OperationID]
			if !ok {
				return fail(NotFound)
			}
			if old.Subject != entry.Subject || old.Action == nil || (old.Action.Action != "memory.put" && old.Action.Action != "memory.correct") {
				return fail(IdentityConflict)
			}
			old.SemanticSHA256 = ""
			st.MemoryOperations[entry.OperationID] = old
		}
		return nil
	})
}
