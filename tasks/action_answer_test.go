package tasks_test

import (
	"context"
	"lerna/tasks"
	"testing"
)

func TestActionTaskTransitionsToAnswerWithoutResettingHistoryOrBudget(t *testing.T) {
	service, work, actions, run, backing := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(run)
	decision, err := actions.Reserve(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = actions.PrepareAnswer(ctx, q); err == nil {
		t.Fatal("pending model request permitted answer transition")
	}
	err = actions.Record(ctx, q, decision.Number, tasks.DecisionRecord{Evidence: "decision", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "lookup", OperationID: "lookup-op", Descriptor: "search", InputRef: "query-input", ResourceVersion: 1, ControlVersion: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err = actions.Admit(ctx, q, decision.Number)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = actions.PrepareAnswer(ctx, tasks.QualificationOf(run)); err == nil {
		t.Fatal("ready action skipped")
	}
	action, err := actions.Next(ctx, tasks.QualificationOf(run))
	if err != nil {
		t.Fatal(err)
	}
	if err = work.ConsumeExecution(ctx, tasks.ExecutionReport{OperationID: action.OperationID, Qualification: action.Qualification, Revision: 1, Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Reference: "search-response"}); err != nil {
		t.Fatal(err)
	}
	run, err = service.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	original := tasks.QualificationOf(run)
	ready, err := actions.PrepareAnswer(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Actions.AnswerQualification == nil || len(ready.Actions.Actions) != 1 || ready.Task.ModelUsedRequests != 1 || ready.Task.ModelUsedTokens != 10 {
		t.Fatal("transition reset evidence or usage")
	}
	replay, err := actions.PrepareAnswer(ctx, original)
	if err != nil || replay.Task.Version != ready.Task.Version {
		t.Fatal("transition replay changed version")
	}
	if _, err = actions.Reserve(ctx, tasks.QualificationOf(ready)); err == nil {
		t.Fatal("action generation reopened")
	}
	generations, err := work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 100, OutputTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	generations = generations.WithOutputIdentities(func(ctx context.Context) (string, error) { return backing.NewOperation(ctx, backing.token) })
	reserved, err := generations.ReserveDecision(ctx, ready)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Task.ModelUsedRequests != 1 || reserved.Task.ModelReservedRequests != 1 {
		t.Fatal("answer did not use original task budget")
	}
	g := reserved.Generations[len(reserved.Generations)-1]
	if err = generations.BeginRequest(ctx, g.Qualification, 0); err != nil {
		t.Fatal(err)
	}
	if err = generations.Settle(ctx, g.Qualification, tasks.GenerationUsage{Requests: 1, Tokens: 20}); err != nil {
		t.Fatal(err)
	}
	publication := tasks.WorkChange{ChangeID: "answer", Kind: "complete", Qualification: g.Qualification, Finished: true, Proposal: tasks.Proposal{Kind: "answer", BaseVersion: g.Qualification.Version, Complete: true, Result: "controlled-answer"}}
	if err = generations.PreparePublication(ctx, publication); err != nil {
		t.Fatal(err)
	}
	done, err := generations.Commit(ctx, publication)
	if err != nil || done.Task.State != "COMPLETED" || done.Task.ModelUsedRequests != 2 || done.Task.ModelUsedTokens != 30 || len(done.Actions.Actions) != 1 {
		t.Fatalf("answer completion lost original task: state=%s requests=%d tokens=%d actions=%d err=%v", done.Task.State, done.Task.ModelUsedRequests, done.Task.ModelUsedTokens, len(done.Actions.Actions), err)
	}
}
