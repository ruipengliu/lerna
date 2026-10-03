package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type IndexStatus struct {
	ChangeHead          uint64           `json:"change_head"`
	ContiguousWatermark uint64           `json:"contiguous_watermark"`
	IndexGeneration     uint64           `json:"index_generation"`
	StrategyRef         api.ComponentRef `json:"strategy_ref"`
	State               string           `json:"state"`
	Mode                string           `json:"mode"`
}

// IndexStatus 只报告已提交连续元数据投影；查询始终有界补扫当前权威。
func (s *Service) IndexStatus(ctx context.Context, scope runtime.Scope, auth runtime.Auth) (IndexStatus, error) {
	var out IndexStatus
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		if !auth.HasRole("memory_admin") {
			return api.E("forbidden", "memory_management_required")
		}
		head, err := loadHead(ctx, tx)
		if err != nil {
			return err
		}
		var projection ProjectionState
		_, err = tx.Get(ctx, "memory.index_heads", scope.OwnerID, &projection)
		if api.IsCode(err, "not_found") {
			projection = ProjectionState{IndexGeneration: 1, StrategyRef: LexicalProfile(), State: "building"}
		} else if err != nil {
			return err
		}
		out = IndexStatus{ChangeHead: head.ChangeHead, ContiguousWatermark: projection.ContiguousWatermark, IndexGeneration: projection.IndexGeneration, StrategyRef: projection.StrategyRef, State: projection.State, Mode: "metadata_authority_scan"}
		return nil
	})
	return out, err
}
