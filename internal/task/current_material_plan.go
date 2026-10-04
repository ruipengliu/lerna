package task

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 准备事实不提供许可或 Current 结论。Auth 是每个 Task 保存的原提交者身份，
// 包括原凭据代次和角色。
type CurrentMaterialPreparation struct {
	TaskRef         api.ObjectRef
	GoalRevision    uint64
	ControlRevision uint64
	Auth            runtime.Auth
	Materials       []ContextMaterial
}

// ReadCurrentMaterialPlanTx 只读本 owner 元数据。getTask 按原根到叶路径锁定
// 并核验最多 32 个祖先，不扫描表。外部准备后仍须 CheckTaskCurrentTx(true)。
func (s *Service) ReadCurrentMaterialPlanTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) ([]CurrentMaterialPreparation, error) {
	t, err := getTask(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = principal(auth, t); err != nil {
		return nil, err
	}
	// 未装配 ContextMaterialGate 时，CheckCurrent 不消费材料。
	if _, ok := s.ports.ContextLookup.(ContextMaterialGate); !ok {
		return []CurrentMaterialPreparation{}, nil
	}
	tasks := []taskState{t}
	for _, ancestorID := range t.Ancestors {
		ancestor, err := getTask(ctx, tx, ancestorID)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, ancestor)
	}
	plan := make([]CurrentMaterialPreparation, 0, len(tasks))
	for _, actual := range tasks {
		if len(actual.ContextMaterials) > 64 {
			return nil, api.E("overloaded", "context_material_capacity")
		}
		if actual.SubmitterGeneration == 0 {
			return nil, api.E("forbidden", "submitter_generation_unavailable")
		}
		entry := CurrentMaterialPreparation{TaskRef: taskRef(tx, actual), GoalRevision: actual.Task.GoalRevision, ControlRevision: actual.Task.ControlRevision, Auth: submitterAuth(tx.Scope(), actual)}
		if err = api.Decode(api.Raw(actual.ContextMaterials), &entry.Materials); err != nil {
			return nil, err
		}
		plan = append(plan, entry)
	}
	return plan, nil
}
