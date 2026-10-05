package admission_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"google.golang.org/protobuf/proto"
)

type boundedQueryLeases struct{ *durable.Service }

func (w *boundedQueryLeases) ExecuteJob(ctx context.Context, c *v1.Caller, j *v1.JobCommand) (*v1.CommandReceipt, error) {
	if j.Action == "CLAIM" {
		j = proto.Clone(j).(*v1.JobCommand)
		j.LeaseMs = 3000
	}
	return w.Service.ExecuteJob(ctx, c, j)
}

// 规则：G1、G3、G5、G11、R7
func TestLateQueryFactSurvivesExpiredWorkerAndNewReconciliationOwner(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	target.SetQueryGate(entered, release)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	f.h.Ledger.WithWork(&boundedQueryLeases{Service: f.h.LedgerWork})
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	done := make(chan error, 1)
	go func() { done <- f.h.Ledger.ProcessReconciliations(f.ctx, f.caller) }()
	<-entered
	p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	q, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.ActiveQueryRef)
	if e != nil {
		t.Fatal(e)
	}
	query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, q.QueryOperationRef.Name)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(time.Until(time.UnixMilli(query.Execution.Send.LeaseUntilUnixMs)) + 30*time.Millisecond)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || p.State != "PAUSED" || p.PauseReason != "QUERY_RESULT_UNKNOWN" {
		t.Fatalf("new owner guessed old read outcome: %v %v", p, e)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || p.State != "COMPLETED" || p.CheckCount != 1 {
		t.Fatalf("late fact fenced by expired scheduling lease: %v %v", p, e)
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "APPLIED" || original.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("late fact attribution: %v %v", original, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("new owner repeated IO: %v %v", requests, effects)
	}
}
