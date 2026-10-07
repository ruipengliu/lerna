package ledger

import (
	"strings"
	"testing"
)

// 规则：R6、G1
func TestLedgerConstructionRejectsMissingStoreAndCompletionRequiresEveryLink(t *testing.T) {
	var absent *factsStore
	for _, store := range []Store{nil, absent} {
		service, err := New(store, "u", "d/ledger", "d")
		if err == nil || service != nil || !strings.Contains(err.Error(), "ledger.store") {
			t.Fatalf("missing store construction: %v %v", service, err)
		}
	}
	service, err := New(&factsStore{}, "u", "d/ledger", "d")
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name    string
		connect func()
	}{
		{"work", func() { service.WithWork(struct{ ExecutionWork }{}) }},
		{"compiler", func() { service.WithCompiler(struct{ Compiler }{}) }},
		{"rules", func() { service.WithEvidenceRules(struct{ EvidenceRules }{}) }},
		{"starts", func() { service.WithStarts(struct{ StartFacts }{}) }},
		{"observations", func() { service.WithObservations(struct{ ObservationContent }{}) }},
		{"usage", func() { service.WithReports(struct{ UsageReceiver }{}, nil, nil) }},
		{"usageReceipts", func() { service.WithReports(struct{ UsageReceiver }{}, struct{ ReceiptReader }{}, nil) }},
		{"trace", func() {
			service.WithReports(struct{ UsageReceiver }{}, struct{ ReceiptReader }{}, struct{ TraceReceiver }{})
		}},
		{"grantClosures", func() { service.WithGrantClosures(struct{ GrantClosures }{}) }},
		{"completionClosures", func() { service.WithCompletionClosures(struct{ CompletionClosureSource }{}) }},
		{"cancellationClosures", func() { service.WithCancellationClosures(struct{ CancellationClosureSource }{}) }},
		{"taskClosures", func() { service.WithTaskClosures(struct{ TaskClosureSource }{}) }},
		{"progressReceiver", func() { service.WithOperationProgress(struct{ OperationProgressReceiver }{}) }},
		{"reconciliationTasks", func() { service.WithReconciliation(struct{ ReconciliationTasks }{}, nil, nil) }},
		{"reconciliationGrants", func() {
			service.WithReconciliation(struct{ ReconciliationTasks }{}, struct{ ReconciliationGrants }{}, nil)
		}},
		{"reconciliationEgress", func() {
			service.WithReconciliation(struct{ ReconciliationTasks }{}, struct{ ReconciliationGrants }{}, struct{ ReconciliationEgress }{})
		}},
	}
	for _, step := range steps {
		if err := service.ValidateDependencies(); err == nil || !strings.Contains(err.Error(), "ledger."+step.name) {
			t.Fatalf("missing %s: %v", step.name, err)
		}
		step.connect()
	}
	if err := service.ValidateDependencies(); err != nil {
		t.Fatalf("complete connections rejected: %v", err)
	}
	var missingWork *struct{ ExecutionWork }
	service.WithWork(missingWork)
	if err := service.ValidateDependencies(); err == nil || !strings.Contains(err.Error(), "ledger.work") {
		t.Fatalf("typed nil connection accepted: %v", err)
	}
	service.WithWork(struct{ ExecutionWork }{})
	var missingRules *struct{ EvidenceRules }
	service.WithEvidenceRules(missingRules)
	if err := service.ValidateDependencies(); err == nil || !strings.Contains(err.Error(), "ledger.rules") {
		t.Fatalf("typed nil evidence rules accepted: %v", err)
	}
}
