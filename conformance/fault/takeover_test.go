//go:build fault

package fault_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// 旧工作者领取后暂停，租约过期，新进程实例接替并推进；旧工作者恢复后的写入被拒绝，
// 不会出现两个执行者。
//
// 规则：G11、G3
func TestTakenOverWorkerCannotOverwrite(t *testing.T) {
	h := setup(t)
	h.SubmitGoal(harness.NewID(), "g", goalCmd().GetRequirements())
	ctx := context.Background()
	adj := h.Host.Adjudication
	old, err := adj.ClaimJobs(ctx, "old-claim", "paused-proc", 1)
	if err != nil || len(old) != 1 {
		t.Fatalf("old claim: %v %v", old, err)
	}
	h.Clock.Advance(adj.LeaseDuration + time.Second)
	h.MustRun()
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks`); n != 1 {
		t.Fatalf("the new worker must take over: tasks = %d", n)
	}
	err = adj.Advance(ctx, &old[0], "tasks:create_task", func(tx *durable.Tx) (durable.Transition, error) {
		_, err := tx.Exec(`UPDATE tasks SET control_generation = control_generation + 100`)
		return durable.Done(), err
	})
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM) {
		t.Fatalf("old worker must be fenced, got %v", err)
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM tasks WHERE control_generation >= 100`); n != 0 {
		t.Fatal("fenced write leaked into the current state")
	}
}
