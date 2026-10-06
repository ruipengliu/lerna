package admission_test

import (
	"slices"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G6、完成-1、完成-3、完成-4、完成-5、完成-6、完成-7
func TestCompletionFreezesVerifiedResultAfterOneActualCall(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("scoped-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: f.capability, ParametersRef: f.parameters}}}})
	accepted(t, r, e)
	a, c := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("complete-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	proposal, e := (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}}}).Propose(f.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("complete-proposal"), Proposal: proposal})
	accepted(t, r, e)
	start := &v1.BeginCompletionCommand{Header: header("begin-completion"), TaskId: f.task.Name, ProposalRef: r.ResultRef}
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, start)
	accepted(t, r, e)
	round, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, r.ResultRef)
	if e != nil || round.Status != "VERIFYING" || round.ControlGeneration != snap.ControlGeneration+1 {
		t.Fatalf("round %v %v", round, e)
	}
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.Conditions) != 1 || result.Conditions[0].Conclusion != "SATISFIED" || len(result.Conditions[0].EvidenceRefs) == 0 || len(result.OperationRefs) != 1 {
		t.Fatalf("result %v %v", result, e)
	}
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || !proto.Equal(task.ResultRef, result.Ref) {
		t.Fatalf("task %v %v", task, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.VerificationFreeze != 0 {
		t.Fatalf("freeze %v %v", planning, e)
	}
	round, e = f.h.Tasks.QueryVerification(f.ctx, f.caller, result.VerificationRef)
	if e != nil || round.Status != "PASSED" {
		t.Fatalf("passed %v %v", round, e)
	}
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, start)
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(result, again) {
		t.Fatalf("mutable result %v %v", again, e)
	}
	if e = f.h.Trace.Recover(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	traceView, e := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	resultLinked, verificationLinked := false, false
	for _, event := range traceView.Events {
		if event.EventType == "RESULT_FIXED" && proto.Equal(event.SourceRecordRef, result.Ref) {
			resultLinked = true
		}
		if event.EventType == "VERIFICATION_CHANGED" && proto.Equal(event.SourceRecordRef, round.Ref) {
			verificationLinked = true
		}
	}
	if !resultLinked || !verificationLinked {
		t.Fatalf("missing completion authority links: %v", traceView)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("target requests=%d effects=%d", len(requests), len(effects))
	}
}

// 规则：G2、G3、G4、R7、完成-6
func TestCompletionSealsLateAdmissionBeforeItCanStart(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("late-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	proposal = completeProposal(t, f, nil)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("late-begin"), TaskId: f.task.Name, ProposalRef: proposal})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletionClosures(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op != nil {
		t.Fatalf("delivery unexpectedly happened: %v %v", op, e)
	}
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result != nil {
		t.Fatalf("absent intent was lost: %v %v", result, e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op == nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || len(op.ClosureEvidenceRefs) == 0 {
		t.Fatalf("late intent escaped: %v %v", op, e)
	}
	seal, e := f.h.Ledger.QueryCompletionSeal(f.ctx, f.caller, op.ClosureEvidenceRefs[0])
	if e != nil || seal == nil || !seal.NoSendProven || !proto.Equal(seal.AdmissionRef, a.Ref) {
		t.Fatalf("seal %v %v", seal, e)
	}
	if e = f.h.Trace.Recover(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if e != nil {
		t.Fatal(e)
	}
	sealLinked, handoffLinked := false, false
	for _, event := range view.Events {
		sealLinked = sealLinked || event.EventType == "COMPLETION_SEALED" && proto.Equal(event.SourceRecordRef, seal.Ref)
		handoffLinked = handoffLinked || event.EventType == "COMPLETION_HANDOFF_CHANGED"
	}
	if !sealLinked || !handoffLinked {
		t.Fatal("trace lost pre-acceptance seal and original handoff")
	}
	if f.calls.Load() != 0 {
		t.Fatal("late intent sent")
	}
}
func completeProposal(t *testing.T, f *fixture, evidence []*v1.CompletionEvidence) *v1.Ref {
	t.Helper()
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("complete-request" + f.suffix), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	if evidence == nil {
		evidence = []*v1.CompletionEvidence{}
	}
	p, e := (&scripted.Reasoner{CompletionEvidence: evidence}).Propose(f.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("complete-proposal" + f.suffix), Proposal: p})
	accepted(t, r, e)
	return r.ResultRef
}

// 规则：G2、G11、完成-7
func TestRequirementReplacementSupersedesActualRoundAtomically(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := completeProposal(t, f, nil)
	r, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("begin-old"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	oldRef := r.ResultRef
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("replace"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "new", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	accepted(t, r, e)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	superseded, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	if e != nil || superseded.Status != "SUPERSEDED" || superseded.EndedReason == "" || planning.VerificationFreeze != 0 {
		t.Fatalf("non-atomic supersession %v %v %v", planning, superseded, e)
	}
	f.suffix = "new"
	p = completeProposal(t, f, nil)
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("begin-new"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	newRef := r.ResultRef
	r, e = f.h.Tasks.RecheckCompletion(f.ctx, f.caller, &v1.RecheckCompletionCommand{Header: header("late-old"), VerificationRef: oldRef})
	if e != nil || r.Error.GetCode() != "STALE_VERIFICATION" {
		t.Fatalf("old result %v %v", r, e)
	}
	planning, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.VerificationFreeze != 2 || !proto.Equal(planning.VerificationRef, newRef) {
		t.Fatalf("old round released new freeze %v %v", planning, e)
	}
}

// 规则：G2、G11、完成-4、完成-5
func TestDefinitivelyUnappliedRequiredActionRejectsOnlyItsRound(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("reject-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	if e != nil || v.Status != "REJECTED" || planning.VerificationFreeze != 0 || len(planning.RejectedVerificationOperations) != 1 {
		t.Fatalf("rejection %v %v %v", planning, v, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || task.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || !slices.Contains(task.WaitingOn, "COMPLETION:REPLAN_REQUIRED") {
		t.Fatalf("missing continuation %v %v", task, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 0 {
		t.Fatalf("target receives=%d effects=%d", len(requests), len(effects))
	}
}
func scopeRequirement(t *testing.T, f *fixture) {
	t.Helper()
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("scope" + f.suffix), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: f.capability, ParametersRef: f.parameters}}}})
	accepted(t, r, e)
}

// 规则：G3、G11、R7、完成-6
func TestRestartResumesClaimedCompletionClosureAfterLeaseExpires(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("restart-begin"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	r, e = f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("crashed-closure-worker").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_COMPLETION_CLOSURE"}, Limit: 1, LeaseMs: 1000, ProcessInstance: "crashed-worker"})
	accepted(t, r, e)
	if len(r.Jobs) != 1 {
		t.Fatal("closure responsibility missing")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" {
		t.Fatalf("restart stranded closure %v %v", result, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("restart duplicated physical call")
	}
}
