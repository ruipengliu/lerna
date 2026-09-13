package tasks_test

import (
	"context"
	"fmt"
	"lerna/tasks"
	"testing"
)

func TestControlFactQueriesKeepOriginalIdentityAndAllowance(t *testing.T) {
	s, _, p, r, back := actionRun(t)
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
	if err = p.ChargeExecutionFactQuery(ctx, binding, "not-controlled"); err == nil {
		t.Fatal("control fact path accepted RUN")
	}
	controls, err := s.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Get(ctx, back.token, r.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.Request(ctx, back.token, controlRequest(t, back.Service, back.token, current, "CANCEL")); err != nil {
		t.Fatal(err)
	}
	forged := binding
	forged.InputRef = "other"
	if err = p.ChargeExecutionFactQuery(ctx, forged, "forged"); err == nil {
		t.Fatal("fact query changed admitted input")
	}
	if err = p.ChargeExecutionQuery(ctx, binding, "body"); err == nil {
		t.Fatal("cancel revived execution observation")
	}
	for i := 0; i < 32; i++ {
		if err = p.ChargeExecutionFactQuery(ctx, binding, fmt.Sprintf("fact/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = p.ChargeExecutionFactQuery(ctx, binding, "over-limit"); err == nil || err.Error() != "QUERY_BUDGET_EXCEEDED" {
		t.Fatalf("fact quota was renewed: %v", err)
	}
	after, err := s.Load(ctx, r.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries) != 32 || after.Actions.Actions[0].Qualification != binding.Qualification || after.Task.Control.Intent != "CANCEL" {
		t.Fatal("fact observations changed original action or control")
	}
}
