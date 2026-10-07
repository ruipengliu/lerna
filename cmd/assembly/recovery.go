package assembly

import "github.com/ruipengliu/lerna/infra/hosting"

// connectRecovery 只把固定的原负责方接入两个宿主模式。
func (h *Harness) connectRecovery() error {
	var e error
	h.Recovery, e = hosting.New(hosting.ManualDependencies{
		Sessions: h.Sessions, Observations: h.Content, Reports: h.Ledger,
		Revocations: h.Grants, Completions: h.Tasks, Cancellations: h.Tasks,
		Reconciliations: h.Ledger, TaskClosings: h.Tasks, BudgetClosures: h.Budget,
		ExecutionFollowups: h.Ledger, SettlementFollowups: h.Budget, Trace: h.Trace,
	}, hosting.StartupDependencies{
		User: h.user, TasksCompatibility: h.Tasks, LedgerCompatibility: h.Ledger,
		Sessions: h.Sessions, Handoffs: h.Tasks, Registrations: h.Content,
		Observations: h.Content, Reports: h.Ledger, Revocations: h.Grants,
		Cancellations: h.Tasks, Completions: h.Tasks, Reconciliations: h.Ledger,
		TaskClosures: h.Tasks, TaskClosings: h.Tasks, BudgetClosures: h.Budget,
		ExecutionFollowups: h.Ledger, SettlementFollowups: h.Budget, Drivers: h.Tasks,
		Trace: h.Trace,
	})
	return e
}
