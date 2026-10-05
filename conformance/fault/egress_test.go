//go:build fault

package fault_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/adapters/mockapi"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

func ledgerOp(t *testing.T, h *harness.Harness) *lernav1.Operation {
	t.Helper()
	var opID string
	if err := h.Host.Ledger.Read(context.Background(), func(tx *durable.Tx) error {
		return tx.QueryRow(`SELECT operation_id FROM operations`).Scan(&opID)
	}); err != nil {
		t.Fatal(err)
	}
	r, err := h.Host.LedgerModule.Record(context.Background(), harness.User, opID)
	if err != nil {
		t.Fatal(err)
	}
	return r.GetOperation()
}

const executeIO = "egress:mockapi.put:SEND_PURPOSE_EXECUTE"

// 在 P4 之前、P4 与 P5 之间崩溃：没有 P5 就没有外部 I/O；恢复后用原发送身份继续，
// 开始回执丢失时查询原发送，不重复占用发送额度，目标只收到一次请求。
//
// 规则：G1、G3、G5、开始-5
func TestCrashBeforeDispatchPossibleNeverLosesOrDuplicatesTheSend(t *testing.T) {
	points := []struct {
		point string
		mode  fault.Mode
	}{
		{"cmd:" + ports.CommandStartSend + ":before_commit", fault.Crash},
		{"cmd:" + ports.CommandStartSend + ":after_commit", fault.Crash},
		{"deliver:" + ports.CommandStartSend + ":receipt", fault.Lose},
		{"deliver:" + ports.CommandStartSend + ":receipt", fault.Crash},
		{"ledger:p5_dispatch_possible:before_commit", fault.Crash},
		{"ledger:p3_attempt:after_commit", fault.Crash},
	}
	for _, p := range points {
		name := p.point
		if p.mode == fault.Lose {
			name += "/lose"
		}
		t.Run(name, func(t *testing.T) {
			h := setup(t)
			h.GrantStanding("mockapi.put")
			h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
			fault.Inject(p.point, p.mode, 1)
			runUntilSettled(t, h)
			if fault.Count(p.point) == 0 {
				t.Fatalf("point %s never reached", p.point)
			}
			if h.API.Received() != 1 || h.API.Applied() != 1 {
				t.Fatalf("target received %d / applied %d, want 1/1", h.API.Received(), h.API.Applied())
			}
			op := ledgerOp(t, h)
			if op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED || op.GetLifecycle() != lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
				t.Fatalf("op = %v", op)
			}
			if n := h.Count(host.DomainAdjudication, `SELECT sends_used FROM reservations`); n != 1 {
				t.Fatalf("send quota used %d times, want 1", n)
			}
			if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM attempts`); n != 1 {
				t.Fatalf("attempts = %d; recovery must reuse the original attempt", n)
			}
		})
	}
}

// P5 之后崩溃（I/O 之前或之后）：可能已发出，恢复后保持未知，不得当作未执行，
// 不可查询也不幂等的目标不会被盲目重发。
//
// 规则：G1、G11
func TestCrashAfterDispatchPossibleKeepsUnknown(t *testing.T) {
	for _, point := range []string{"ledger:p5_dispatch_possible:after_commit", executeIO + ":before_io", executeIO + ":after_io"} {
		t.Run(point, func(t *testing.T) {
			fault.Reset()
			t.Cleanup(fault.Reset)
			h := harness.NewWithAPI(t, mockapi.Unqueryable)
			h.GrantStanding("mockapi.put")
			h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
			fault.Inject(point, fault.Crash, 1)
			runUntilSettled(t, h)
			if h.API.Received() > 1 {
				t.Fatalf("target received %d requests; possibly-dispatched sends must not be blindly resent", h.API.Received())
			}
			op := ledgerOp(t, h)
			if op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN || op.GetLateEffect() != lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR {
				t.Fatalf("after DISPATCH_POSSIBLE the effect must stay UNKNOWN/MAY_OCCUR, got %v", op)
			}
			if op.GetExecutorEndpointId() != host.EndpointLocal {
				t.Fatalf("takeover must not reassign the endpoint")
			}
		})
	}
}
