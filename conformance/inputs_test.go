package conformance_test

import (
	"context"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/conformance/scripted"
	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

func modify(tv *lernav1.TaskView, sessionID string, draft *lernav1.RequirementSetDraft, keep bool) *lernav1.SubmitInputCommand {
	return &lernav1.SubmitInputCommand{
		SessionId: sessionID, InputKind: lernav1.InputKind_INPUT_KIND_MODIFY_TASK, TaskId: tv.GetTask().GetTaskId(),
		Text: "改一下", Requirements: draft, KeepRequirements: keep,
		ExpectedRequirementsVersion: tv.GetTask().GetRequirementsVersion(), ExpectedInputVersion: tv.GetTask().GetInputVersion(),
	}
}

// 修改被接纳后，基于旧上下文的提议无法准入；等待中的旧确认被替代，不能再被消费。
//
// 规则：G4、准入-2、C1
func TestModificationInvalidatesProposalsOnTheOldContext(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	old := pendingConfirmation(t, h, res.GetSessionId())
	tv := taskOf(t, h, res)
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, modify(tv, res.GetSessionId(), apiPutDraft("k1", "v2"), false), nil)
	h.MustRun()
	after := taskOf(t, h, res)
	if after.GetTask().GetInputVersion() != tv.GetTask().GetInputVersion()+1 ||
		after.GetTask().GetControlGeneration() <= tv.GetTask().GetControlGeneration() ||
		after.GetTask().GetRequirementsVersion() != 2 {
		t.Fatalf("modification must bump input version and control generation and accept the new requirements: %v", after.GetTask())
	}
	rec, err := h.Submit(harness.NewID(), ports.CommandSubmitInput, confirmCmd(old, true))
	if err != nil || rec.GetRejection().GetCode() != lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID {
		t.Fatalf("the confirmation for the old proposal must be superseded: %v %v", rec, err)
	}
	fresh := pendingConfirmation(t, h, res.GetSessionId())
	if fresh.GetConfirmationId() == old.GetConfirmationId() || fresh.GetInputVersion() != after.GetTask().GetInputVersion() {
		t.Fatalf("a new proposal on the new context needs its own confirmation: %v", fresh)
	}
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, confirmCmd(fresh, true), nil)
	h.MustRun()
	if v, _ := h.API.Value("k1"); v != "v2" || h.API.Received() != 1 {
		t.Fatalf("only the action on the new context may run: value %q, calls %d", v, h.API.Received())
	}
}

// 已准入、尚未开始的旧动作在修改后过不了开始门禁，被封闭并以内部封闭证明收尾。
//
// 规则：开始-2、G4、G11
func TestModificationBlocksAdmittedButUnstartedAction(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	for i := 0; i < 50 && h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`) == 0; i++ {
		if _, err := h.Host.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	tv := taskOf(t, h, res)
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, modify(tv, res.GetSessionId(), apiPutDraft("k1", "v2"), false), nil)
	h.MustRun()
	if v, _ := h.API.Value("k1"); v != "v2" || h.API.Received() != 1 {
		t.Fatalf("the old action must never start; only the new one runs: value %q, calls %d", v, h.API.Received())
	}
	final := taskOf(t, h, res)
	if final.GetPhase() != "SUCCEEDED" || len(final.GetOperations()) != 2 {
		t.Fatalf("task must succeed with the old operation closed: %s %v", final.GetPhase(), final.GetOperations())
	}
	oldOp := final.GetOperations()[0]
	if !oldOp.GetSettled() || oldOp.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED || oldOp.GetStarted() {
		t.Fatalf("the unstarted old operation settles NOT_APPLIED by internal closure: %v", oldOp)
	}
}

// 规则：G3、准入-2
func TestModificationAgainstStaleVersionsIsRejected(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	cmd := modify(taskOf(t, h, res), res.GetSessionId(), nil, true)
	cmd.ExpectedInputVersion = 0
	rec, err := h.Submit(harness.NewID(), ports.CommandSubmitInput, cmd)
	if err != nil || rec.GetRejection().GetCode() != lernav1.ErrorCode_ERROR_CODE_STALE_INPUT {
		t.Fatalf("a modification against old versions must be rejected: %v %v", rec, err)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs WHERE input_kind = ?`, int32(lernav1.InputKind_INPUT_KIND_MODIFY_TASK)); n != 0 {
		t.Fatal("a rejected modification must not be recorded")
	}
}

// 模板外的目标：用户用显式条件列表回答澄清请求后，条件集被接纳，任务继续推进。
//
// 规则：G2、C1
func TestAnswerWithExplicitRequirementsResumesTheTask(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "随便做点什么", nil)
	h.MustRun()
	sv := h.Session(res.GetSessionId())
	if len(sv.GetOpenRequests()) != 1 {
		t.Fatalf("want a clarification request")
	}
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		SessionId: res.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_ANSWER, RequestId: sv.GetOpenRequests()[0].GetRequestId(),
		Text: "完成条件见列表",
		Requirements: &lernav1.RequirementSetDraft{Explicit: []*lernav1.Requirement{{
			RequirementId: "u1", Description: "k9 设为 v9", Necessary: true,
			Rule: &lernav1.VerificationRule{Kind: lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED,
				CapabilityId: "mockapi.put", Params: map[string]string{"key": "k9", "value": "v9"}},
		}}},
	}, nil)
	h.MustRun()
	tv := taskOf(t, h, res)
	if tv.GetPhase() != "SUCCEEDED" || tv.GetRequirements().GetAcceptanceSource() != lernav1.AcceptanceSource_ACCEPTANCE_SOURCE_USER_LIST {
		t.Fatalf("the user's explicit list must be accepted and drive the task: %s %v", tv.GetPhase(), tv.GetRequirements())
	}
	if len(h.Session(res.GetSessionId()).GetOpenRequests()) != 0 {
		t.Fatal("the answered request must be closed")
	}
}

// 会改变依据的修改尚未处理时，普通目标准入被拒绝，任务等待用户说明。
//
// 规则：准入-3、G2
func TestUnprocessedModificationBlocksTargetAdmission(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = scripted.ActOnly
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k1", "v1"))
	h.MustRun()
	before := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`)
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, modify(taskOf(t, h, res), res.GetSessionId(), nil, false), nil)
	h.MustRun()
	tv := taskOf(t, h, res)
	if tv.GetRequirements().GetBoundInputVersion() == tv.GetTask().GetInputVersion() {
		t.Fatal("an unprocessed basis-changing input must leave the requirement set unbound")
	}
	if !waitingOn(tv, lernav1.WaitingKind_WAITING_KIND_USER) {
		t.Fatalf("task must ask the user: %v", tv.GetTask().GetWaitingOn())
	}
	if after := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM admissions`); after != before {
		t.Fatal("no new target admission while the input is unprocessed")
	}
}

// 会话事务序：并发提交的输入分配连续的序号，不丢输入、不误投。
//
// 规则：G3、G12
func TestConcurrentInputsGetContiguousSequence(t *testing.T) {
	h := harness.New(t)
	sres := &lernav1.CreateSessionResult{}
	h.MustAccept(harness.NewID(), ports.CommandCreateSession, &lernav1.CreateSessionCommand{}, sres)
	const n = 20
	var wg sync.WaitGroup
	errsCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env, err := durable.NewEnvelope(harness.Identity(harness.NewID()), ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
				SessionId: sres.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_RECORD_ONLY, Text: "note",
			})
			if err == nil {
				_, err = h.Client().Submit(context.Background(), env)
			}
			errsCh <- err
		}()
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	sv := h.Session(sres.GetSessionId())
	if len(sv.GetInputs()) != n {
		t.Fatalf("inputs = %d, want %d", len(sv.GetInputs()), n)
	}
	for i, in := range sv.GetInputs() {
		if in.GetSessionSeq() != int64(i+1) || len(in.GetTaskId()) != 0 {
			t.Fatalf("input %d has seq %d / task %q", i, in.GetSessionSeq(), in.GetTaskId())
		}
	}
}

// 显式依赖未满足时不写决定；前提到达后原命令可以被接纳。
//
// 规则：G3
func TestDependsOnUnrecordedInputWaits(t *testing.T) {
	h := harness.New(t)
	sres := &lernav1.CreateSessionResult{}
	h.MustAccept(harness.NewID(), ports.CommandCreateSession, &lernav1.CreateSessionCommand{}, sres)
	id := harness.NewID()
	cmd := &lernav1.SubmitInputCommand{SessionId: sres.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_RECORD_ONLY, Text: "b", DependsOn: []string{"missing"}}
	if _, err := h.Submit(id, ports.CommandSubmitInput, cmd); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE) {
		t.Fatalf("want DEPENDENCY_UNAVAILABLE, got %v", err)
	}
	if q := h.Query(id); q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND {
		t.Fatal("an unmet dependency is not a decision")
	}
}

// 一个会话可以先后关联多个任务；任务关闭后会话仍然保留，可以发起新任务。
//
// 规则：C1
func TestSessionOutlivesItsTasks(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	first := h.SubmitGoal(harness.NewID(), "第一个任务", apiPutDraft("a", "1"))
	h.MustRun()
	res := &lernav1.SubmitInputResult{}
	h.MustAccept(harness.NewID(), ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		SessionId: first.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "第二个任务",
		Requirements: apiPutDraft("b", "2"), TaskBudget: harness.TaskBudget,
	}, res)
	h.MustRun()
	sv := h.Session(first.GetSessionId())
	if len(sv.GetTaskIds()) != 2 || sv.GetSession().GetStatus() != lernav1.SessionStatus_SESSION_STATUS_ACTIVE {
		t.Fatalf("session must stay active with both tasks: %v", sv)
	}
	for _, id := range sv.GetTaskIds() {
		if h.Task(id).GetPhase() != "SUCCEEDED" {
			t.Fatalf("task %s did not succeed", id)
		}
	}
}
