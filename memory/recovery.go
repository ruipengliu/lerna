package memory

import (
	"context"
	"encoding/hex"
	"unicode/utf8"
)

// RecoveryScope is a collection's existing authority scope. Recovery metadata
// is for trusted maintenance; it is not a user query or a synchronization grant.
type RecoveryScope struct{ Namespace, Collection string }

type RecoveryRecord struct {
	Version VersionRef
	SHA256  string
}

// RecoverySnapshot describes one consistent, complete bounded collection state.
// Records includes all retained revisions. Deleted includes permanent deletion
// revisions. Operations covers original writes/deletions within the next 512
// positions after After; exact erasures occupy positions in Erased instead.
// Position is the combined collection head at the same snapshot. Comparisons
// erased by deletion remain absent. No record bodies occur.
type RecoverySnapshot struct {
	Scope      RecoveryScope
	Position   uint64
	After      uint64
	Records    []RecoveryRecord
	Deleted    []VersionRef
	Operations []Receipt
	// Erased is the complete exact-erasure event inventory; SourceFences is the
	// complete namespace source cutoff inventory. Neither contains record bodies.
	Erased       []SourceEvent
	SourceFences []SourceErasure
}

// RecoveryStateSource belongs to the currently trusted authority, selected by
// host configuration outside the backup. A snapshot alone proves no freshness.
type RecoveryStateSource interface {
	RecoverySnapshot(context.Context, RecoveryScope, uint64) (RecoverySnapshot, error)
}

// A challenge is transient freshness material, not a new persistent operation.
type RecoveryChallenge struct {
	Scope RecoveryScope
	After uint64
	Nonce [32]byte
}

// RecoveryProof authenticates state only. It grants no disclosure, residency,
// synchronization, mutation or scheduling rights to the restored node.
type RecoveryProof struct {
	Authority string
	Epoch     uint64
	Challenge RecoveryChallenge
	Snapshot  RecoverySnapshot
	Signature []byte
}
type RecoverySource interface {
	ProveRecovery(context.Context, RecoveryChallenge) (RecoveryProof, error)
}
type RecoveryVerifier interface {
	VerifyRecovery(RecoveryChallenge, RecoveryProof) error
	Configuration() [32]byte
}

// RecoveryBinding is trusted host wiring outside the restored database.
type RecoveryBinding struct {
	Scope    RecoveryScope
	Source   RecoverySource
	Verifier RecoveryVerifier
}
type RecoveryProgress struct {
	Scope                                             RecoveryScope
	Authority                                         string
	Epoch                                             uint64
	OriginPosition, VerifiedPosition, AppliedPosition uint64
	Retained, Missing                                 int
	State                                             string
}

func ValidRecoveryScope(scope RecoveryScope) bool {
	return recoveryLabel(scope.Namespace) && recoveryLabel(scope.Collection)
}

func recoveryLabel(value string) bool { return text(value, 256) && utf8.ValidString(value) }

// Current retained/deleted state accounts for the whole revision history, while
// operation identities are verified page by page. Missing rows cannot silently
// become evidence of absence. This validates structure, not authority/freshness.
func ValidRecoverySnapshot(s RecoverySnapshot) bool {
	if !ValidRecoveryScope(s.Scope) || len(s.Records)+len(s.Deleted) > 512 || s.Position > 1<<32 || s.After > s.Position || len(s.Erased) > 512 || len(s.SourceFences) > 512 || (len(s.Erased) > 0 && len(s.SourceFences) == 0) {
		return false
	}
	fences := map[[2]string]bool{}
	for _, f := range s.SourceFences {
		key := [2]string{f.Kind, f.Key}
		if f.Namespace != s.Scope.Namespace || !recoveryLabel(f.Kind) || !recoveryLabel(f.Key) || f.ThroughRevision == 0 || fences[key] {
			return false
		}
		fences[key] = true
	}
	validRef := func(ref Ref) bool {
		return ref.Namespace == s.Scope.Namespace && ref.Collection == s.Scope.Collection && recoveryLabel(ref.Key)
	}
	hash := func(value string) bool {
		raw, err := hex.DecodeString(value)
		return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
	}
	deleted := map[Ref]uint64{}
	historySize := uint64(len(s.Records))
	for _, v := range s.Deleted {
		if !validRef(v.Ref) || v.Revision < 2 || v.Revision > 1<<32 || deleted[v.Ref] != 0 {
			return false
		}
		deleted[v.Ref] = v.Revision
		historySize += v.Revision
	}
	records := map[VersionRef]bool{}
	counts, maxima := map[Ref]uint64{}, map[Ref]uint64{}
	for _, record := range s.Records {
		v := record.Version
		if !validRef(v.Ref) || v.Revision == 0 || v.Revision > 1<<32 || records[v] || deleted[v.Ref] != 0 || !hash(record.SHA256) {
			return false
		}
		records[v] = true
		counts[v.Ref]++
		maxima[v.Ref] = max(maxima[v.Ref], v.Revision)
	}
	erased := map[VersionRef]SourceEvent{}
	erasedPositions := map[uint64]bool{}
	lastErasure := uint64(0)
	for _, event := range s.Erased {
		v := VersionRef{Ref: event.Ref, Revision: event.Revision}
		if !ValidSourceEvent(event) || event.Kind != SourceErased || !validRef(event.Ref) || event.Position > s.Position || event.Position <= lastErasure || event.Position <= event.Revision || records[v] || erased[v].Position != 0 {
			return false
		}
		lastErasure = event.Position
		if end := deleted[event.Ref]; end != 0 {
			if event.Revision >= end {
				return false
			}
		} else {
			counts[event.Ref]++
			maxima[event.Ref] = max(maxima[event.Ref], event.Revision)
			historySize++
		}
		erased[v] = event
		erasedPositions[event.Position] = true
		historySize++
	}
	if historySize != s.Position {
		return false
	}
	pageEnd := min(s.Position, s.After+512)
	expectedOperations := pageEnd - s.After
	for position := range erasedPositions {
		if position > s.After && position <= pageEnd {
			expectedOperations--
		}
	}
	if uint64(len(s.Operations)) != expectedOperations {
		return false
	}
	for ref, count := range counts {
		if count != maxima[ref] {
			return false
		}
	}
	heads := map[Ref]uint64{}
	ops := map[string]bool{}
	cursor := s.After
	for _, receipt := range s.Operations {
		cursor++
		for erasedPositions[cursor] {
			cursor++
		}
		if !validRef(receipt.Ref) || !recoveryLabel(receipt.OperationID) || !recoveryLabel(receipt.Subject) || ops[receipt.OperationID] || receipt.Position != cursor || receipt.Position > pageEnd || receipt.Revision == 0 || (heads[receipt.Ref] != 0 && receipt.Revision != heads[receipt.Ref]+1) || (heads[receipt.Ref] == 0 && receipt.Revision > s.After+1) {
			return false
		}
		v := VersionRef{Ref: receipt.Ref, Revision: receipt.Revision}
		if event := erased[v]; event.Position != 0 && receipt.Position >= event.Position {
			return false
		}
		if deleted[receipt.Ref] == receipt.Revision {
			for _, event := range s.Erased {
				if event.Ref == receipt.Ref && event.Position >= receipt.Position {
					return false
				}
			}
		}
		ops[receipt.OperationID] = true
		heads[receipt.Ref] = receipt.Revision
		if end := deleted[receipt.Ref]; end != 0 {
			if receipt.Revision > end || (receipt.Revision < end && receipt.SemanticSHA256 != "") || (receipt.Revision == end && !hash(receipt.SemanticSHA256)) {
				return false
			}
		} else {
			v := VersionRef{Ref: receipt.Ref, Revision: receipt.Revision}
			if erased[v].Position != 0 {
				if receipt.SemanticSHA256 != "" {
					return false
				}
			} else if !hash(receipt.SemanticSHA256) || !records[v] {
				return false
			}
		}
	}
	return true
}
