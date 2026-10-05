package tasks

import (
	"testing"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func completionBase() completionFacts {
	t := &lernav1.Task{TaskId: "t", RequirementsVersion: 1, InputVersion: 1, ControlGeneration: 3, FrozenRound: 1}
	rule := &lernav1.VerificationRule{
		Kind: lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED, CapabilityId: "api.put",
		Params: map[string]string{"key": "k"},
	}
	return completionFacts{
		task: t,
		requirements: &lernav1.RequirementSet{Version: 1, Status: lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED, BoundInputVersion: 1,
			Requirements: []*lernav1.Requirement{{RequirementId: "r1", Necessary: true, Rule: rule}}},
		round: &lernav1.VerificationRound{RoundNo: 1, Status: lernav1.VerificationStatus_VERIFICATION_STATUS_VERIFYING,
			RequirementsVersion: 1, InputVersion: 1, ControlGeneration: 3, OperationIds: []string{"op1"}},
		operations: []*lernav1.OperationView{{OperationId: "op1", CapabilityId: "api.put", Arguments: map[string]string{"key": "k"},
			Effect: lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED, LateEffect: lernav1.LateEffect_LATE_EFFECT_RULED_OUT, Settled: true}},
		judgements: []*lernav1.RequirementJudgement{{RequirementId: "r1", Verdict: lernav1.Verdict_VERDICT_SATISFIED, EvidenceRefs: []string{"op1"}}},
	}
}

func wantVerdict(t *testing.T, f completionFacts, v completionVerdict) completionResult {
	t.Helper()
	r := evaluateCompletion(f)
	if r.verdict != v {
		t.Fatalf("verdict = %d, want %d (gaps %v)", r.verdict, v, r.gaps)
	}
	return r
}

// 规则：完成-1、完成-2
func TestVerificationStartsOnlyOnCurrentVersions(t *testing.T) {
	fresh := func() (*lernav1.Proposal, *lernav1.Task, *lernav1.RequirementSet) {
		return &lernav1.Proposal{PlanningGeneration: 2, RequirementsVersion: 1, InputVersion: 1, ControlGeneration: 2},
			&lernav1.Task{PlanningGeneration: 2, RequirementsVersion: 1, InputVersion: 1, ControlGeneration: 2,
				Lifecycle: lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN, Control: lernav1.TaskControl_TASK_CONTROL_ACTIVE},
			&lernav1.RequirementSet{Status: lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED, BoundInputVersion: 1}
	}
	if !canBeginVerification(fresh()) {
		t.Fatal("current completion proposal must start a round")
	}
	for name, mutate := range map[string]func(*lernav1.Proposal, *lernav1.Task, *lernav1.RequirementSet){
		"old generation": func(p *lernav1.Proposal, _ *lernav1.Task, _ *lernav1.RequirementSet) { p.PlanningGeneration = 1 },
		"old input":      func(p *lernav1.Proposal, _ *lernav1.Task, _ *lernav1.RequirementSet) { p.InputVersion = 0 },
		"old control":    func(p *lernav1.Proposal, _ *lernav1.Task, _ *lernav1.RequirementSet) { p.ControlGeneration = 1 },
		"draft set": func(_ *lernav1.Proposal, _ *lernav1.Task, s *lernav1.RequirementSet) {
			s.Status = lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_DRAFT
		},
		"frozen": func(_ *lernav1.Proposal, t *lernav1.Task, _ *lernav1.RequirementSet) { t.FrozenRound = 7 },
	} {
		p, task, set := fresh()
		mutate(p, task, set)
		if canBeginVerification(p, task, set) {
			t.Fatalf("%s: must not start verification", name)
		}
	}
}

// 规则：完成-3、G2
func TestCompletionNeedsAcceptedRequirements(t *testing.T) {
	f := completionBase()
	wantVerdict(t, f, verdictPass)
	f.requirements.BoundInputVersion = 0
	wantVerdict(t, f, verdictReject)
}

// 规则：完成-4
func TestCompletionNeedsAVerifyingRound(t *testing.T) {
	f := completionBase()
	f.round.Status = lernav1.VerificationStatus_VERIFICATION_STATUS_SUPERSEDED
	wantVerdict(t, f, verdictSuperseded)
}

// 规则：完成-5、G2
func TestCompletionNeedsQualifiedEvidenceForEveryNecessaryRequirement(t *testing.T) {
	f := completionBase()
	f.judgements[0].EvidenceRefs = []string{"op-that-does-not-exist"}
	r := wantVerdict(t, f, verdictReject)
	if len(r.gaps) == 0 {
		t.Fatal("rejection must name the gap")
	}
	f = completionBase()
	f.operations[0].Arguments["key"] = "other"
	wantVerdict(t, f, verdictReject)
	f = completionBase()
	f.judgements = nil
	wantVerdict(t, f, verdictReject)
	f = completionBase()
	f.requirements.Requirements[0].Necessary = false
	f.judgements = nil
	wantVerdict(t, f, verdictPass)
}

// 规则：完成-6、G2
func TestCompletionNeedsEveryAdmittedOperationSettled(t *testing.T) {
	f := completionBase()
	f.operations[0].Settled = false
	wantVerdict(t, f, verdictWait)
	f.operations[0].ReconcileState = lernav1.ReconcileState_RECONCILE_STATE_PAUSED
	wantVerdict(t, f, verdictReject)
	f = completionBase()
	f.operations = append(f.operations, &lernav1.OperationView{OperationId: "op2", Settled: true,
		Effect: lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED, LateEffect: lernav1.LateEffect_LATE_EFFECT_RULED_OUT})
	wantVerdict(t, f, verdictReject)
	// 唯一例外：确定不会再迟到生效的未知效果，要在 Result 中披露。
	f = completionBase()
	f.round.OperationIds = append(f.round.OperationIds, "op3")
	f.operations = append(f.operations, &lernav1.OperationView{OperationId: "op3", Settled: true,
		Effect: lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN, LateEffect: lernav1.LateEffect_LATE_EFFECT_RULED_OUT})
	r := wantVerdict(t, f, verdictPass)
	if len(r.uncertainties) != 1 {
		t.Fatalf("the remaining unknown must be disclosed: %v", r.uncertainties)
	}
}

// 规则：完成-7
func TestCompletionRechecksVersionsAtClose(t *testing.T) {
	f := completionBase()
	f.task.ControlGeneration = 4
	wantVerdict(t, f, verdictSuperseded)
	f = completionBase()
	f.task.InputVersion = 2
	wantVerdict(t, f, verdictSuperseded)
}
