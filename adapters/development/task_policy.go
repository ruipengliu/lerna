package development

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
)

// 默认政策按原完整值计算一次摘要；构造本身不读取数据库或执行外部 I/O。
// 初始准确 profile 和真正 App 装配复用同一规则，避免占位摘要再纠正重开。
func builtinTaskPolicy(c Config, answerSchema api.ComponentRef) task.TaskPolicy {
	policy := task.TaskPolicy{PolicyRef: component("task-policy"), ContinuationLimit: 30, RepairLimit: 3, NoProgressLimit: 8, ContextRoundLimit: 3, SafeAttemptLimit: 1, MaxRequirements: 20, MaxDelegations: 20, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600, InputPolicyRef: answerSchema, RuleRegistryRef: component("rule-registry")}
	if c.WASI != nil && c.WASI.CPUSecondsBudgetLimit != "" {
		policy.BudgetLimits = append(policy.BudgetLimits, api.Amount{Unit: "cpu_seconds", Value: c.WASI.CPUSecondsBudgetLimit})
	}
	policy.PolicyRef.Digest, _ = api.Digest(policy)
	return policy
}
