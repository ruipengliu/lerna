package tasks_test

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
	"testing"
	"time"
)

func TestWaitingRecoveryStartKeepsItsBoundQueryAuthority(t *testing.T) {
	s, w, p, r, back := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, err := p.Reserve(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Record(ctx, q, d.Number, tasks.DecisionRecord{Evidence: "raw", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "stable", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}}); err != nil {
		t.Fatal(err)
	}
	r, err = p.Admit(ctx, q, d.Number)
	if err != nil {
		t.Fatal(err)
	}
	action, err := p.Next(ctx, tasks.QualificationOf(r))
	if err != nil {
		t.Fatal(err)
	}
	binding := tasks.ActionBinding{Qualification: action.Qualification, OperationID: action.OperationID, Descriptor: action.Descriptor, InputRef: action.InputRef, ResourceVersion: action.ResourceVersion, ControlVersion: action.ControlVersion}
	r, err = p.Wait(ctx, tasks.QualificationOf(r), "reconciliation")
	if err != nil {
		t.Fatal(err)
	}
	recovering := w.WithActionRecovery(tasks.QualificationOf(r))
	queries, err := recovering.Actions(r.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = queries.ChargeExecutionQuery(ctx, binding, "before-start"); err != nil {
		t.Fatal(err)
	}
	if err = back.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
		return recovering.GuardExecution(tx, action.Qualification, action.OperationID, true)
	}); err != nil {
		t.Fatal(err)
	}
	if err = queries.ChargeExecutionQuery(ctx, binding, "after-start"); err != nil {
		t.Fatalf("own recovery start revoked query authority: %v", err)
	}
	guard := func(consume bool) error {
		return back.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
			return recovering.GuardExecution(tx, action.Qualification, action.OperationID, consume)
		})
	}
	if err = guard(false); err != nil {
		t.Fatalf("own start rejected driver check: %v", err)
	}
	if err = guard(true); err == nil {
		t.Fatal("own start allowed a second execution permit")
	}
	after, err := s.Load(ctx, r.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if after.Task.State != "RUNNING" || after.Task.Version != r.Task.Version+1 || len(after.Actions.Queries) != 2 || after.Actions.Actions[0].Qualification != action.Qualification || after.Actions.Actions[0].RecoveryStart != tasks.QualificationOf(r) {
		t.Fatal("recovery query changed transition or original identity")
	}
	after, err = p.Wait(ctx, tasks.QualificationOf(after), "reconciliation")
	if err != nil {
		t.Fatal(err)
	}
	if err = queries.ChargeExecutionQuery(ctx, binding, "later-transition"); err == nil {
		t.Fatal("start binding authorized a later task transition")
	}
	if err = guard(false); err == nil {
		t.Fatal("driver check accepted a later task transition")
	}
	// The allowance is only for this host's own start transition. A replaced
	// worker incarnation cannot inherit it through the old recovery object.
	back.clock.now = back.clock.now.Add(2 * time.Second)
	if _, err = p.EnsureLease(ctx, tasks.QualificationOf(after)); err != nil {
		t.Fatal(err)
	}
	if err = guard(false); err == nil {
		t.Fatal("driver check accepted a replacement worker")
	}
	if err = queries.ChargeExecutionQuery(ctx, binding, "stale-start"); err == nil {
		t.Fatal("replacement lease revived old recovery-start authority")
	}
}
