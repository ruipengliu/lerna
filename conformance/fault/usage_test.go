//go:build fault

package fault_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

// P7：用量交给预算按 R7 交接；回执丢失或崩溃后查询原交接，费用只计一次，预留不重复释放。
//
// 规则：G10、R7、G3
func TestUsageHandoffCountsOnceAcrossCrashesAndLostReceipts(t *testing.T) {
	for _, kind := range []string{ports.CommandReportUsage, ports.CommandCloseReservation} {
		for _, p := range []struct {
			point string
			mode  fault.Mode
		}{
			{"deliver:" + kind + ":receipt", fault.Lose},
			{"deliver:" + kind + ":receipt", fault.Crash},
			{"cmd:" + kind + ":after_commit", fault.Crash},
			{"cmd:" + kind + ":before_commit", fault.Crash},
			{"handoff:" + kind + ":record_receipt:before_commit", fault.Crash},
		} {
			t.Run(p.point+"/"+map[fault.Mode]string{fault.Crash: "crash", fault.Lose: "lose"}[p.mode], func(t *testing.T) {
				h := setup(t)
				h.GrantStanding("mockapi.put")
				h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
				fault.Inject(p.point, p.mode, 1)
				runUntilSettled(t, h)
				if fault.Count(p.point) == 0 {
					t.Fatalf("point %s never reached", p.point)
				}
				v, err := h.Client().Budget(context.Background(), harness.User)
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range v.GetBudgets() {
					if b.GetUsed() != h.API.Price || b.GetHeld() != 0 {
						t.Fatalf("budget %s: used %d held %d; want one charge and no leftover reservation", b.GetBudgetId(), b.GetUsed(), b.GetHeld())
					}
				}
				if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM billing_sources WHERE status = 'FINAL'`); n != 1 {
					t.Fatalf("billing sources = %d", n)
				}
			})
		}
	}
}
