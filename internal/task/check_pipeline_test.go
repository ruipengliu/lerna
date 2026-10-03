package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 该外部证据边界提供明确预批准观察；真实文件读回由全项目装配合同验证。
type preapprovedCheckSource struct {
	rule     api.RuleDefinition
	scopeRef api.ContentRef
}

func (p preapprovedCheckSource) ValidateRequirements(context.Context, runtime.Scope, api.Task, api.RequirementDelta) (task.ValidationReport, error) {
	return task.ValidationReport{}, api.E("unsupported", "no_semantic_fixture")
}
func (p preapprovedCheckSource) Coverage(context.Context, runtime.Scope, api.Task) (api.GoalCoverage, error) {
	return api.GoalCoverage{}, api.E("unsupported", "no_coverage_fixture")
}
func (p preapprovedCheckSource) Check(_ context.Context, _ runtime.Scope, t api.Task, request task.CheckRequest) (api.ConditionResult, error) {
	observed := api.Time(time.Now())
	return api.ConditionResult{CheckID: request.CheckID, TaskID: t.TaskID, GoalRevision: t.GoalRevision, RequirementID: request.Input.RequirementRef.RequirementID, RequirementRevision: request.Input.RequirementRef.Revision, ArtifactRef: request.Input.ArtifactRef, RuleRef: p.rule.RuleRef, EvaluatorRef: p.rule.RuleRef, Verdict: "pass", Applicability: "usable", Basis: "verified", EvidenceRefs: request.Input.EvidenceRefs, ScopeRef: p.scopeRef, ObservedAt: observed, CheckedAt: observed}, nil
}

func TestCompletionProposalWaitsForRealCheckJobAndPreservesOriginalArtifact(t *testing.T) {
	ctx := context.Background()
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	source := &preapprovedCheckSource{rule: rule}
	h := newHarness(t, task.Ports{Gate: gate, Evidence: source}, rule)
	gate.service = governance.New(h.store, governance.Options{})
	source.scopeRef = h.content("preapproved accurate observation scope")
	current := readyTask(t, h, rule)
	prepared := h.prepared(current, "0")
	prepared.Snapshot.Purpose = "decide"
	prepared.Snapshot.CoverageRef = current.CurrentCoverageRef
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	artifact := h.content("exact original report bytes")
	proposal := task.Proposal{DecisionID: prepared.DecisionID, Kind: "complete", ReasonRef: current.GoalRef, ArtifactRefs: []api.ContentRef{artifact}, CheckRequests: []task.AttachInput{{TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementRef: api.RequirementRef{RequirementID: current.Requirements[0].RequirementID, Revision: 1}, ArtifactRef: artifact, EvidenceRefs: []api.ContentRef{h.content("independent preapproved evidence bytes")}}}}
	out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil)
	if err != nil || out.Outcome != "awaiting_checks" {
		t.Fatalf("complete proposal did not queue evidence: %+v %v", out, err)
	}
	again, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil)
	if err != nil || !api.Equal(out, again) {
		t.Fatalf("duplicate complete changed original responsibility: %+v %v", again, err)
	}
	drainKind(t, h, task.JobAdvance)
	current, err = h.service.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || current.Status == "succeeded" {
		t.Fatalf("unchecked suggestion became result: %+v %v", current, err)
	}
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !api.IsCode(err, "invalid_state") {
		t.Fatalf("pending check allowed a new decision: %v", err)
	}
	drainKind(t, h, task.JobCheck)
	drainKind(t, h, task.JobAdvance)
	view, err := h.service.Result(ctx, h.store, h.scope, h.auth, current.TaskID, task.ResultInput{})
	if err != nil || len(view.Result.ArtifactRefs) != 1 || !api.Equal(view.Result.ArtifactRefs[0], artifact) || len(view.Result.ConditionResults) != 1 || view.Result.ConditionResults[0].Verdict != "pass" {
		t.Fatalf("actual checked result lost original artifact: %+v %v", view, err)
	}
}

func TestCompleteProposalInvalidCheckRollsBackEntireEvidenceBatch(t *testing.T) {
	ctx := context.Background()
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	h := newHarness(t, task.Ports{Gate: gate}, rule)
	gate.service = governance.New(h.store, governance.Options{})
	current := readyTask(t, h, rule)
	prepared := h.prepared(current, "0")
	prepared.Snapshot.Purpose = "decide"
	prepared.Snapshot.CoverageRef = current.CurrentCoverageRef
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	artifact := h.content("fixed complete artifact")
	valid := task.AttachInput{TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementRef: api.RequirementRef{RequirementID: current.Requirements[0].RequirementID, Revision: 1}, ArtifactRef: artifact, EvidenceRefs: []api.ContentRef{h.content("explicit observation")}}
	invalid := valid
	invalid.RequirementRef.RequirementID = api.NewID("requirement")
	out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "complete", ReasonRef: current.GoalRef, ArtifactRefs: []api.ContentRef{artifact}, CheckRequests: []task.AttachInput{valid, invalid}}, nil)
	if err != nil || out.Outcome != "rejected" {
		t.Fatalf("bad check suggestion was not a finite rejection: %+v %v", out, err)
	}
	work, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobCheck}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(work) != 0 {
		t.Fatalf("rejected batch leaked partial check responsibility: %+v %v", work, err)
	}
}
