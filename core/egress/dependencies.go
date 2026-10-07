package egress

import "github.com/ruipengliu/lerna/core/durable"

// ValidateDependencies 验证实际出口所需的全部连接，包含 typed nil。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("egress",
		durable.Dependency{Name: "starts", Value: s.starts},
		durable.Dependency{Name: "ledger", Value: s.ledger},
		durable.Dependency{Name: "content", Value: s.content},
		durable.Dependency{Name: "io", Value: s.io},
		durable.Dependency{Name: "critical", Value: s.critical},
	)
}
