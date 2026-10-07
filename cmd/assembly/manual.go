package assembly

import "github.com/ruipengliu/lerna/infra/hosting"

// connectManualProgress 只把既有负责方接入宿主，不改变启动资格或恢复阶段。
func (h *Harness) connectManualProgress() error {
	var e error
	h.Recovery, e = hosting.NewManual(hosting.ManualDependencies{
		Sessions: h.Sessions, Observations: h.Content, Reports: h.Ledger,
		Revocations: h.Grants, Completions: h.Tasks, Cancellations: h.Tasks,
		Reconciliations: h.Ledger, TaskClosings: h.Tasks, BudgetClosures: h.Budget,
		ExecutionFollowups: h.Ledger, SettlementFollowups: h.Budget, Trace: h.Trace,
	})
	return e
}
