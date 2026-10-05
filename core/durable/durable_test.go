package durable_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3、G12
func TestGoalCreatesPersistentSessionAndTask(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lerna.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	cmd := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "goal-1"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "Write a greeting"}
	receipt, err := h.Sessions.SubmitGoal(ctx, caller, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED {
		t.Fatalf("expected submitted, got %v", receipt)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || q.Receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("expected accepted decision: %v", q)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	task, err := h.Tasks.QueryTask(ctx, caller, q.Receipt.TaskRef.Name)
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || task.InputVersion != 1 || task.OwnerDomainId != "local" {
		t.Fatalf("invalid task: %v", task)
	}
	session, err := h.Sessions.QuerySession(ctx, caller, q.Receipt.SessionRef.Name)
	if err != nil {
		t.Fatal(err)
	}
	if session.LastCommittedSeq != 1 || len(session.Inputs) != 1 || len(session.TaskRefs) != 1 {
		t.Fatalf("invalid snapshot: %v", session)
	}
	content, err := h.Content.Read(ctx, caller, task.GoalRef)
	if err != nil || content.Text != "Write a greeting" {
		t.Fatalf("goal not persisted: %v %v", content, err)
	}
}
