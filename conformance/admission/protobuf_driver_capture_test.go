package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G3、G4、G6、G10、G11
func TestCaptureReasonerQuestionDriverPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureReasonerQuestionDriver(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ReasonerDriverPolicy", "lerna.v1.ReasonerDriver", "lerna.v1.ConfigureReasonerDriverCommand", "lerna.v1.AdvanceReasonerTaskRequest"})
	requireCapturedFields(t, samples, []string{
		"lerna.v1.ReasonerDriverPolicy.settings", "lerna.v1.ReasonerDriverPolicy.model_capability_ref", "lerna.v1.ReasonerDriverPolicy.model_grant_ref", "lerna.v1.ReasonerDriverPolicy.session_id",
		"lerna.v1.ReasonerDriver.ref", "lerna.v1.ReasonerDriver.task_id", "lerna.v1.ReasonerDriver.policy", "lerna.v1.ReasonerDriver.configured_by", "lerna.v1.ReasonerDriver.request_ref", "lerna.v1.ReasonerDriver.outcome_ref", "lerna.v1.ReasonerDriver.state", "lerna.v1.ReasonerDriver.waiting_reason", "lerna.v1.ReasonerDriver.enabled", "lerna.v1.ReasonerDriver.contract_version", "lerna.v1.ReasonerDriver.implementation_version", "lerna.v1.ReasonerDriver.question_ref",
		"lerna.v1.ConfigureReasonerDriverCommand.header", "lerna.v1.ConfigureReasonerDriverCommand.task_id", "lerna.v1.ConfigureReasonerDriverCommand.policy", "lerna.v1.ConfigureReasonerDriverCommand.replaces", "lerna.v1.ConfigureReasonerDriverCommand.disabled",
		"lerna.v1.AdvanceReasonerTaskRequest.task_id", "lerna.v1.AdvanceReasonerTaskRequest.limit",
	})
	roundTripClosingSamples(t, samples)
}

func captureReasonerQuestionDriver(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "question-driver-"+name, m, e)
	}
	provider := simulator.NewModelProvider()
	output, e := protojson.Marshal(&v1.Proposal{Kind: "QUESTION", Question: &v1.QuestionProposal{Question: "Which destination?", Options: []string{"archive", "inbox"}, ChangesBasis: true}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	f := modelFixtureTarget(t, provider)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.SessionId = goal.Receipt.SessionRef.Name
	configure := &v1.ConfigureReasonerDriverCommand{Header: header("capture-question-driver-configure"), TaskId: f.task.Name, Policy: policy}
	receipt, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, configure)
	accepted(t, receipt, e)
	add("configure-command", configure, nil)
	ready, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	add("ready", ready, e)
	add("saved-policy", ready.Policy, nil)
	if ready.State != "READY" || !ready.Enabled || ready.ContractVersion != 1 || ready.ImplementationVersion != "default-v1" || !proto.Equal(ready.ConfiguredBy, configure.Header.Identity) {
		t.Fatal("actual configured responsibility absent")
	}
	advance := &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1}
	var waiting *v1.ReasonerDriver
	for i := 0; i < 6; i++ {
		waiting, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
		if e != nil {
			t.Fatal(e)
		}
		if waiting.WaitingReason == "USER_INPUT_REQUIRED" {
			break
		}
	}
	add("advance-command", advance, nil)
	add("waiting", waiting, nil)
	if waiting.State != "WAITING" || waiting.WaitingReason != "USER_INPUT_REQUIRED" || waiting.RequestRef == nil || waiting.OutcomeRef == nil || waiting.QuestionRef == nil {
		t.Fatalf("original question responsibility absent: %v", waiting)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, waiting.RequestRef)
	add("original-request", request, e)
	snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
	add("original-snapshot", snapshot, e)
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, waiting.OutcomeRef)
	add("original-outcome", outcome, e)
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, waiting.QuestionRef)
	add("original-question", question, e)
	publication, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, question.PublishedBy)
	add("question-publication", publication, e)
	if question.Status != "PENDING" || !proto.Equal(publication.Receipt.ResultRef, question.Ref) || !proto.Equal(question.TaskId, request.TaskId) || !proto.Equal(outcome.RequestRef, request.Ref) || !proto.Equal(request.OutcomeReceipt.ResultRef, outcome.Ref) {
		t.Fatal("question lost original proposal/request association")
	}
	history, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, ready.Ref)
	add("ready-history", history, e)
	if !proto.Equal(history, ready) {
		t.Fatal("original driver version rewritten")
	}
	update := &v1.ConfigureReasonerDriverCommand{Header: header("capture-question-driver-disable"), TaskId: f.task.Name, Policy: policy, Replaces: waiting.Ref, Disabled: true}
	receipt, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, update)
	accepted(t, receipt, e)
	add("disable-command", update, nil)
	disabled, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	add("disabled", disabled, e)
	if disabled.Enabled || disabled.State != "STOPPED" || !proto.Equal(disabled.QuestionRef, waiting.QuestionRef) || !proto.Equal(disabled.RequestRef, waiting.RequestRef) {
		t.Fatal("lawful configuration lost original position")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
	add("restart-replay", restarted, e)
	history, e = f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, waiting.Ref)
	add("waiting-history", history, e)
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	add("budget", budget, e)
	if !proto.Equal(restarted, disabled) || !proto.Equal(history, waiting) || provider.Calls() != 1 || len(provider.Bills()) != 1 || f.calls.Load() != 1 || budget.Settled != 7 || budget.Reserved != 0 {
		t.Fatalf("driver restart: replay=%t history=%t model=%d bills=%d fixturecalls=%d settled=%d reserved=%d", proto.Equal(restarted, disabled), proto.Equal(history, waiting), provider.Calls(), len(provider.Bills()), f.calls.Load(), budget.Settled, budget.Reserved)
	}
	t.Logf("actual question driver READY→WAITING→STOPPED; bounded limit1, original request/outcome/question and immutable history; restart MODEL calls1/bills1/endpoint requests1 with no ACTION authority; %d public objects", len(*samples))
}

// 规则：H1、G1、G3、G4、G6、G10、G11、R7、准入-9、完成-6
func TestCaptureReasonerApprovedActionDriverPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureReasonerApprovedActionDriver(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ReasonerActionAuthority"})
	requireCapturedFields(t, samples, []string{"lerna.v1.ReasonerActionAuthority.capability_ref", "lerna.v1.ReasonerActionAuthority.grant_ref", "lerna.v1.ReasonerActionAuthority.confirmation_ref", "lerna.v1.ReasonerDriverPolicy.actions", "lerna.v1.ReasonerDriver.admission_ref", "lerna.v1.SnapshotConfirmation.proposal_ref"})
	roundTripClosingSamples(t, samples)
}

func captureReasonerApprovedActionDriver(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "approved-action-driver-"+name, m, e)
	}
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, true, target)
	parametersCommand := &v1.SubmitGoalCommand{Identity: header("capture-driver-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`}
	parameters, e := f.h.Content.Stage(f.ctx, f.caller, parametersCommand)
	if e != nil {
		t.Fatal(e)
	}
	add("parameters-command", parametersCommand, nil)
	f.parameters = parameters
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.AdapterRef.Name.LocalId = "reference-v1"
	cap.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	configureCap := &v1.ConfigureCapabilityCommand{Header: header("capture-driver-target-cap"), Capability: cap}
	receipt, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, configureCap)
	accepted(t, receipt, e)
	add("target-cap-command", configureCap, nil)
	f.capability = receipt.ResultRef
	cap, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	add("target-cap", cap, e)
	targetCap, targetGrant := f.capability, f.grant
	scopeRequirement(t, f)
	provider := simulator.NewModelProvider()
	captureModelAuthorityForTarget(t, samples, "approved-action-driver", f, provider)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: targetCap, ParametersRef: parameters, SchemaDigest: cap.SchemaDigest, ExpectedEvidence: []string{"original target record"}}, BasisRefs: []*v1.Ref{parameters}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.SessionId = goal.Receipt.SessionRef.Name
	policy.Actions = []*v1.ReasonerActionAuthority{{CapabilityRef: targetCap, GrantRef: targetGrant}}
	configure := &v1.ConfigureReasonerDriverCommand{Header: header("capture-approved-driver-configure"), TaskId: f.task.Name, Policy: policy}
	receipt, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, configure)
	accepted(t, receipt, e)
	add("configure-command", configure, nil)
	advance := &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1}
	var d *v1.ReasonerDriver
	for i := 0; i < 4; i++ {
		d, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
		if e != nil {
			t.Fatal(e)
		}
		if d.OutcomeRef != nil {
			break
		}
	}
	add("advance-command", advance, nil)
	add("model-position", d, nil)
	if d.OutcomeRef == nil || d.AdmissionRef != nil {
		t.Fatal("bounded original MODEL outcome absent or target already admitted")
	}
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, d.OutcomeRef)
	add("original-outcome", outcome, e)
	full, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
	add("governed-original-action", full, e)
	requestConfirmation := &v1.RequestAdmissionConfirmationCommand{Header: header("capture-driver-request-approval"), TaskId: f.task.Name, ProposalRef: full.Ref, GrantRef: targetGrant, SessionId: policy.SessionId}
	receipt, e = f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, requestConfirmation)
	accepted(t, receipt, e)
	add("request-confirmation-command", requestConfirmation, nil)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	add("pending-confirmation", pending, e)
	if pending.State != "PENDING" || !proto.Equal(pending.GetOperationAdmission().ProposalRef, full.Ref) {
		t.Fatal("original operation confirmation absent")
	}
	captureTaskConfirmationSummaries(t, samples, "approved-action-driver-pending", f)
	approve := &v1.RespondConfirmationCommand{Header: header("capture-driver-approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
	receipt, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, approve)
	accepted(t, receipt, e)
	add("approval-command", approve, nil)
	approved, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	add("approved-confirmation", approved, e)
	if approved.State != "APPROVED" {
		t.Fatal("actual approval absent")
	}
	policy.Actions[0].ConfirmationRef = approved.Ref
	update := &v1.ConfigureReasonerDriverCommand{Header: header("capture-driver-bind-original-approval"), TaskId: f.task.Name, Policy: policy, Replaces: d.Ref}
	receipt, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, update)
	accepted(t, receipt, e)
	add("authority-update-command", update, nil)
	d, e = f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	add("approved-configuration", d, e)
	add("saved-policy", d.Policy, nil)
	add("saved-action-authority", d.Policy.Actions[0], nil)
	for i := 0; i < 3; i++ {
		d, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
		if e != nil {
			t.Fatal(e)
		}
		if d.AdmissionRef != nil {
			break
		}
	}
	add("admitted-before-IO", d, nil)
	if d.AdmissionRef == nil {
		t.Fatalf("real approved admission absent: %v", d)
	}
	admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, d.AdmissionRef)
	add("original-admission", admission, e)
	consumed, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	add("consumed-confirmation", consumed, e)
	if consumed.State != "CONSUMED" || !proto.Equal(consumed.GetConsumedAdmissionRef(), admission.Ref) || !proto.Equal(admission.Origin, full.Ref) {
		t.Fatal("approval consumption lost original admission")
	}
	captureTaskConfirmationSummaries(t, samples, "approved-action-driver-consumed", f)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatal("limit1 crossed target I/O before cancellation")
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	cancel := &v1.SubmitInputCommand{Header: header("capture-driver-cancel-unsent"), SessionId: policy.SessionId, TaskId: f.task.Name, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
	receipt, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
	accepted(t, receipt, e)
	add("cancel-command", cancel, nil)
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	add("original-cancellation", scope, e)
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	begin := &v1.BeginTaskCloseCommand{Header: header("capture-driver-current-cancelled-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
	receipt, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, begin)
	accepted(t, receipt, e)
	add("begin-close-command", begin, nil)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	add("fixed-cancelled-result", result, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	add("original-unsent-operation", op, e)
	if result.Outcome != "CANCELLED" || len(result.UnknownOperationRefs) != 0 || op.Dispatch != "SEALED" || op.Execution != nil || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatal("actual no-send closing qualification absent")
	}
	completed, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
	add("completed", completed, e)
	if completed.State != "COMPLETED" || !proto.Equal(completed.RequestRef, d.RequestRef) || !proto.Equal(completed.OutcomeRef, d.OutcomeRef) || !proto.Equal(completed.AdmissionRef, d.AdmissionRef) {
		t.Fatal("COMPLETED lost original position")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, advance)
	add("restart-replay", restarted, e)
	history, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, d.Ref)
	add("admitted-history", history, e)
	fixed, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	add("unchanged-result", fixed, e)
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, d.RequestRef, 0)
	add("original-model-call", call, e)
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	add("budget", budget, e)
	requests, effects = target.Target.Snapshot()
	if !proto.Equal(history, d) || !proto.Equal(restarted, completed) || !proto.Equal(fixed, result) || !proto.Equal(call.Ref, outcome.ModelCallRef) || !proto.Equal(call.Result.CallRef, call.Ref) || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 || f.calls.Load() != 0 || provider.Calls() != 1 || len(provider.Bills()) != 1 || budget.Settled != 7 || budget.Reserved != 0 {
		t.Fatal("restart altered fixed original authority/call/fee/no-send facts")
	}
	t.Logf("actual action driver approval PENDING→APPROVED→CONSUMED original admission; limit1 before I/O→CANCEL→fixed CANCELLED→COMPLETED/restart; MODEL1/bill1 target requests0/effects0/bills0 settled7/reserved0; %d public objects", len(*samples))
}
