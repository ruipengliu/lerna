package conformance_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3
func TestOpenAutomaticallyRecoversSubmittedGoal(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "recover"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "recover exactly once"}
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	q, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
		t.Fatalf("open did not recover pending command: %v %v", q, err)
	}
	s, err := h.Sessions.QuerySession(ctx, caller, q.Receipt.SessionRef.Name)
	if err != nil || len(s.TaskRefs) != 1 || s.LastCommittedSeq != 1 {
		t.Fatalf("recovery must create one input and task: %v %v", s, err)
	}
}
