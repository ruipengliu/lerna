package conformance_test

import (
	"testing"

	"github.com/ruipengliu/lerna/adapters/mockapi"
	"github.com/ruipengliu/lerna/conformance/harness"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

func userBudget(v *lernav1.BudgetView) *lernav1.Budget {
	for _, b := range v.GetBudgets() {
		if b.GetScope() == lernav1.BudgetScope_BUDGET_SCOPE_USER {
			return b
		}
	}
	return nil
}

func taskBudget(v *lernav1.BudgetView) *lernav1.Budget {
	for _, b := range v.GetBudgets() {
		if b.GetScope() == lernav1.BudgetScope_BUDGET_SCOPE_TASK {
			return b
		}
	}
	return nil
}

// 用量回报后结清、释放未用的预留；任务关闭后任务预算不再接受新消耗。
//
// 规则：G10、准入-8
func TestUsageSettlesReservationOnTheNormalPath(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	v := h.Budgets()
	u, tb := userBudget(v), taskBudget(v)
	if u.GetUsed() != h.API.Price || u.GetHeld() != 0 || tb.GetUsed() != h.API.Price || tb.GetHeld() != 0 || !tb.GetClosed() {
		t.Fatalf("one charge settled, the unused reservation released, task budget closed: user %v task %v", u, tb)
	}
	if len(v.GetSources()) != 1 || v.GetSources()[0].GetStatus() != "FINAL" || v.GetSources()[0].GetNativeId() == "" {
		t.Fatalf("the billing source must be final with its provider alias: %v", v.GetSources())
	}
}

// 已收费但回执丢失：费用记为未知而不是零，预留继续占用；迟到的账单被接纳并结清，
// 重复账单不重复计费，超出上界的实际费用照常入账并显示缺口。
//
// 规则：G10、G1
func TestUnknownChargeLateBillDuplicateAndOverspend(t *testing.T) {
	h := harness.NewWithAPI(t, mockapi.Unqueryable)
	h.API.InjectOnCall(1, mockapi.LoseAfterCommit)
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	v := h.Budgets()
	u := userBudget(v)
	if u.GetUsed() != 0 || u.GetHeld() != h.API.Price || u.GetUnknownSources() != 1 {
		t.Fatalf("an unknown charge keeps its reservation and is not counted as zero: %v", u)
	}
	call := h.API.Calls()[0]
	bill := &lernav1.UsageReport{BillingSource: "mockapi", NativeId: call.RequestID, ExternalKey: call.ExternalKey,
		Unit: "micro_usd", Amount: h.API.Price, Final: true, Measurement: lernav1.Measurement_MEASUREMENT_CUMULATIVE}
	h.Bill("bill-1", bill)
	h.Bill("bill-1-resent-by-provider", bill)
	v = h.Budgets()
	u = userBudget(v)
	if u.GetUsed() != h.API.Price || u.GetHeld() != 0 || u.GetUnknownSources() != 0 {
		t.Fatalf("the late bill settles the source exactly once: %v", u)
	}
	// 供应商事后向上更正：实际费用超出上界，照常入账并显示缺口。
	over := &lernav1.UsageReport{BillingSource: "mockapi", NativeId: call.RequestID, Unit: "micro_usd",
		Amount: 2_000_000, Final: true, Measurement: lernav1.Measurement_MEASUREMENT_CORRECTION}
	h.Bill("bill-1-correction", over)
	u = userBudget(h.Budgets())
	if u.GetUsed() != 2_000_000 || u.GetDeficit() == 0 || u.GetAvailable() != 0 {
		t.Fatalf("overspend must be recorded and visible: %v", u)
	}
	// 缺口阻止新的准入。
	res := h.SubmitGoal(harness.NewID(), "g2", apiPutDraft("k2", "v2"))
	h.MustRun()
	if !waitingOn(taskOf(t, h, res), lernav1.WaitingKind_WAITING_KIND_BUDGET) {
		t.Fatal("a budget in deficit must stop new admissions")
	}
}

// 迟到的账单在任务关闭之后仍被接纳；Result 不变。
//
// 规则：G10、G11
func TestLateBillAfterCloseLeavesResultUnchanged(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	before := resultBlob(t, h, res)
	call := h.API.Calls()[0]
	h.Bill("late-bill", &lernav1.UsageReport{BillingSource: "mockapi", NativeId: call.RequestID, Unit: "micro_usd",
		Amount: h.API.Price + 40, Final: true, Measurement: lernav1.Measurement_MEASUREMENT_CORRECTION})
	h.MustRun()
	if u := userBudget(h.Budgets()); u.GetUsed() != h.API.Price+40 {
		t.Fatalf("the upward correction after close must be accepted: %v", u)
	}
	if string(before) != string(resultBlob(t, h, res)) {
		t.Fatal("a late bill must not rewrite the Result")
	}
}

// 无法关联到任何来源的账单进入待核对，不凭金额相等自动匹配。
//
// 规则：G10
func TestUnmatchedBillIsKeptForReconciliation(t *testing.T) {
	h := harness.New(t)
	h.Bill("stray", &lernav1.UsageReport{BillingSource: "mockapi", NativeId: "req-unknown", Unit: "micro_usd", Amount: 7, Final: true})
	v := h.Budgets()
	if len(v.GetUnmatchedBills()) != 1 || userBudget(v).GetUsed() != 0 {
		t.Fatalf("stray bill must be kept unmatched: %v", v)
	}
	rec, err := h.Submit("forged-bill", ports.CommandIngestBill, &lernav1.UsageReport{ReportId: "x", UserId: harness.User})
	if err != nil || rec.GetRejection().GetCode() != lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Fatalf("only a trusted billing integration may report bills: %v %v", rec, err)
	}
}
