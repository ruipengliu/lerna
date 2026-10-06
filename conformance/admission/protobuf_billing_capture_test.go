package admission_test

import (
	"encoding/json"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func captureBilling(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.NewBillingTarget(25)
	target.SetNativeInstance("same-provider-line")
	f := newFixtureWithTarget(t, 150, 150, false, target)
	a, start := prepareStart(t, f)
	receipt, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	execution, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
	captureObject(t, samples, "settled-billing-source", source, err)
	entry, err := f.h.Budget.QueryBillingEntry(f.ctx, f.caller, source.EntryRef)
	captureObject(t, samples, "billing-entry", entry, err)
	if entry.Amount != 25 || entry.Released != 5 {
		t.Fatal(entry)
	}
	f.suffix = "conflict"
	a, start = prepareStart(t, f)
	receipt, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	execution, err = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	source, err = f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
	captureObject(t, samples, "conflicted-billing-source", source, err)
	if source.Status != "CONFLICT" {
		t.Fatal(source)
	}
	conflict, err := f.h.Budget.QueryBillingConflict(f.ctx, f.caller, source.ConflictRef)
	captureObject(t, samples, "billing-conflict", conflict, err)
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	captureObject(t, samples, "billing-blocked-budget", budget, err)
	if budget.Settled != 25 || budget.Reserved != 30 || !budget.BillingBlocked || len(target.Bills()) != 2 {
		t.Fatalf("%v bills=%d", budget, len(target.Bills()))
	}
	adjust := &v1.AdjustBudgetLimitCommand{Header: header("capture-limit"), ExpectedRef: budget.Ref, Limit: 20, Reason: "synthetic limit change"}
	receipt, err = f.h.Budget.AdjustLimit(f.ctx, f.caller, adjust)
	accepted(t, receipt, err)
	captureObject(t, samples, "adjust-budget-command", adjust, nil)
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	captureObject(t, samples, "deficit-budget", budget, err)
	if budget.Deficit != 35 {
		t.Fatal(budget)
	}

	// 回执丢失后的账单是新的证据，不得重新发送原动作。
	late := simulator.NewBillingTarget(120)
	late.DropReceipt(true)
	f = newFixtureWithTarget(t, 100, 80, false, late)
	a, start = prepareStart(t, f)
	receipt, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	execution, err = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	source, err = f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
	captureObject(t, samples, "unknown-billing-source", source, err)
	body, err := json.Marshal(map[string]any{"billing": late.Bills()[0]})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("capture-bill-evidence").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
	if err != nil {
		t.Fatal(err)
	}
	bill := &v1.ImportBillCommand{Header: header("capture-import-bill"), SendRef: execution.Send.Ref, EvidenceRef: evidence}
	receipt, err = f.h.Budget.ImportBill(f.ctx, f.caller, bill)
	accepted(t, receipt, err)
	captureObject(t, samples, "import-bill-command", bill, nil)
	source, err = f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
	captureObject(t, samples, "late-over-ceiling-source", source, err)
	operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "unknown-operation-after-bill", operation, err)
	if operation.Effect.Outcome != "UNKNOWN" || len(late.Bills()) != 1 || f.calls.Load() != 1 {
		t.Fatal("bill changed effect or sent again")
	}
}
