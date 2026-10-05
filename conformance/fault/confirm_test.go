//go:build fault

package fault_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

// 交互适配器 M1 演示 2：确认回执丢失，恢复后只消费一次。
//
// 规则：G3、准入-9
func TestConfirmationReceiptLostIsConsumedOnce(t *testing.T) {
	for _, p := range []struct {
		point string
		mode  fault.Mode
	}{
		{"deliver:" + ports.CommandSubmitInput + ":receipt", fault.Lose},
		{"cmd:" + ports.CommandSubmitInput + ":after_commit", fault.Crash},
		{"cmd:" + ports.CommandAdmit + ":after_commit", fault.Crash},
	} {
		t.Run(p.point, func(t *testing.T) {
			h := setup(t)
			res := h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
			h.MustRun()
			sv := h.Session(res.GetSessionId())
			if len(sv.GetPendingConfirmations()) != 1 {
				t.Fatalf("want a pending confirmation")
			}
			c := sv.GetPendingConfirmations()[0]
			cmd := &lernav1.SubmitInputCommand{SessionId: c.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_CONFIRMATION,
				RequestId: c.GetConfirmationId(), Approve: true, IntentFingerprint: c.GetIntentFingerprint()}
			id := harness.NewID()
			fault.Inject(p.point, p.mode, 1)
			submitUntilDecided(t, h, id, cmd)
			runUntilSettled(t, h)
			submitUntilDecided(t, h, id, cmd)
			runUntilSettled(t, h)
			if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM confirmations WHERE status = ?`,
				int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_CONSUMED)); n != 1 {
				t.Fatalf("consumed confirmations = %d, want 1", n)
			}
			if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`); n != 1 {
				t.Fatalf("admissions = %d, want 1", n)
			}
			if h.API.Received() != 1 {
				t.Fatalf("target received %d, want 1", h.API.Received())
			}
		})
	}
}
