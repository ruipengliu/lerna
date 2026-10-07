package assembly

import (
	"github.com/ruipengliu/lerna/adapters/api"
	"github.com/ruipengliu/lerna/infra/rules"
)

// connect 只连接固定具体实现；真实循环在全部模块构造后补齐。
func (h *Harness) connect() {
	h.Tasks.WithDecisions(h.Durable)
	h.Content.WithAssociations(h.Tasks)
	h.Grants.WithAdmissions(h.Tasks)
	h.Grants.WithConfirmations(h.Sessions).WithConfirmationContent(h.Content)
	h.Sessions.WithConfirmations(h.Tasks, h.Grants)
	h.Tasks.WithConfirmationRequests(h.Sessions, h.Content).WithModelContent(h.Content).WithReasonerQuestions(h.Sessions).WithConditionConfirmations(h.Sessions)
	h.Ledger.WithWork(h.LedgerWork).WithCompiler(executionCompiler{api: api.Adapter{Content: h.Content}}).WithStarts(h.Tasks).WithEvidenceRules(rules.Fixed{})
	h.Content.WithObservations(h.Ledger)
	h.Ledger.WithObservations(h.Content)
	h.Budget.WithUsageSource(h.Ledger).WithBillingEvidence(h.Content).WithCompletionAuthority(h.Tasks).WithCancellationAuthority(h.Tasks).WithTaskClosingAuthority(h.Tasks)
	h.Ledger.WithReports(h.Budget, h.Durable, h.Trace)
	h.Tasks.WithStart(h.Grants, h.Budget, h.Ledger)
	h.Tasks.WithCompletion(h.Ledger, h.Budget)
	h.Tasks.WithModelExecution(h.Ledger, h.LedgerWork, h.Grants, h.Egress)
	h.Tasks.WithReasonerDriver(h.Durable, defaultReasoner)
	h.Ledger.WithGrantClosures(h.Grants)
	h.Grants.WithRevocationExits(h.Egress)
	h.Ledger.WithCompletionClosures(h.Tasks)
	h.Tasks.WithCompletionClosures(h.Durable, h.Egress)
	h.Ledger.WithCancellationClosures(h.Tasks)
	h.Tasks.WithCancellationClosures(h.Durable, h.Egress)
	h.Ledger.WithTaskClosures(h.Tasks)
	h.Tasks.WithTaskClosures(h.Durable, h.Egress).WithTaskClosingFacts(h.Ledger, h.Budget)
	h.Ledger.WithReconciliation(h.Tasks, h.Grants, h.Egress)
	h.Tasks.WithClosureSource(h.Ledger).WithOperationProgress(h.Ledger)
	h.Ledger.WithOperationProgress(h.Tasks)
	h.Tasks.WithAdmission(h.Grants, h.Budget, h.Content, h.Sessions, h.Durable, h.Ledger).WithHandoffs(h.Durable, h.Ledger)
}
