package memorywire

import (
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func DeletionReport(r memory.DeletionStatus) *wire.MemoryDeletionReport {
	convert := func(items []memory.CleanupProgress) []*wire.MemoryCleanupProgress {
		out := make([]*wire.MemoryCleanupProgress, 0, len(items))
		for _, p := range items {
			out = append(out, &wire.MemoryCleanupProgress{Name: p.Name, State: p.State, Position: p.Position})
		}
		return out
	}
	return &wire.MemoryDeletionReport{Authority: r.Authority, Revision: r.Event.Revision, ChangePosition: r.Event.Position, Local: convert(r.Local), Replicas: convert(r.Replicas), DerivedArchives: convert(r.Derived)}
}

// ValidDeletionState rejects mismatched identity and inconsistent completion
// claims. It cannot prove a remote host's honesty or undisclosed inventory.
func ValidDeletionState(q *wire.MemoryDeletionQuery, s *wire.MemoryDeletionState) bool {
	if q == nil || s == nil || s.OperationId != q.OperationId || s.Purpose != q.Purpose || !proto.Equal(s.Ref, q.Ref) {
		return false
	}
	switch s.State {
	case "not_admitted", "admission_expired", "unknown":
		return s.Report == nil
	case "committed":
	default:
		return false
	}
	r := s.Report
	if r == nil || r.Authority != "committed" || r.Revision < 2 || r.Revision > 1<<32 || r.ChangePosition == 0 || r.ChangePosition > 1<<63-1 {
		return false
	}
	if len(r.Local)+len(r.Replicas)+len(r.DerivedArchives) > 35 {
		return false
	}
	for _, group := range [][]*wire.MemoryCleanupProgress{r.Local, r.Replicas, r.DerivedArchives} {
		if len(group) < 1 || len(group) > 32 {
			return false
		}
		seen := map[string]bool{}
		for _, p := range group {
			if p == nil || p.Name == "" || len(p.Name) > 256 || seen[p.Name] || p.Position > 1<<63-1 {
				return false
			}
			seen[p.Name] = true
			switch p.State {
			case "not_covered":
				if p.Position != 0 {
					return false
				}
			case "pending":
				if p.Position >= r.ChangePosition {
					return false
				}
			case "applied":
				if p.Position < r.ChangePosition {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}
