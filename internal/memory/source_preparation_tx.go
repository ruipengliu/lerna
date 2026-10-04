package memory

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
)

// MemorySourceRefsForPreparationTx 向有界受信准备方暴露准确当前记录的来源
// 元数据，不授权 Content、不跳过来源闭包、不签发证明、不登记 holder、不读正文。
// 最终 CheckMemoriesTx 仍以原主体核验准确断言修订及全部 Content。
func (s *Service) MemorySourceRefsForPreparationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef, purpose string) ([]api.ContentRef, error) {
	if len(refs) > 64 {
		return nil, api.E("overloaded", "memory_reference_check_capacity")
	}
	seen := map[string]bool{}
	order := append([]api.ObjectRef{}, refs...)
	for _, ref := range order {
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
	}
	sort.Slice(order, func(i, j int) bool { return order[i].ObjectID < order[j].ObjectID })
	// 与权威 CheckMemoriesTx 保持相同的 head/主体/记录锁序。
	var head ChangeHead
	if _, err := tx.Get(ctx, "memory.heads", tx.Scope().OwnerID, &head); err != nil {
		return nil, err
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return nil, err
	}
	rows := make([]MemoryRecord, len(order))
	for i, ref := range order {
		if _, err := tx.Get(ctx, "memory.records", ref.ObjectID, &rows[i]); err != nil {
			return nil, err
		}
	}
	sources := []api.ContentRef{}
	unique := map[api.ContentRef]bool{}
	for i, record := range rows {
		if record.Revision != order[i].Revision {
			return nil, api.E("gone", "memory_revision_superseded")
		}
		if record.State == "deleted" {
			return nil, api.E("gone", "memory_deleted")
		}
		if record.State != "active" {
			return nil, api.E("forbidden", "memory_not_active")
		}
		// 先核记录自身的当前元数据策略；memoryAllowed/CheckContent 留给
		// 已取得 Current 的最终消费事务。
		if _, err := s.allowed(ctx, tx, auth, record.Values.PolicyRef, purpose, s.Location, true); err != nil {
			return nil, err
		}
		exact := append([]api.ContentRef{record.Values.ContentRef, record.Values.ScopeRef}, sourceRefs(record.Values.Sources)...)
		for _, ref := range exact {
			if ref.TenantID != tx.Scope().TenantID {
				return nil, api.E("forbidden", "reference_scope_mismatch")
			}
			if err := api.ValidateRecord("ContentRef", ref); err != nil {
				return nil, err
			}
			if !unique[ref] {
				unique[ref] = true
				sources = append(sources, ref)
			}
		}
	}
	return sources, nil
}
