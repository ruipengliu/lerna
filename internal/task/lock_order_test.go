package task_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type firstLedgerLockTx struct {
	runtime.Tx
	locked chan string
	first  bool
}

func (tx *firstLedgerLockTx) Get(ctx context.Context, ns, id string, value any) (uint64, error) {
	revision, err := tx.Tx.Get(ctx, ns, id, value)
	if err == nil && !tx.first && (ns == "task.tasks" || ns == "task.reservations") {
		tx.first = true
		tx.locked <- ns
		// 仅放大真实PG行锁交错，未模拟源费用或数据库提交。
		time.Sleep(100 * time.Millisecond)
	}
	return revision, err
}

func TestPostgresUsageAndDecisionAdmissionShareTaskBeforeReservationLockOrder(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN is not configured")
	}
	store, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := harnessForStore(t, store, task.Ports{})
	current := h.submit(t)
	original := h.prepared(current, "10")
	if _, err = h.service.PrepareDecision(context.Background(), store, h.scope, h.trusted(), original); err != nil {
		t.Fatal(err)
	}
	current, err = h.service.Read(context.Background(), store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	prepared := h.prepared(current, "0")
	// 字面金额夹具只检验原账务差额，不声称存在付费供应商调用。
	usage := api.UsageSnapshot{SourceRef: h.scope.Ref(original.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "7"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{h.content("literal original cumulative usage")}}
	usage.UsageDigest, _ = task.UsageDigest(usage)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type outcome struct {
		status runtime.CommitStatus
		err    error
	}
	first := make(chan string, 1)
	a := make(chan outcome, 1)
	b := make(chan outcome, 1)
	go func() {
		status, err := store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
			_, err := h.service.ReconcileUsageTx(ctx, &firstLedgerLockTx{Tx: tx, locked: first}, h.trusted(), "brain_decision", usage)
			return err
		})
		a <- outcome{status, err}
	}()
	select {
	case <-first:
	case result := <-a:
		t.Fatalf("usage ended before reaching a real source lock: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		status, err := store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
			if _, err := h.service.PrepareDecisionTx(ctx, tx, h.trusted(), prepared); err != nil {
				return err
			}
			_, err := h.service.ReconcileUsageTx(ctx, tx, h.trusted(), "brain_decision", usage)
			return err
		})
		b <- outcome{status, err}
	}()
	usageResult, admission := <-a, <-b
	if usageResult.err != nil || usageResult.status != runtime.Committed {
		t.Fatalf("original usage failed under legal admission competition: status=%s err=%v", usageResult.status, usageResult.err)
	}
	if admission.err != nil {
		var rejected *api.Error
		if admission.status != runtime.RolledBack || !errors.As(admission.err, &rejected) || rejected.Code != "revision_conflict" || rejected.Reason != "stale_snapshot" {
			t.Fatalf("admission produced a database deadlock instead of a current source fence: status=%s err=%v", admission.status, admission.err)
		}
	} else if admission.status != runtime.Committed {
		t.Fatalf("admission returned an unsupported commit result: %+v", admission)
	}
	budget, err := h.service.BudgetRead(context.Background(), store, h.scope, h.auth, current.TaskID)
	if err != nil || budget.Budget[0].Spent != "7" || budget.Budget[0].Reserved != "0" {
		t.Fatalf("same original usage was lost or charged twice: %+v %v", budget, err)
	}
}
