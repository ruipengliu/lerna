package task

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 原根锁保护子树索引；先按层、按 ID 取得全部根到叶锁，再进入领域门禁。
// 终态子 Task 仍可有未知效果，因此定位包含其完整有界后继，不能靠活动分页猜关闭。
func (s *Service) lockTaskTree(ctx context.Context, tx runtime.Tx, rootID string) error {
	level := []string{rootID}
	seen := map[string]bool{}
	active := uint64(0)
	for len(level) != 0 {
		sort.Strings(level)
		next := []string{}
		for _, id := range level {
			if seen[id] {
				return api.E("dependency_unavailable", "subtree_cycle")
			}
			seen[id] = true
			if uint64(len(seen)) > s.config.MaxRelations+1 {
				return api.E("dependency_unavailable", "subtree_index_capacity")
			}
			current, err := getTask(ctx, tx, id)
			if err != nil {
				return err
			}
			if !terminal(current) && id != rootID {
				active++
				if active > s.config.MaxActiveSubtree {
					return api.E("dependency_unavailable", "subtree_index_capacity")
				}
			}
			rows, err := s.fullRelations(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.Kind == "child" {
					if row.Ref.OwnerID != tx.Scope().OwnerID || row.Ref.TenantID != tx.Scope().TenantID {
						return api.E("forbidden", "child_subtree_scope_mismatch")
					}
					next = append(next, row.Ref.ObjectID)
				}
			}
		}
		level = next
	}
	return nil
}
