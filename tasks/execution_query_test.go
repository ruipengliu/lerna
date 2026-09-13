package tasks_test

import (
	"context"
	"fmt"
	"lerna/tasks"
	"testing"
	"time"
)

func TestExecutionQueriesRequireExplicitRecoveryBindingAndKeepQuota(t *testing.T) {
	s, w, p, r, back := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	decision, err := p.Reserve(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Record(ctx, q, decision.Number, tasks.DecisionRecord{Evidence: "raw", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "stable", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}}); err != nil {
		t.Fatal(err)
	}
	r, err = p.Admit(ctx, q, decision.Number)
	if err != nil {
		t.Fatal(err)
	}
	action, err := p.Next(ctx, tasks.QualificationOf(r))
	if err != nil {
		t.Fatal(err)
	}
	binding := tasks.ActionBinding{Qualification: action.Qualification, OperationID: action.OperationID, Descriptor: action.Descriptor, InputRef: action.InputRef, ResourceVersion: action.ResourceVersion, ControlVersion: action.ControlVersion}
	if err = p.ChargeQuery(ctx, tasks.QualificationOf(r), "model-observation"); err != nil {
		t.Fatal(err)
	}
	if err = p.ChargeExecutionQuery(ctx, binding, "initial"); err != nil {
		t.Fatal(err)
	}
	oldRecovery, err := w.WithActionRecovery(tasks.QualificationOf(r)).Actions(r.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	back.clock.now = back.clock.now.Add(2 * time.Second)
	r, err = p.EnsureLease(ctx, tasks.QualificationOf(r))
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []*tasks.ActionPort{p, oldRecovery} {
		if err = old.ChargeExecutionQuery(ctx, binding, "stale"); err == nil {
			t.Fatal("replacement lease revived an old query host")
		}
	}
	current, err := w.WithActionRecovery(tasks.QualificationOf(r)).Actions(r.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	forged := binding
	forged.Descriptor = "other"
	if err = current.ChargeExecutionQuery(ctx, forged, "forged"); err == nil {
		t.Fatal("query accepted a changed original action binding")
	}
	if err = current.ChargeExecutionQuery(ctx, binding, "recovered"); err != nil {
		t.Fatal(err)
	}
	r, err = s.Load(ctx, r.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Actions.Queries) != 3 || r.Work[0].Generation != 2 || r.Actions.Actions[0].Qualification != action.Qualification {
		t.Fatal("recovery reset queries or original invocation identity")
	}
	for i := 3; i < 32; i++ {
		if err = current.ChargeExecutionQuery(ctx, binding, fmt.Sprintf("fill/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = current.ChargeExecutionQuery(ctx, binding, "exhausted"); err == nil || err.Error() != "QUERY_BUDGET_EXCEEDED" {
		t.Fatalf("execution quota not enforced: %v", err)
	}
	back.clock.now = back.clock.now.Add(2 * time.Second)
	r, err = p.EnsureLease(ctx, tasks.QualificationOf(r))
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := w.WithActionRecovery(tasks.QualificationOf(r)).Actions(r.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = refreshed.ChargeExecutionQuery(ctx, binding, "rebound"); err == nil || err.Error() != "QUERY_BUDGET_EXCEEDED" {
		t.Fatalf("new recovery binding renewed quota: %v", err)
	}
}
