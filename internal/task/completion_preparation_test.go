package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type completionCarrierKey struct{}

type currentCompletionGate struct{ *evidenceBridge }

func (g currentCompletionGate) Authorize(ctx context.Context, tx runtime.Tx, auth runtime.Auth, purpose string, refs []api.ContentRef, objects []api.ObjectRef) error {
	if err := g.evidenceBridge.Authorize(ctx, tx, auth, purpose, refs, objects); err != nil {
		return err
	}
	if purpose == "task.complete" && ctx.Value(completionCarrierKey{}) != "this-job-exact-original-artifacts" {
		return api.E("dependency_unavailable", "fresh_completion_sources_required")
	}
	return nil
}

type completionPreparation struct {
	calls  int
	input  task.CompleteInput
	before func()
}

func (*completionPreparation) Prepare(context.Context, runtime.Scope, runtime.Auth, api.Task) (task.PreparedDecision, error) {
	return task.PreparedDecision{}, api.E("unsupported", "ordinary_decision_is_not_a_completion_preparation")
}
func (p *completionPreparation) PrepareCompletionGate(ctx context.Context, _ runtime.Scope, _ runtime.Auth, _ api.Task, in task.CompleteInput) (context.Context, error) {
	p.calls++
	p.input = in
	if p.before != nil {
		p.before()
	}
	return context.WithValue(ctx, completionCarrierKey{}, "this-job-exact-original-artifacts"), nil
}

func TestNewCompletionJobPreparesCurrentOriginalArtifactSourcesBeforeFinalGate(t *testing.T) {
	for _, outcome := range []string{"complete", "cancel_during_preparation"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			rule := fixtureRule()
			base := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
			gate := currentCompletionGate{base}
			source := &preapprovedCheckSource{rule: rule}
			preparation := &completionPreparation{}
			h := newHarness(t, task.Ports{Gate: gate, Evidence: source, Context: preparation}, rule)
			base.service = governance.New(h.store, governance.Options{})
			source.scopeRef = h.content("preapproved observation, independent from the proposal")
			current := readyTask(t, h, rule)
			prepared := h.prepared(current, "0")
			prepared.Snapshot.Purpose, prepared.Snapshot.CoverageRef = "decide", current.CurrentCoverageRef
			if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
				t.Fatal(err)
			}
			artifact := h.content("the exact original checked artifact")
			proposal := task.Proposal{DecisionID: prepared.DecisionID, Kind: "complete", ReasonRef: current.GoalRef, ArtifactRefs: []api.ContentRef{artifact}, CheckRequests: []task.AttachInput{{TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementRef: api.RequirementRef{RequirementID: current.Requirements[0].RequirementID, Revision: 1}, ArtifactRef: artifact, EvidenceRefs: []api.ContentRef{h.content("original independent observation")}}}}
			admitted, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil)
			if err != nil || admitted.Outcome != "awaiting_checks" {
				t.Fatalf("original pending completion: %+v %v", admitted, err)
			}
			drainKind(t, h, task.JobAdvance)
			if preparation.calls != 0 {
				t.Fatal("unchecked completion read fresh data before the original check finished")
			}
			drainKind(t, h, task.JobCheck)
			if outcome == "cancel_during_preparation" {
				preparation.before = func() {
					current, err := h.service.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
					if err != nil {
						t.Fatal(err)
					}
					receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "original cancellation while fresh sources are prepared"})))
					if err != nil || receipt.Stage != "applied" {
						t.Fatalf("actual public cancellation: %+v %v", receipt, err)
					}
				}
			}
			drainKind(t, h, task.JobAdvance)
			if preparation.calls != 1 || preparation.input.TaskID != current.TaskID || preparation.input.ExpectedGoalRevision != current.GoalRevision || len(preparation.input.ArtifactRefs) != 1 || !api.Equal(preparation.input.ArtifactRefs[0], artifact) {
				t.Fatalf("new original completion job did not prepare its exact sources: calls=%d input=%+v", preparation.calls, preparation.input)
			}
			view, err := h.service.Result(ctx, h.store, h.scope, h.auth, current.TaskID, task.ResultInput{})
			if outcome == "cancel_during_preparation" {
				if !api.IsCode(err, "invalid_state") {
					t.Fatalf("preparation bypassed cancellation: %+v %v", view, err)
				}
				closed, err := h.service.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
				if err != nil || closed.Status != "cancelled" {
					t.Fatalf("cancellation changed during completion: %+v %v", closed, err)
				}
				return
			}
			if err != nil || len(view.Result.ArtifactRefs) != 1 || !api.Equal(view.Result.ArtifactRefs[0], artifact) || len(view.Result.ConditionResults) != 1 || view.Result.ConditionResults[0].Verdict != "pass" {
				t.Fatalf("current checked completion did not preserve the original artifact: %+v %v", view, err)
			}
			again, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil)
			if err != nil || !api.Equal(again, admitted) || preparation.calls != 1 {
				t.Fatalf("original completion proposal replay caused new preparation: %+v %v", again, err)
			}
		})
	}
}
