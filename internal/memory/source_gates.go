package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// MemorySourceGate 只收紧基于原断言的新派生使用，不改写原 Content 字节。
type MemorySourceGate struct {
	Revision  uint64           `json:"revision"`
	MemoryRef api.ObjectRef    `json:"memory_ref"`
	SourceRef api.ContentRef   `json:"source_ref"`
	State     string           `json:"state"`
	PolicyRef api.ComponentRef `json:"policy_ref"`
}

func sourceGate(ctx context.Context, tx runtime.Tx, record MemoryRecord, source api.ContentRef, state string, policy api.ComponentRef) error {
	id := semanticID("mgate", record.MemoryID+":"+contentKey(source))
	var gate MemorySourceGate
	rev, err := tx.Get(ctx, "memory.source_gates", id, &gate)
	if api.IsCode(err, "not_found") {
		return tx.Create(ctx, "memory.source_gates", id, contentKey(source), MemorySourceGate{Revision: 1, MemoryRef: tx.Scope().Ref(record.MemoryID, record.Revision), SourceRef: source, State: state, PolicyRef: policy})
	}
	if err != nil {
		return err
	}
	gate.Revision = rev + 1
	gate.MemoryRef = tx.Scope().Ref(record.MemoryID, record.Revision)
	gate.State = state
	gate.PolicyRef = policy
	return tx.Put(ctx, "memory.source_gates", id, rev, gate)
}

func (s *Service) checkSourceGate(ctx context.Context, tx runtime.Tx, auth runtime.Auth, source api.ContentRef, purpose, location string, continuous bool) error {
	rows, err := tx.List(ctx, "memory.source_gates", contentKey(source), "", 101)
	if err != nil {
		return err
	}
	if len(rows) > 100 {
		return api.E("dependency_unavailable", "source_gate_limit")
	}
	for _, row := range rows {
		var gate MemorySourceGate
		if err = row.Decode(&gate); err != nil {
			return err
		}
		if gate.State != "restricted" {
			return api.E("forbidden", "derived_assertion_needs_review")
		}
		if _, err = s.allowed(ctx, tx, auth, gate.PolicyRef, purpose, location, continuous); err != nil {
			return err
		}
	}
	return nil
}
