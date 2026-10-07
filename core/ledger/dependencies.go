package ledger

import "github.com/ruipengliu/lerna/core/durable"

// ValidateDependencies 在循环连接完成后、历史兼容检查和恢复前验证必需依赖。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("ledger",
		durable.Dependency{Name: "store", Value: s.store},
		durable.Dependency{Name: "work", Value: s.work},
		durable.Dependency{Name: "compiler", Value: s.adapter},
		durable.Dependency{Name: "rules", Value: s.rules},
		durable.Dependency{Name: "starts", Value: s.starts},
		durable.Dependency{Name: "observations", Value: s.observations},
		durable.Dependency{Name: "usage", Value: s.usage},
		durable.Dependency{Name: "usageReceipts", Value: s.usageReceipts},
		durable.Dependency{Name: "trace", Value: s.trace},
		durable.Dependency{Name: "grantClosures", Value: s.grantClosures},
		durable.Dependency{Name: "completionClosures", Value: s.completionClosures},
		durable.Dependency{Name: "cancellationClosures", Value: s.cancellationClosures},
		durable.Dependency{Name: "taskClosures", Value: s.taskClosures},
		durable.Dependency{Name: "progressReceiver", Value: s.progressReceiver},
		durable.Dependency{Name: "reconciliationTasks", Value: s.reconciliationTasks},
		durable.Dependency{Name: "reconciliationGrants", Value: s.reconciliationGrants},
		durable.Dependency{Name: "reconciliationEgress", Value: s.reconciliationEgress},
	)
}
