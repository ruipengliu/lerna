package budget

import "github.com/ruipengliu/lerna/core/durable"

// ValidateDependencies 在循环连接完成后、兼容检查和业务恢复前验证必需依赖。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("budget",
		durable.Dependency{Name: "store", Value: s.store},
		durable.Dependency{Name: "decisions", Value: s.decisions},
		durable.Dependency{Name: "usageSource", Value: s.usageSource},
		durable.Dependency{Name: "evidence", Value: s.evidence},
		durable.Dependency{Name: "completionAuthority", Value: s.completionAuthority},
		durable.Dependency{Name: "cancellationAuthority", Value: s.cancellationAuthority},
		durable.Dependency{Name: "taskClosingAuthority", Value: s.taskClosingAuthority},
	)
}
