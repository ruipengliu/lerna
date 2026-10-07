package durable_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

type declaredDurableStore struct{ durable.Store }

var (
	_ durable.Store = (*sqlite.Store)(nil)
	_ durable.Store = (*sqlite.ContentWork)(nil)
	_ durable.Store = (*sqlite.LedgerWork)(nil)
	_ durable.Store = (*sqlite.TraceWork)(nil)
	_ durable.Store = declaredDurableStore{}
)

// 规则：R6、G3、G11
func TestDurableConstructorRejectsMissingStore(t *testing.T) {
	for _, store := range []durable.Store{nil, (*declaredDurableStore)(nil)} {
		owner, err := durable.New(store, "alice", "local")
		if owner != nil || err == nil || err.Error() != "missing required dependency: durable.store" {
			t.Fatalf("construction: %v %v", owner, err)
		}
	}
}

// 规则：R6、G3、G11
func TestDeclaredDurableStorePreservesOriginalClaimAndReceipt(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "declared.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner, err := durable.New(declaredDurableStore{store}, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	seed(t, owner)
	ctx := context.Background()
	cmd := jobCommand("declared-claim")
	claimed, err := owner.ExecuteJob(ctx, caller, cmd)
	if err != nil || len(claimed.GetJobs()) != 1 {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	job, err := owner.QueryJob(ctx, caller, claimed.Jobs[0].Ref.Name)
	if err != nil || !proto.Equal(job, claimed.Jobs[0]) || job.ClaimEpoch != 1 || job.JobType != "DECIDE_GOAL" || job.State != "CLAIMED" {
		t.Fatalf("original responsibility: %v %v", job, err)
	}
	receipt, err := owner.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil || !proto.Equal(receipt.GetReceipt(), claimed) {
		t.Fatalf("original receipt: %v %v", receipt, err)
	}
	replay, err := owner.ExecuteJob(ctx, caller, cmd)
	if err != nil || !proto.Equal(replay, claimed) {
		t.Fatalf("claim replay: %v %v", replay, err)
	}
}
