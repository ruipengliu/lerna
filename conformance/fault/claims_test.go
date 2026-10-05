//go:build fault

package fault_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func claimCommand(id, instance string, lease int64) *v1.JobCommand {
	return &v1.JobCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: id}, ContractVersion: 1, Action: "CLAIM", ProcessInstance: instance, AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: lease}
}

// 规则：G3、G11
func TestOldWorkerCannotWriteAfterTakeover(t *testing.T) {
	h, err := assembly.Open(filepath.Join(t.TempDir(), "claims.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx := context.Background()
	caller, c := goal()
	if _, err = h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	old, err := h.Durable.ExecuteJob(ctx, caller, claimCommand("old", "old-process", 10))
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧工作者暂停；权威租约过期后新工作者取得同一责任。
	time.Sleep(20 * time.Millisecond)
	fresh, err := h.Durable.ExecuteJob(ctx, caller, claimCommand("new", "new-process", 30000))
	if err != nil || len(fresh.Jobs) != 1 {
		t.Fatalf("takeover %v %v", fresh, err)
	}
	if err = h.Sessions.ProcessClaim(ctx, old.Jobs[0]); err == nil {
		t.Fatal("old worker wrote business facts")
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED {
		t.Fatalf("stale write changed receipt %v %v", q, err)
	}
	if err = h.Sessions.ProcessClaim(ctx, fresh.Jobs[0]); err != nil {
		t.Fatal(err)
	}
	if err = h.Sessions.ProcessClaim(ctx, old.Jobs[0]); err == nil {
		t.Fatal("old worker completed after current worker")
	}
	assertOneTask(t, h, caller, c)
}

// 规则：G3、G11
func TestClaimReceiptLossReturnsOriginalClaim(t *testing.T) {
	h, err := assembly.Open(filepath.Join(t.TempDir(), "claims.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx := context.Background()
	caller, c := goal()
	if _, err = h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	fault, err := sqlite.WithFault(ctx, "durable.jobs", sqlite.LoseReceipt)
	if err != nil {
		t.Fatal(err)
	}
	cmd := claimCommand("claim", "process", 30000)
	r, err := h.Durable.ExecuteJob(fault, caller, cmd)
	if err == nil || r != nil {
		t.Fatalf("unknown leaked result %v %v", r, err)
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || len(q.Receipt.Jobs) != 1 {
		t.Fatalf("saved claim missing %v %v", q, err)
	}
	r, err = h.Durable.ExecuteJob(ctx, caller, cmd)
	if err != nil || !proto.Equal(r, q.Receipt) {
		t.Fatalf("original claim changed %v %v", r, err)
	}
}
