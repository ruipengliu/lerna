package tasks

import "github.com/ruipengliu/lerna/core/durable"

// ValidateDependencies 在循环链接完成后、历史资格检查与恢复之前调用。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("tasks",
		durable.Dependency{Name: "store", Value: s.store},
		durable.Dependency{Name: "decisions", Value: s.decisions},
		durable.Dependency{Name: "closureJobs", Value: s.closureJobs},
		durable.Dependency{Name: "closureRecipient", Value: s.closureRecipient},
		durable.Dependency{Name: "taskCloseFacts", Value: s.taskCloseFacts},
		durable.Dependency{Name: "taskCloseBudget", Value: s.taskCloseBudget},
		durable.Dependency{Name: "reasonerWork", Value: s.reasonerWork},
		durable.Dependency{Name: "reasonerFactory", Value: s.reasonerFactory},
		durable.Dependency{Name: "conditionConfirmations", Value: s.conditionConfirmations},
		durable.Dependency{Name: "reasonerQuestions", Value: s.reasonerQuestions},
		durable.Dependency{Name: "modelLedger", Value: s.modelLedger},
		durable.Dependency{Name: "modelWork", Value: s.modelWork},
		durable.Dependency{Name: "modelCredentials", Value: s.modelCredentials},
		durable.Dependency{Name: "modelEgress", Value: s.modelEgress},
		durable.Dependency{Name: "modelContent", Value: s.modelContent},
		durable.Dependency{Name: "completionFacts", Value: s.completionFacts},
		durable.Dependency{Name: "completionBudget", Value: s.completionBudget},
		durable.Dependency{Name: "progressSource", Value: s.progressSource},
		durable.Dependency{Name: "closureSource", Value: s.closureSource},
		durable.Dependency{Name: "confirmationPublisher", Value: s.confirmationPublisher},
		durable.Dependency{Name: "confirmationContent", Value: s.confirmationContent},
		durable.Dependency{Name: "startGrants", Value: s.startGrants},
		durable.Dependency{Name: "startBudget", Value: s.startBudget},
		durable.Dependency{Name: "startExecution", Value: s.startExecution},
		durable.Dependency{Name: "grants", Value: s.grants},
		durable.Dependency{Name: "budget", Value: s.budget},
		durable.Dependency{Name: "content", Value: s.content},
		durable.Dependency{Name: "confirmations", Value: s.confirmations},
		durable.Dependency{Name: "scheduling", Value: s.scheduling},
		durable.Dependency{Name: "execution", Value: s.execution},
		durable.Dependency{Name: "handoffJobs", Value: s.handoffJobs},
		durable.Dependency{Name: "recipient", Value: s.recipient},
	)
}
