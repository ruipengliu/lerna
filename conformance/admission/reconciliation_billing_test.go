package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G10、G11、准入-8
func TestQueryBillingUsesItsOwnSendAndUnknownFeeRetainsHold(t *testing.T) {
	for _, withhold := range []bool{false, true} {
		name := "known"
		if withhold {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			target := simulator.NewBillingTarget(3)
			target.Target = simulator.New("queryable")
			target.DropReceipt(true)
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			original, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			target.DropReceipt(false)
			target.WithholdBill(withhold)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, original, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.OperationId)
			if e != nil || plan.State != "COMPLETED" {
				t.Fatalf("query effect: %v %v", plan, e)
			}
			relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			read, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			write, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			assertBilling := func() {
				t.Helper()
				originalSource, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, write.Execution.Send.Ref)
				if e != nil || originalSource.Amount != nil || originalSource.Status != "PENDING" {
					t.Fatalf("query settled original fee: %v %v", originalSource, e)
				}
				source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, read.Execution.Send.Ref)
				if e != nil || proto.Equal(source.Ref.Name, originalSource.Ref.Name) || !proto.Equal(source.OperationId, read.Ref.Name) {
					t.Fatalf("query billing identity: %v %v", source, e)
				}
				settled, reserved := int64(3), int64(30)
				if withhold {
					settled, reserved = 0, 35
					if source.Amount != nil || source.Status != "PENDING" {
						t.Fatalf("unknown query fee became zero: %v", source)
					}
				} else if source.Amount == nil || *source.Amount != 3 || source.Status != "SETTLED" {
					t.Fatalf("own query bill not settled: %v", source)
				}
				for _, task := range []*v1.GlobalName{nil, original.TaskId} {
					budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, task)
					if e != nil || budget.Settled != settled || budget.Reserved != reserved {
						t.Fatalf("query projection: %v %v", budget, e)
					}
				}
			}
			assertBilling()
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			assertBilling()
			bills := target.Bills()
			requests, effects := target.Target.Snapshot()
			if len(bills) != 2 || bills[0].SendID == bills[1].SendID || bills[1].SendID != read.Execution.Send.Ref.Name.LocalId || len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
				t.Fatalf("target billing: %v requests %v effects %v", bills, requests, effects)
			}
		})
	}
}
