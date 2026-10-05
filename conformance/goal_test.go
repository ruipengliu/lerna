package conformance_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

func apiPutDraft(key, value string) *lernav1.RequirementSetDraft {
	return &lernav1.RequirementSetDraft{
		TemplateId:     "api_put",
		TemplateParams: map[string]string{"capability": "mockapi.put", "key": key, "value": value},
	}
}

// 规则：G3、开始-1
func TestSubmitGoalCreatesSessionAndOneTask(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	if res.GetRoutingStatus() != lernav1.RoutingStatus_ROUTING_STATUS_RECORDED {
		t.Fatalf("receipt must say only RECORDED before the task accepts it, got %s", res.GetRoutingStatus())
	}
	h.MustRun()

	sv := h.Session(res.GetSessionId())
	if len(sv.GetInputs()) != 1 || sv.GetInputs()[0].GetRoutingStatus() != lernav1.RoutingStatus_ROUTING_STATUS_TASK_ACCEPTED {
		t.Fatalf("session input not accepted by a task: %v", sv.GetInputs())
	}
	if len(sv.GetTaskIds()) != 1 {
		t.Fatalf("want 1 task linked to session, got %v", sv.GetTaskIds())
	}
	tv := h.Task(sv.GetTaskIds()[0])
	if tv.GetTask().GetLifecycle() != lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN || tv.GetPhase() == "" {
		t.Fatalf("task not open: %v", tv)
	}
	if tv.GetRequirements().GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED ||
		tv.GetRequirements().GetAcceptanceSource() != lernav1.AcceptanceSource_ACCEPTANCE_SOURCE_TEMPLATE {
		t.Fatalf("template requirements not accepted: %v", tv.GetRequirements())
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks`); n != 1 {
		t.Fatalf("persisted tasks = %d, want 1", n)
	}
}

// 规则：G3
func TestDuplicateSubmitReturnsOriginalDecision(t *testing.T) {
	h := harness.New(t)
	id := harness.NewID()
	cmd := &lernav1.SubmitInputCommand{
		InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "goal", Requirements: apiPutDraft("k", "v"),
	}
	r1 := h.MustAccept(id, ports.CommandSubmitInput, cmd, nil)
	h.MustRun()
	r2 := h.MustAccept(id, ports.CommandSubmitInput, cmd, nil)
	h.MustRun()
	if r1.GetCommitPosition() != r2.GetCommitPosition() || string(r1.GetResult()) != string(r2.GetResult()) {
		t.Fatalf("retry must return the original decision:\n%v\n%v", r1, r2)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks`); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs`); n != 1 {
		t.Fatalf("inputs = %d, want 1", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM command_receipts WHERE command_id = ?`, id); n != 1 {
		t.Fatalf("receipts = %d, want 1", n)
	}
}

// 规则：G3
func TestSameIdentityDifferentContentIsConflict(t *testing.T) {
	h := harness.New(t)
	id := harness.NewID()
	h.MustAccept(id, ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "goal A", Requirements: apiPutDraft("k", "v"),
	}, nil)
	_, err := h.Submit(id, ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "goal B", Requirements: apiPutDraft("k", "v"),
	})
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT) {
		t.Fatalf("want IDEMPOTENCY_CONFLICT, got %v", err)
	}
	h.MustRun()
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs WHERE body = 'goal B'`); n != 0 {
		t.Fatalf("conflicting content must not be applied")
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks`); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
}

// 规则：G3
func TestQueryDistinguishesNotSeenFromDecided(t *testing.T) {
	h := harness.New(t)
	id := harness.NewID()
	if q := h.Query(id); q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND {
		t.Fatalf("before submit: %s", q.GetOutcome())
	}
	rec := h.MustAccept(id, ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "g", Requirements: apiPutDraft("k", "v"),
	}, nil)
	q := h.Query(id)
	if q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED ||
		q.GetReceipt().GetCommitPosition() != rec.GetCommitPosition() {
		t.Fatalf("after submit: %v", q)
	}
}

// 规则：G12
func TestWrongIdentityIsRejected(t *testing.T) {
	h := harness.New(t)
	cmd := &lernav1.SubmitInputCommand{InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "g", Requirements: apiPutDraft("k", "v")}
	for name, id := range map[string]*lernav1.CommandIdentity{
		"other user":    {UserId: "u-other", IssuerId: harness.Issuer, TargetDomainId: host.DomainAdjudication, CommandId: "c1"},
		"forged issuer": {UserId: harness.User, IssuerId: "someone-else", TargetDomainId: host.DomainAdjudication, CommandId: "c2"},
	} {
		env, err := durable.NewEnvelope(id, ports.CommandSubmitInput, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.Client().Submit(context.Background(), env); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) {
			t.Fatalf("%s: want PERMISSION_DENIED, got %v", name, err)
		}
	}
	if _, err := h.Client().Task(context.Background(), "u-other", "t"); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) {
		t.Fatalf("query for other user: %v", err)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM command_receipts WHERE command_kind = ?`, ports.CommandSubmitInput); n != 0 {
		t.Fatalf("rejected identities must leave no receipt, got %d", n)
	}
}

// 规则：G2
func TestGoalOutsideTemplatesWaitsForUser(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "随便做点什么", nil)
	h.MustRun()
	sv := h.Session(res.GetSessionId())
	tv := h.Task(sv.GetTaskIds()[0])
	if tv.GetRequirements().GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_DRAFT {
		t.Fatalf("requirements must stay DRAFT, got %s", tv.GetRequirements().GetStatus())
	}
	if tv.GetPhase() != "WAITING" || len(tv.GetTask().GetWaitingOn()) != 1 ||
		tv.GetTask().GetWaitingOn()[0].GetKind() != lernav1.WaitingKind_WAITING_KIND_USER {
		t.Fatalf("task must wait for user clarification: %v", tv.GetTask())
	}
	if len(sv.GetOpenRequests()) != 1 {
		t.Fatalf("an input request must be published: %v", sv.GetOpenRequests())
	}
}

// 规则：G3
func TestInvalidTemplateIsDurableRejection(t *testing.T) {
	h := harness.New(t)
	id := harness.NewID()
	cmd := &lernav1.SubmitInputCommand{InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL, Text: "g",
		Requirements: &lernav1.RequirementSetDraft{TemplateId: "no_such_template"}}
	rec, err := h.Submit(id, ports.CommandSubmitInput, cmd)
	if err != nil || rec.GetDecision() != lernav1.Decision_DECISION_REJECTED {
		t.Fatalf("want durable rejection, got %v %v", rec, err)
	}
	h.MustRun()
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs`); n != 0 {
		t.Fatalf("a rejection must not leave partial business writes")
	}
	rec2, _ := h.Submit(id, ports.CommandSubmitInput, cmd)
	if rec2.GetCommitPosition() != rec.GetCommitPosition() {
		t.Fatalf("rejection must be returned as the original decision")
	}
}

// 规则：G12
func TestBranchingIsUnsupported(t *testing.T) {
	h := harness.New(t)
	res := h.SubmitGoal(harness.NewID(), "g", apiPutDraft("k", "v"))
	rec, err := h.Submit(harness.NewID(), ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		SessionId: res.GetSessionId(), InputKind: lernav1.InputKind_INPUT_KIND_RECORD_ONLY, Text: "x", ForkFromMessageId: "m1",
	})
	if err != nil || errs.Code(errs.FromProto(rec.GetRejection())) != lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE {
		t.Fatalf("want UNSUPPORTED_FEATURE, got %v %v", rec, err)
	}
}
