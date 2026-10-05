//go:build fault

package fault_test

import (
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

func goalCmd() *lernav1.SubmitInputCommand {
	return &lernav1.SubmitInputCommand{
		InputKind: lernav1.InputKind_INPUT_KIND_NEW_GOAL,
		Text:      "把 k1 设为 v1",
		Requirements: &lernav1.RequirementSetDraft{
			TemplateId:     "api_put",
			TemplateParams: map[string]string{"capability": "mockapi.put", "key": "k1", "value": "v1"},
		},
	}
}

func setup(t *testing.T) *harness.Harness {
	t.Helper()
	fault.Reset()
	t.Cleanup(fault.Reset)
	return harness.New(t)
}

// submitUntilDecided 模拟命令行：结果未知或进程崩溃后用原命令标识、原内容重试，直到拿到决定。
func submitUntilDecided(t *testing.T, h *harness.Harness, id string, cmd *lernav1.SubmitInputCommand) *lernav1.Receipt {
	t.Helper()
	for i := 0; i < 5; i++ {
		rec, err := h.Submit(id, ports.CommandSubmitInput, cmd)
		switch {
		case err == nil:
			return rec
		case errors.Is(err, harness.ErrCrashed):
			h.Restart()
		case errs.IsIndeterminate(err):
			q := h.Query(id)
			if q.GetOutcome() == lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED {
				return q.GetReceipt()
			}
		default:
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatal("no decision after retries")
	return nil
}

// runUntilSettled 推进工作；崩溃后重启并继续。
func runUntilSettled(t *testing.T, h *harness.Harness) {
	t.Helper()
	for i := 0; i < 10; i++ {
		err := h.Run()
		if err == nil {
			return
		}
		if !errors.Is(err, harness.ErrCrashed) {
			t.Fatalf("run: %v", err)
		}
		h.Restart()
	}
	t.Fatal("did not settle")
}

func assertOneTask(t *testing.T, h *harness.Harness) {
	t.Helper()
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs`); n != 1 {
		t.Fatalf("inputs = %d, want 1", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks`); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM session_inputs WHERE routing_status = ?`,
		int32(lernav1.RoutingStatus_ROUTING_STATUS_TASK_ACCEPTED)); n != 1 {
		t.Fatalf("the input must end up accepted by the task")
	}
}

// 交互适配器 M1 演示 1：输入已持久保存、任务尚未建立时崩溃，恢复后只创建一个任务。
//
// 规则：G3
func TestCrashAfterInputBeforeTaskCreatesOneTask(t *testing.T) {
	for _, point := range []string{"tasks:create_task:before_commit", "tasks:create_task:after_commit"} {
		t.Run(point, func(t *testing.T) {
			h := setup(t)
			h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
			fault.Inject(point, fault.Crash, 1)
			if err := h.Run(); !errors.Is(err, harness.ErrCrashed) {
				t.Fatalf("expected injected crash at %s, got %v", point, err)
			}
			h.Restart()
			h.MustRun()
			assertOneTask(t, h)
		})
	}
}

// 规则：G3
func TestReceiptLostOnSubmitIsRecoveredByOriginalIdentity(t *testing.T) {
	h := setup(t)
	id := harness.NewID()
	fault.Inject("deliver:"+ports.CommandSubmitInput+":receipt", fault.Lose, 1)
	_, err := h.Submit(id, ports.CommandSubmitInput, goalCmd())
	if !errs.IsIndeterminate(err) {
		t.Fatalf("lost receipt must surface as indeterminate, got %v", err)
	}
	q := h.Query(id)
	if q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED {
		t.Fatalf("the original decision must be queryable, got %s", q.GetOutcome())
	}
	runUntilSettled(t, h)
	assertOneTask(t, h)
}

// 规则：G3
func TestCrashBeforeCommitLeavesNothingAndRetryCreatesOne(t *testing.T) {
	h := setup(t)
	id := harness.NewID()
	fault.Inject("cmd:"+ports.CommandSubmitInput+":before_commit", fault.Crash, 1)
	if _, err := h.Submit(id, ports.CommandSubmitInput, goalCmd()); !errors.Is(err, harness.ErrCrashed) {
		t.Fatalf("expected crash, got %v", err)
	}
	h.Restart()
	if q := h.Query(id); q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND {
		t.Fatalf("an uncommitted command must not be found, got %s", q.GetOutcome())
	}
	submitUntilDecided(t, h, id, goalCmd())
	runUntilSettled(t, h)
	assertOneTask(t, h)
}

// 在提交目标这条路径经过的每个持久化点前后注入崩溃和回执丢失：
// 命令行按原标识重试，恢复后只有一条输入、一个任务。
//
// 规则：G3、G11
func TestEveryPersistencePointOnSubmitPath(t *testing.T) {
	probe := setup(t)
	probeID := harness.NewID()
	submitUntilDecided(t, probe, probeID, goalCmd())
	probe.MustRun()
	points := fault.Seen()
	if len(points) < 4 {
		t.Fatalf("too few persistence points observed: %v", points)
	}
	for _, point := range points {
		for _, mode := range []fault.Mode{fault.Crash, fault.Lose} {
			name := point
			if mode == fault.Lose {
				name += "/lose"
			}
			t.Run(name, func(t *testing.T) {
				h := setup(t)
				fault.Inject(point, mode, 1)
				id := harness.NewID()
				submitUntilDecided(t, h, id, goalCmd())
				runUntilSettled(t, h)
				// 再重试一次原命令：仍是原决定，不产生第二个任务。
				submitUntilDecided(t, h, id, goalCmd())
				runUntilSettled(t, h)
				assertOneTask(t, h)
			})
		}
	}
}
