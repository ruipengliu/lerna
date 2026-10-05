package conformance_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/adapters/mockapi"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/conformance/scripted"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
)

func operation(t *testing.T, h *harness.Harness) *lernav1.Operation {
	t.Helper()
	recs := admissionRecords(t, h)
	if len(recs) == 0 {
		t.Fatal("no admitted operation")
	}
	r, err := h.Host.LedgerModule.Record(context.Background(), harness.User, recs[0].GetOperationId())
	if err != nil {
		t.Fatal(err)
	}
	return r.GetOperation()
}

// 正常路径：出口闸门在 P5 持久写下"可能已发出"之后发送一次；目标独立记录真实收到的次数为 1。
//
// 规则：G5、G4
func TestNormalPathSendsExactlyOnce(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	if h.API.Received() != 1 || h.API.Applied() != 1 {
		t.Fatalf("target received %d, applied %d; want 1/1", h.API.Received(), h.API.Applied())
	}
	if v, _ := h.API.Value("k1"); v != "v1" {
		t.Fatalf("target value = %q", v)
	}
	op := operation(t, h)
	if op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED || op.GetLateEffect() != lernav1.LateEffect_LATE_EFFECT_RULED_OUT ||
		op.GetLifecycle() != lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
		t.Fatalf("ledger must decide APPLIED/RULED_OUT and settle: %v", op)
	}
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM sends WHERE dispatch_possible = 1 AND start_receipt IS NOT NULL`); n != 1 {
		t.Fatalf("each physical send needs a start receipt and a prior DISPATCH_POSSIBLE record, got %d", n)
	}
	if n := h.Count(host.DomainLedger, `SELECT COUNT(*) FROM observations`); n != 1 {
		t.Fatalf("observations = %d", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT sends_used FROM reservations`); n != 1 {
		t.Fatalf("the start gate must occupy exactly one send, got %d", n)
	}
	tv := taskOf(t, h, res)
	if len(tv.GetOperations()) != 1 || !tv.GetOperations()[0].GetSettled() ||
		tv.GetOperations()[0].GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED {
		t.Fatalf("task orchestration must learn the effect: %v", tv.GetOperations())
	}
}

// 适配器只回报，效果由执行管理按受信规则写入："已接受"不等于已生效。
//
// 规则：R3、G2
func TestAcceptedResponseIsNotTreatedAsApplied(t *testing.T) {
	h := harness.NewWithAPI(t, mockapi.Unqueryable)
	h.API.EffectDelay = 0
	h.API.InjectOnCall(1, mockapi.DelayedEffect)
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	op := operation(t, h)
	if op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN || op.GetLifecycle() == lernav1.OperationLifecycle_OPERATION_LIFECYCLE_SETTLED {
		t.Fatalf("a 202 acknowledgement only proves acceptance; effect must stay UNKNOWN: %v", op)
	}
}

// 禁用隐式重试与回退：回执丢失后，出口闸门和适配器内部都不重发。
//
// 规则：G5、G1
func TestNoImplicitRetryAfterLostResponse(t *testing.T) {
	h := harness.NewWithAPI(t, mockapi.Unqueryable)
	h.API.InjectOnCall(1, mockapi.LoseAfterCommit)
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	h.Clock.Advance(24 * 3600e9)
	h.MustRun()
	if h.API.Received() != 1 {
		t.Fatalf("target received %d requests; a lost response must not be retried implicitly", h.API.Received())
	}
	op := operation(t, h)
	if op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN || op.GetLateEffect() != lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR {
		t.Fatalf("an unqueryable, non-idempotent timeout stays UNKNOWN: %v", op)
	}
}

func startCmd(h *harness.Harness, t *testing.T, mutate func(*lernav1.StartSendCommand)) (*lernav1.Receipt, error) {
	t.Helper()
	recs := admissionRecords(t, h)
	adm := recs[0]
	cmd := &lernav1.StartSendCommand{
		UserId: harness.User, TaskId: adm.GetTaskId(), OperationId: adm.GetOperationId(), AttemptId: "forged-attempt", SendSeq: 1,
		Purpose: lernav1.SendPurpose_SEND_PURPOSE_EXECUTE, ExecutorEndpointId: adm.GetExecutorEndpointId(),
		CapabilityId: adm.GetCapabilityId(), SafeResend: true,
	}
	var cred string
	_ = h.Host.Adjudication.Read(context.Background(), func(tx *durable.Tx) error {
		return tx.QueryRow(`SELECT credential_id FROM credentials WHERE operation_id = ?`, adm.GetOperationId()).Scan(&cred)
	})
	cmd.CredentialRef = cred
	cmd.RequestDigest = h.Host.Tasks.Digest(adm)
	mutate(cmd)
	id := egress.StartID(harness.User, host.DomainAdjudication, host.DomainLedger, cmd.GetAttemptId(), cmd.GetSendSeq(), cmd.GetPurpose())
	env, err := durable.NewEnvelope(id, ports.CommandStartSend, cmd)
	if err != nil {
		t.Fatal(err)
	}
	return h.Host.Adjudication.Execute(context.Background(), env)
}

func rejectedWith(t *testing.T, rec *lernav1.Receipt, err error, code lernav1.ErrorCode) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if rec.GetDecision() != lernav1.Decision_DECISION_REJECTED || rec.GetRejection().GetCode() != code {
		t.Fatalf("want rejection %s, got %v", code, rec)
	}
}

// 规则：开始-1
func TestStartGateRejectsMismatchedDescription(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = scripted.ActOnly
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	rec, err := startCmd(h, t, func(c *lernav1.StartSendCommand) { c.RequestDigest = []byte("tampered") })
	rejectedWith(t, rec, err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
	rec, err = startCmd(h, t, func(c *lernav1.StartSendCommand) { c.AttemptId = "a2"; c.ExecutorEndpointId = "phone" })
	rejectedWith(t, rec, err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
}

// 规则：开始-3
func TestStartGateChecksTheCredential(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = scripted.ActOnly
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	rec, err := startCmd(h, t, func(c *lernav1.StartSendCommand) { c.CredentialRef = "not-a-credential" })
	rejectedWith(t, rec, err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
}

// 每个新的发送身份占用一次发送额度；用完之后开始门禁拒绝。
//
// 规则：开始-5、G10
func TestStartGateOccupiesSendQuota(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = scripted.ActOnly
	h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	// 幂等能力准入时分配 2 次发送额度，正常路径已用 1 次。
	rec, err := startCmd(h, t, func(c *lernav1.StartSendCommand) { c.AttemptId = "x1" })
	if err != nil || rec.GetDecision() != lernav1.Decision_DECISION_ACCEPTED {
		t.Fatalf("second send within quota: %v %v", rec, err)
	}
	again, err := startCmd(h, t, func(c *lernav1.StartSendCommand) { c.AttemptId = "x1" })
	if err != nil || again.GetCommitPosition() != rec.GetCommitPosition() {
		t.Fatalf("the same send identity must not occupy the quota twice: %v", err)
	}
	rec, err = startCmd(h, t, func(c *lernav1.StartSendCommand) { c.AttemptId = "x2" })
	rejectedWith(t, rec, err, lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED)
}

// 规则：G5
func TestMockAPIDeclaresCapabilitiesPerClass(t *testing.T) {
	for class, want := range map[mockapi.Class][2]bool{
		mockapi.Idempotent:  {true, false},
		mockapi.Queryable:   {false, true},
		mockapi.Unqueryable: {false, false},
	} {
		d := mockapi.New("m", mockapi.NewTarget(class, harness.NewClock().Now)).Declarations()[0]
		if d.GetIdempotency().GetSupported() != want[0] || d.GetQuery().GetSupported() != want[1] {
			t.Fatalf("%s: idempotent=%v query=%v", class, d.GetIdempotency().GetSupported(), d.GetQuery().GetSupported())
		}
		if d.GetExternalEffect() != lernav1.ExternalEffect_EXTERNAL_EFFECT_CHANGES_WORLD || !d.GetBilling().GetCeilingKnown() {
			t.Fatalf("%s: must declare its external effect and cost ceiling", class)
		}
	}
}
