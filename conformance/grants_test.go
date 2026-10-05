package conformance_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/adapters/mockapi"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/conformance/scripted"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

func pendingConfirmation(t *testing.T, h *harness.Harness, sessionID string) *lernav1.Confirmation {
	t.Helper()
	sv := h.Session(sessionID)
	if len(sv.GetPendingConfirmations()) != 1 {
		t.Fatalf("want one pending confirmation, got %v", sv.GetPendingConfirmations())
	}
	return sv.GetPendingConfirmations()[0]
}

func confirmCmd(c *lernav1.Confirmation, approve bool) *lernav1.SubmitInputCommand {
	return &lernav1.SubmitInputCommand{
		SessionId: c.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_CONFIRMATION,
		RequestId: c.GetConfirmationId(), Approve: approve, IntentFingerprint: c.GetIntentFingerprint(),
	}
}

// 用户确认的是核心生成的动作描述；确认后单次授权与动作在准入事务中原子绑定。
//
// 规则：G4、准入-9、准入-7
func TestConfirmationAdmitsExactlyTheDescribedAction(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	c := pendingConfirmation(t, h, res.GetSessionId())
	if c.GetSubjectKind() != lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION ||
		c.GetDescription() == "" || len(c.GetIntentFingerprint()) == 0 {
		t.Fatalf("confirmation must carry a core-generated description and binding: %v", c)
	}
	if h.API.Received() != 0 {
		t.Fatal("nothing may be sent before the user confirms")
	}
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, confirmCmd(c, true), nil)
	h.MustRun()
	if tv := taskOf(t, h, res); tv.GetPhase() != "SUCCEEDED" || h.API.Received() != 1 {
		t.Fatalf("confirmed action must run once and the task succeed: %s / %d", tv.GetPhase(), h.API.Received())
	}
	recs := admissionRecords(t, h)
	if len(recs) != 1 || recs[0].GetConfirmationRef() != c.GetConfirmationId() {
		t.Fatalf("admission must reference the consumed confirmation: %v", recs)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM confirmations WHERE status = ? AND consumed_kind = 'admission'`,
		int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_CONSUMED)); n != 1 {
		t.Fatalf("the confirmation must be consumed exactly once, got %d", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM grants`); n != 1 {
		t.Fatalf("one single-use grant bound to the action, got %d", n)
	}
}

// 用户回应的摘要与核心绑定不一致（例如看到的是旧参数）时，回应被拒绝，不能套用到不同的动作上。
//
// 规则：准入-9、G4
func TestConfirmationForADifferentItemIsInvalid(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	c := pendingConfirmation(t, h, res.GetSessionId())
	wrong := confirmCmd(c, true)
	wrong.IntentFingerprint = []byte("what the user saw for other parameters")
	rec, err := h.Submit(harness.NewID(), ports.CommandSubmitInput, wrong)
	if err != nil || rec.GetRejection().GetCode() != lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID {
		t.Fatalf("mismatched confirmation must be rejected: %v %v", rec, err)
	}
	h.MustRun()
	if h.API.Received() != 0 || h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`) != 0 {
		t.Fatal("a mismatched confirmation must not admit anything")
	}
}

// 撤销被接纳后，依赖它的新准入立即被拒绝；撤销进度可查询，执行端点确认封闭后才完成。
//
// 规则：G8、准入-7、开始-3
func TestRevocationStopsNewAdmissionsAndReportsProgress(t *testing.T) {
	h := harness.NewWithAPI(t, mockapi.Idempotent)
	h.Reasoner.Policy = scripted.ActOnly
	grant := h.GrantStanding("mockapi.put")
	res := &lernav1.RevocationProgress{}
	h.MustAccept(harness.NewID(), ports.CommandRevokeGrant, &lernav1.RevokeGrantCommand{GrantId: grant}, res)
	if res.GetStatus() != lernav1.GrantStatus_GRANT_STATUS_REVOKED {
		t.Fatalf("revocation must be accepted: %v", res)
	}
	h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM command_receipts WHERE command_kind = ? AND decision = ?`,
		ports.CommandAdmit, int32(lernav1.Decision_DECISION_REJECTED)); n == 0 {
		t.Fatal("admission after revocation must be rejected")
	}
	if h.API.Received() != 0 {
		t.Fatal("nothing may be sent under a revoked grant")
	}
	v, err := h.Client().Grant(context.Background(), harness.User, grant)
	if err != nil {
		t.Fatal(err)
	}
	if v.GetRevocation().GetCompletion() != lernav1.RevocationCompletion_REVOCATION_COMPLETION_COMPLETE {
		t.Fatalf("with no open egress the revocation is complete: %v", v.GetRevocation())
	}
}

// 授权在出口处再次核验当前有效性：准入之后、开始之前撤销，开始门禁拒绝；撤销等执行端点封闭后完成。
//
// 规则：G8、开始-3、G4
func TestRevocationBetweenAdmissionAndStartBlocksTheSend(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = scripted.ActOnly
	grant := h.GrantStanding("mockapi.put")
	h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	// 推进到准入完成，但不让执行管理开始发送。
	for i := 0; i < 50 && h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`) == 0; i++ {
		if _, err := h.Host.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`) != 1 {
		t.Fatal("expected an admission")
	}
	prog := &lernav1.RevocationProgress{}
	h.MustAccept(harness.NewID(), ports.CommandRevokeGrant, &lernav1.RevokeGrantCommand{GrantId: grant}, prog)
	if prog.GetCompletion() != lernav1.RevocationCompletion_REVOCATION_COMPLETION_PENDING || len(prog.GetOpenOperations()) != 1 {
		t.Fatalf("an admitted, unstarted operation keeps the revocation pending: %v", prog)
	}
	h.MustRun()
	if h.API.Received() != 0 {
		t.Fatalf("the start gate must reject sends under a revoked grant; target got %d", h.API.Received())
	}
	v, _ := h.Client().Grant(context.Background(), harness.User, grant)
	if v.GetRevocation().GetCompletion() != lernav1.RevocationCompletion_REVOCATION_COMPLETION_COMPLETE {
		t.Fatalf("revocation completes once the endpoint confirmed the seal: %v", v.GetRevocation())
	}
}

// 读取等其他操作权利不能代替行动：只有 READ 的授权不覆盖调用。
//
// 规则：G8
func TestReadRightDoesNotImplyAct(t *testing.T) {
	h := harness.New(t)
	h.MustAccept(harness.NewID(), ports.CommandIssueGrant, &lernav1.IssueGrantCommand{
		Clauses: []*lernav1.GrantClause{{Resource: "mockapi.put", Actions: []string{"invoke"},
			UseRights:          []lernav1.UseRight{lernav1.UseRight_USE_RIGHT_READ, lernav1.UseRight_USE_RIGHT_SAVE, lernav1.UseRight_USE_RIGHT_SYNC},
			ProcessingPurposes: []lernav1.ProcessingPurpose{lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK}}},
		UseMode: lernav1.UseMode_USE_MODE_STANDING, ValidUntil: timestamppb.New(h.Clock.Now().Add(time.Hour)),
	}, nil)
	res := h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	if h.API.Received() != 0 || !waitingOn(taskOf(t, h, res), lernav1.WaitingKind_WAITING_KIND_USER) {
		t.Fatal("a read/save/sync grant must not authorize an action")
	}
}

// 授权确认（GRANT_ISSUANCE）只能在签发事务中消费一次；类型不一致的消费被拒绝。
//
// 规则：G7、准入-9
func TestGrantIssuanceConfirmationIsConsumedOnce(t *testing.T) {
	h := harness.New(t)
	sres := &lernav1.CreateSessionResult{}
	h.MustAccept(harness.NewID(), ports.CommandCreateSession, &lernav1.CreateSessionCommand{}, sres)
	grant := &lernav1.IssueGrantCommand{
		Clauses: []*lernav1.GrantClause{{Resource: "mockapi.put", Actions: []string{"invoke"},
			UseRights:          []lernav1.UseRight{lernav1.UseRight_USE_RIGHT_ACT},
			ProcessingPurposes: []lernav1.ProcessingPurpose{lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK}}},
		UseMode: lernav1.UseMode_USE_MODE_STANDING, ValidUntil: timestamppb.New(h.Clock.Now().Add(time.Hour)),
	}
	req := &lernav1.RequestGrantResult{}
	h.MustAccept(harness.NewID(), ports.CommandRequestGrant, &lernav1.RequestGrantCommand{SessionId: sres.GetSessionId(), Grant: grant}, req)
	c := pendingConfirmation(t, h, sres.GetSessionId())
	if c.GetDescription() != req.GetDescription() || c.GetSubjectKind() != lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_GRANT_ISSUANCE {
		t.Fatalf("grant confirmation must show the core-generated description: %v", c)
	}
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, confirmCmd(c, true), nil)
	grant.ConfirmationId = c.GetConfirmationId()
	h.MustAccept(harness.NewID(), ports.CommandIssueGrant, grant, nil)
	rec, err := h.Submit(harness.NewID(), ports.CommandIssueGrant, grant)
	if err != nil || rec.GetRejection().GetCode() != lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID {
		t.Fatalf("a consumed confirmation cannot issue a second grant: %v %v", rec, err)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM grants`); n != 1 {
		t.Fatalf("grants = %d, want 1", n)
	}
}
