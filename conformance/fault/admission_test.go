//go:build fault

package fault_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

// R7：裁决域记下意图 → 执行管理持久接纳（P2）→ 裁决域记下回执。在三步之间分别崩溃和丢回执，
// 恢复后查询原交接，不新建动作。同进程也不省略回执，不跨域共用事务。
//
// 规则：R7、G3、G11
func TestIntentHandoffSurvivesCrashAndReceiptLossAtEveryStep(t *testing.T) {
	kind := ports.CommandAcceptIntent
	cases := []struct {
		point string
		mode  fault.Mode
	}{
		{"cmd:" + ports.CommandAdmit + ":before_commit", fault.Crash},
		{"cmd:" + ports.CommandAdmit + ":after_commit", fault.Crash},
		{"cmd:" + ports.CommandAdmit + ":after_commit", fault.Lose},
		{"cmd:" + kind + ":before_commit", fault.Crash},
		{"cmd:" + kind + ":after_commit", fault.Crash},
		{"deliver:" + kind + ":receipt", fault.Lose},
		{"deliver:" + kind + ":receipt", fault.Crash},
		{"handoff:" + kind + ":record_receipt:before_commit", fault.Crash},
		{"handoff:" + kind + ":record_receipt:after_commit", fault.Crash},
	}
	for _, c := range cases {
		name := c.point
		if c.mode == fault.Lose {
			name += "/lose"
		}
		t.Run(name, func(t *testing.T) {
			h := setup(t)
			h.GrantStanding("mockapi.put")
			h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
			fault.Inject(c.point, c.mode, 1)
			runUntilSettled(t, h)
			if fault.Count(c.point) == 0 {
				t.Fatalf("fault point %s was never reached", c.point)
			}
			if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`); n != 1 {
				t.Fatalf("admissions = %d, want 1", n)
			}
			if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM operations`); n != 1 {
				t.Fatalf("ledger operations = %d, want exactly 1 (no new intent after recovery)", n)
			}
			if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM handoffs WHERE command_kind = ? AND state = 'ACCEPTED'`, kind); n != 1 {
				t.Fatalf("the original handoff must end up with the ledger's receipt, got %d", n)
			}
			if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM command_receipts WHERE command_kind = ?`, kind); n != 1 {
				t.Fatalf("ledger receipts = %d, want 1", n)
			}
		})
	}
}
