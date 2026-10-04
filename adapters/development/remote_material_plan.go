package development

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原 Task/Memory 准备事务只取得实际消费方的元数据。材料使用每个 Task 的
// 原提交者身份；计划不扩大 bundle.Contents，也不替代最终门禁。
func (a *App) remoteTaskMaterialGroupsTx(ctx context.Context, tx runtime.Tx, id string) ([]remoteCurrentGroup, []task.CurrentMaterialPreparation, error) {
	plan, err := a.Task.ReadCurrentMaterialPlanTx(ctx, tx, a.ServiceAuth, id)
	if err != nil {
		return nil, nil, err
	}
	groups := []remoteCurrentGroup{}
	for _, entry := range plan {
		contentRefs := []api.ContentRef{}
		memoryRefs := []api.ObjectRef{}
		memories := map[string]api.ObjectRef{}
		for _, material := range entry.Materials {
			contentRefs = append(contentRefs, material.ContentRef)
			if material.QueryRef != nil {
				contentRefs = append(contentRefs, *material.QueryRef)
			}
			if material.MemoryRef != nil {
				old, exists := memories[material.MemoryRef.ObjectID]
				if exists && old != *material.MemoryRef {
					return nil, nil, api.E("invalid_request", "context_memory_reference_conflict")
				}
				if !exists {
					memories[material.MemoryRef.ObjectID] = *material.MemoryRef
					memoryRefs = append(memoryRefs, *material.MemoryRef)
				}
			}
		}
		// 空引用仍核原 CheckMemories 的 head/当前主体合同。
		// 策略元数据不提供 Content 权限，也不进入设备 bundle。
		assertionRefs, err := a.Memory.MemorySourceRefsForPreparationTx(ctx, tx, entry.Auth, memoryRefs, "task.context")
		if err != nil {
			return nil, nil, err
		}
		if a.Memory.Location == "cloud" {
			contentRefs = append(contentRefs, assertionRefs...)
		} else if len(assertionRefs) > 0 {
			groups = append(groups, remoteCurrentGroup{entry.Auth, uniqueSources(assertionRefs), "task.context", a.Memory.Location})
		}
		if len(contentRefs) > 0 {
			groups = append(groups, remoteCurrentGroup{entry.Auth, uniqueSources(contentRefs), "task.context", "cloud"})
		}
	}
	return groups, plan, nil
}

// 最终原事务在 CheckTaskCurrentTx(true) 前重读准确 Task 元数据。
// Task/祖先材料或原主体变化不能借用旧计划的证明；MemoryRef 的准确
// 修订仍由原 CheckMemoriesTx 核验。
func (a *App) checkRemoteMaterialPlanTx(ctx context.Context, tx runtime.Tx, id string, plan []task.CurrentMaterialPreparation) error {
	actual, err := a.Task.ReadCurrentMaterialPlanTx(ctx, tx, a.ServiceAuth, id)
	if err != nil {
		return err
	}
	if !api.Equal(actual, plan) {
		return api.E("revision_conflict", "original_task_material_plan_changed")
	}
	return nil
}
