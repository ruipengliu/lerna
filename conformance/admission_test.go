package conformance_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/adapters/mockapi"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

func admissionRecords(t *testing.T, h *harness.Harness) []*lernav1.AdmissionRecord {
	t.Helper()
	var out []*lernav1.AdmissionRecord
	err := h.Host.Adjudication.Read(context.Background(), func(tx *durable.Tx) error {
		rows, err := tx.Query(`SELECT record FROM admissions ORDER BY created_at`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var b []byte
			if err := rows.Scan(&b); err != nil {
				return err
			}
			r := &lernav1.AdmissionRecord{}
			if err := proto.Unmarshal(b, r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func taskOf(t *testing.T, h *harness.Harness, res *lernav1.SubmitInputResult) *lernav1.TaskView {
	t.Helper()
	sv := h.Session(res.GetSessionId())
	if len(sv.GetTaskIds()) != 1 {
		t.Fatalf("want one task, got %v", sv.GetTaskIds())
	}
	return h.Task(sv.GetTaskIds()[0])
}

func waitingOn(tv *lernav1.TaskView, kind lernav1.WaitingKind) bool {
	for _, w := range tv.GetTask().GetWaitingOn() {
		if w.GetKind() == kind {
			return true
		}
	}
	return false
}

// 持久记录写明动作基于的条件版本、授权和预算；只有准入后的意图进入执行管理。
//
// 规则：G4、准入-7、准入-8
func TestAdmissionPersistsRequirementsGrantAndBudgetBasis(t *testing.T) {
	h := harness.New(t)
	grant := h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	recs := admissionRecords(t, h)
	if len(recs) == 0 {
		t.Fatal("no admission record")
	}
	r := recs[0]
	tv := taskOf(t, h, res)
	if r.GetRequirementsVersion() != 1 || r.GetInputVersion() != 1 || r.GetControlGeneration() == 0 {
		t.Fatalf("admission must fix requirement, input and control versions: %v", r)
	}
	if len(r.GetGrantRefs()) != 1 || r.GetGrantRefs()[0] != grant {
		t.Fatalf("admission must name the grant it used: %v", r.GetGrantRefs())
	}
	if r.GetBudgetRef() == "" || r.GetCeiling() != h.API.Price || r.GetReserved() != h.API.Price*int64(r.GetSendQuota()+r.GetQueryQuota()) {
		t.Fatalf("admission must record the budget basis and ceiling: %v", r)
	}
	if r.GetLedgerDomainId() != host.DomainLedger || r.GetExecutorEndpointId() != host.EndpointLocal {
		t.Fatalf("executor endpoint must be fixed at admission: %v", r)
	}
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM operations WHERE operation_id = ?`, r.GetOperationId()); n != 1 {
		t.Fatalf("ledger must durably accept the admitted intent, got %d", n)
	}
	var accepted bool
	for _, op := range tv.GetOperations() {
		if op.GetOperationId() == r.GetOperationId() {
			accepted = op.GetLedgerAccepted()
		}
	}
	if !accepted {
		t.Fatalf("the source must record the ledger's receipt (R7 step 3): %v", tv.GetOperations())
	}
}

// 提议本身不产生任何执行：没有授权时准入被拒绝，执行管理没有动作，目标没有收到请求。
//
// 规则：G6、R1、准入-7
func TestProposalWithoutGrantProducesNoExecution(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	if h.Reasoner.Calls() == 0 {
		t.Fatal("the reasoner must have been asked for a proposal")
	}
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM operations`); n != 0 {
		t.Fatalf("a proposal without admission must not reach the ledger, got %d operations", n)
	}
	if h.API.Received() != 0 {
		t.Fatalf("target received %d calls without admission", h.API.Received())
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM command_receipts WHERE command_kind = ? AND decision = ?`,
		ports.CommandAdmit, int32(lernav1.Decision_DECISION_REJECTED)); n != 1 {
		t.Fatalf("the rejection must be a durable decision, got %d", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM reservations`); n != 0 {
		t.Fatal("a rejected admission must not leave a budget reservation")
	}
	if tv := taskOf(t, h, res); !waitingOn(tv, lernav1.WaitingKind_WAITING_KIND_USER) {
		t.Fatalf("without a standing grant the task must wait for the user's confirmation: %v", tv.GetTask().GetWaitingOn())
	}
}

type unknownCeiling struct{ *mockapi.Adapter }

func (u unknownCeiling) Declarations() []*lernav1.CapabilityDeclaration {
	ds := u.Adapter.Declarations()
	for _, d := range ds {
		d.Billing = &lernav1.Billing{Billed: true}
	}
	return ds
}

// 费用上界未知的调用不准入。
//
// 规则：准入-8、G10
func TestUnknownCostCeilingIsNotAdmitted(t *testing.T) {
	var target *mockapi.Target
	h := harness.New(t, func(cfg *host.Config) {
		if target == nil {
			target = mockapi.NewTarget(mockapi.Idempotent, cfg.Clock.Now)
		}
		cfg.Executors = append(cfg.Executors, unknownCeiling{mockapi.New("pricey", target)})
	})
	h.GrantStanding("pricey.put")
	res := h.SubmitGoal(harness.NewID(), "调用上界未知的能力", &lernav1.RequirementSetDraft{
		TemplateId: "api_put", TemplateParams: map[string]string{"capability": "pricey.put", "key": "k", "value": "v"},
	})
	h.MustRun()
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM operations`); n != 0 {
		t.Fatalf("unknown ceiling must not be admitted")
	}
	if target.Received() != 0 {
		t.Fatal("target must not be called")
	}
	if tv := taskOf(t, h, res); !waitingOn(tv, lernav1.WaitingKind_WAITING_KIND_BUDGET) {
		t.Fatalf("task must wait on budget: %v", tv.GetTask().GetWaitingOn())
	}
}

// 规则：准入-8
func TestInsufficientBudgetIsNotAdmitted(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	h.MustAccept(harness.NewID(), ports.CommandSetBudget, &lernav1.SetBudgetCommand{
		Scope: lernav1.BudgetScope_BUDGET_SCOPE_USER, Limit: 10, ExpectedLimitVersion: 1,
	}, nil)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM operations`); n != 0 {
		t.Fatal("over-budget call must not be admitted")
	}
	if tv := taskOf(t, h, res); !waitingOn(tv, lernav1.WaitingKind_WAITING_KIND_BUDGET) {
		t.Fatalf("task must wait on budget: %v", tv.GetTask().GetWaitingOn())
	}
}
