package memory

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
)

// CheckMemoryTx 为同库消费者复核准确当前断言，不读正文或授予额外许可。
func (s *Service) CheckMemoryTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef, purpose string) (MemoryRecord, error) {
	rows, err := s.CheckMemoriesTx(ctx, tx, auth, []api.ObjectRef{ref}, purpose)
	if err != nil {
		return MemoryRecord{}, err
	}
	return rows[0], nil
}

// 空集合也固定同一 Memory 变更头与当前主体门禁；多个准确版本按同序取得。
func (s *Service) CheckMemoriesTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef, purpose string) ([]MemoryRecord, error) {
	if len(refs) > 64 {
		return nil, api.E("overloaded", "memory_reference_check_capacity")
	}
	seen := map[string]bool{}
	order := make([]int, len(refs))
	for i, ref := range refs {
		if err := runtime.CheckRef(tx.Scope(), ref); err != nil {
			return nil, err
		}
		if ref.OwnerID != tx.Scope().OwnerID {
			return nil, api.E("forbidden", "memory_owner_mismatch")
		}
		if seen[ref.ObjectID] {
			return nil, api.E("invalid_request", "duplicate_memory_reference")
		}
		seen[ref.ObjectID] = true
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return refs[order[i]].ObjectID < refs[order[j]].ObjectID })
	// 与 within 一致，先持有 Memory 变更头，再核当前主体及领域/Content 门禁。
	if _, err := loadHead(ctx, tx); err != nil {
		return nil, err
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return nil, err
	}
	rows := make([]MemoryRecord, len(refs))
	for _, i := range order {
		if _, err := tx.Get(ctx, "memory.records", refs[i].ObjectID, &rows[i]); err != nil {
			return nil, err
		}
	}
	for _, i := range order {
		if err := s.memoryAllowed(ctx, tx, auth, rows[i], purpose, true); err != nil {
			return nil, err
		}
		if rows[i].Revision != refs[i].Revision {
			return nil, api.E("gone", "memory_revision_superseded")
		}
	}
	return rows, nil
}
