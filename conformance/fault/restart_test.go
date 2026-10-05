//go:build fault

package fault_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

func goal() (*v1.Caller, *v1.SubmitGoalCommand) {
	return &v1.Caller{UserId: "alice", IssuerId: "cli"}, &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "original"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "saved input"}
}

// 规则：G3、G11
func TestAbruptCrashAtEverySubmissionCommit(t *testing.T) {
	for _, point := range []string{"content.stage", "durable.submit", "durable.decide"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "crash.db")
				child := exec.Command(os.Args[0], "-test.run=^TestCrashChild$")
				child.Env = append(os.Environ(), "LERNA_CRASH_DB="+path, "LERNA_CRASH_POINT="+point, "LERNA_CRASH_MODE="+string(mode))
				out, err := child.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("fault did not abruptly terminate child: %v %s", err, out)
				}
				h, err := assembly.Open(path, "alice", "local")
				if err != nil {
					t.Fatal(err)
				}
				defer h.Close()
				caller, c := goal()
				q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
				if err != nil {
					t.Fatal(err)
				}
				committed := point == "durable.decide" || (point == "durable.submit" && mode == sqlite.CrashAfterCommit)
				if committed && q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("saved responsibility not automatically recovered: %v", q)
				}
				if !committed && q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
					t.Fatalf("uncommitted reception became visible: %v", q)
				}
				// 未确认受理时，用原命令身份重投；已确认时也必须拿回同一决定。
				if _, err := h.Sessions.SubmitGoal(context.Background(), caller, c); err != nil {
					t.Fatal(err)
				}
				if err := h.Sessions.ProcessPending(context.Background(), caller); err != nil {
					t.Fatal(err)
				}
				assertOneTask(t, h, caller, c)
			})
		}
	}
}

// 规则：G3
func TestCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_CRASH_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	// 不关闭存储：故障动作必须直接退出进程，不能执行任何 defer。
	ctx, err := sqlite.WithFault(context.Background(), os.Getenv("LERNA_CRASH_POINT"), sqlite.FaultMode(os.Getenv("LERNA_CRASH_MODE")))
	if err != nil {
		t.Fatal(err)
	}
	caller, c := goal()
	if _, err := h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED || q.Receipt.TaskRef != nil {
		t.Fatalf("expected saved input before task: %v %v", q, err)
	}
	claim, err := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "crash-claim"}, ContractVersion: 1, Action: "CLAIM", ProcessInstance: "crash-child", AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: 100})
	if err != nil || len(claim.Jobs) != 1 {
		t.Fatalf("claim %v %v", claim, err)
	}
	if err := h.Sessions.ProcessClaim(ctx, claim.Jobs[0]); err != nil {
		t.Fatal(err)
	}
	t.Fatal("configured crash was not reached")
}

func assertOneTask(t *testing.T, h *assembly.Harness, caller *v1.Caller, c *v1.SubmitGoalCommand) {
	t.Helper()
	ctx := context.Background()
	q, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || q.Receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("expected original accepted decision: %v %v", q, err)
	}
	session, err := h.Sessions.QuerySession(ctx, caller, q.Receipt.SessionRef.Name)
	if err != nil || len(session.TaskRefs) != 1 || len(session.Inputs) != 1 || session.LastCommittedSeq != 1 {
		t.Fatalf("duplicate task/input: %v %v", session, err)
	}
	task, err := h.Tasks.QueryTask(ctx, caller, q.Receipt.TaskRef.Name)
	if err != nil || task == nil {
		t.Fatalf("missing task: %v %v", task, err)
	}
	content, err := h.Content.Read(ctx, caller, task.GoalRef)
	if err != nil || content == nil || content.Text != "saved input" || content.Source.CommandId != c.Identity.CommandId {
		t.Fatalf("original input lost or reassigned: %v %v", content, err)
	}
	jobs, err := h.Durable.Pending(ctx, caller)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unfinished responsibility: %v %v", jobs, err)
	}
	retry, err := h.Sessions.SubmitGoal(ctx, caller, c)
	if err != nil || retry.CommitPosition != q.Receipt.CommitPosition || retry.TaskRef.Name.LocalId != q.Receipt.TaskRef.Name.LocalId {
		t.Fatalf("retry changed original decision: %v %v", retry, err)
	}
}
