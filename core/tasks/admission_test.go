package tasks

import (
	"testing"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func baseFacts() admissionFacts {
	t := &lernav1.Task{
		TaskId: "t", RequirementsVersion: 2, InputVersion: 3, ControlGeneration: 4, PlanningGeneration: 5,
		Lifecycle: lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN, Control: lernav1.TaskControl_TASK_CONTROL_ACTIVE,
	}
	return admissionFacts{
		task: t,
		requirements: &lernav1.RequirementSet{
			Version: 2, Status: lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED, BoundInputVersion: 3,
		},
		proposal: &lernav1.Proposal{
			ProposalId: "p", RequirementsVersion: 2, InputVersion: 3, ControlGeneration: 4, PlanningGeneration: 5,
		},
		proposalStatus: proposalReceived,
		workClass:      WorkTarget,
	}
}

func wantCode(t *testing.T, f admissionFacts, code lernav1.ErrorCode) {
	t.Helper()
	err := checkAdmission(f)
	if !errs.Is(err, code) {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// 规则：准入-1
func TestAdmissionRejectsStaleOrConsumedProposal(t *testing.T) {
	if err := checkAdmission(baseFacts()); err != nil {
		t.Fatalf("base facts must pass: %v", err)
	}
	f := baseFacts()
	f.proposal.PlanningGeneration = 4
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
	f = baseFacts()
	f.proposalStatus = proposalAdmitted
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
}

// 规则：准入-2
func TestAdmissionRejectsStaleVersions(t *testing.T) {
	f := baseFacts()
	f.proposal.RequirementsVersion = 1
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT)
	f = baseFacts()
	f.proposal.InputVersion = 2
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_INPUT)
	f = baseFacts()
	f.proposal.ControlGeneration = 3
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
}

// 规则：准入-3
func TestAdmissionNeedsAcceptedRequirementsBoundToCurrentInput(t *testing.T) {
	f := baseFacts()
	f.requirements.Status = lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_DRAFT
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT)
	f = baseFacts()
	f.requirements.BoundInputVersion = 2
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_INPUT)
	// 准备类准入豁免准入-3。
	f.workClass = WorkPrepare
	if err := checkAdmission(f); err != nil {
		t.Fatalf("preparation work is exempt from 准入-3: %v", err)
	}
}

// 规则：准入-4
func TestAdmissionNeedsOpenActiveUnfrozenTask(t *testing.T) {
	f := baseFacts()
	f.task.Lifecycle = lernav1.TaskLifecycle_TASK_LIFECYCLE_CLOSED
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
	for _, c := range []lernav1.TaskControl{lernav1.TaskControl_TASK_CONTROL_PAUSED, lernav1.TaskControl_TASK_CONTROL_CANCELLING} {
		f = baseFacts()
		f.task.Control = c
		wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
	}
	f = baseFacts()
	f.frozen = true
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION)
}

// 规则：准入-5、G1
func TestAdmissionBlockedByUnsettledTargetOperation(t *testing.T) {
	f := baseFacts()
	f.operations = []*lernav1.OperationView{{
		OperationId: "op", Origin: "proposal:x/s1", Effect: lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN,
		LateEffect: lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR,
	}}
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE)
	// 还没收到执行管理的回报不等于没有执行。
	f.operations[0].Effect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNSPECIFIED
	wantCode(t, f, lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE)
	f.operations[0].Effect = lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED
	if err := checkAdmission(f); err != nil {
		t.Fatalf("an applied operation does not block: %v", err)
	}
	f.operations[0].Effect = lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED
	f.operations[0].Settled = true
	f.operations[0].LateEffect = lernav1.LateEffect_LATE_EFFECT_RULED_OUT
	if err := checkAdmission(f); err != nil {
		t.Fatalf("a settled not-applied operation does not block: %v", err)
	}
	// 收尾类准入豁免准入-5。
	f.operations[0] = &lernav1.OperationView{OperationId: "op", Origin: "proposal:x/s1", Effect: lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN}
	f.workClass = WorkClosure
	if err := checkAdmission(f); err != nil {
		t.Fatalf("closure work is exempt from 准入-5: %v", err)
	}
}
