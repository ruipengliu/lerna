package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
)

func captureInputsAndPlanning(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	captureObject(t, samples, "authenticated-caller", f.caller, nil)
	content, err := f.h.Content.Read(f.ctx, f.caller, f.parameters)
	captureObject(t, samples, "input-content", content, err)
	create := &v1.CreateSessionCommand{Header: header("capture-empty-session")}
	receipt, err := f.h.Sessions.CreateSession(f.ctx, f.caller, create)
	accepted(t, receipt, err)
	captureObject(t, samples, "create-session-command", create, nil)
	session, err := f.h.Sessions.QuerySession(f.ctx, f.caller, receipt.ResultRef.Name)
	captureObject(t, samples, "empty-session", session, err)
	goal, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	question := &v1.PublishQuestionCommand{Header: header("capture-question"), SessionId: goal.Receipt.SessionRef.Name, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: f.parameters, ExpiresAtUnixMs: 4102444800000}
	receipt, err = f.h.Sessions.PublishQuestion(f.ctx, f.caller, question)
	accepted(t, receipt, err)
	captureObject(t, samples, "publish-question-command", question, nil)
	q, err := f.h.Sessions.QueryQuestion(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "question", q, err)
	input := &v1.SubmitInputCommand{Header: header("capture-answer"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: q.Ref}
	receipt, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, input)
	accepted(t, receipt, err)
	captureObject(t, samples, "submit-answer-command", input, nil)
	delivery, err := f.h.Sessions.QueryInput(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "input-delivery", delivery, err)
	route := &v1.RouteInputCommand{Header: header("capture-reroute"), InputRef: delivery.Ref}
	receipt, err = f.h.Sessions.RouteInput(f.ctx, f.caller, route)
	captureObject(t, samples, "already-routed-rejection", receipt, err)
	captureObject(t, samples, "route-input-command", route, nil)
	if receipt.GetError().GetCode() != "ALREADY_ROUTED" {
		t.Fatal(receipt)
	}
	task, err = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	modify := &v1.SubmitInputCommand{Header: header("capture-modify"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "MODIFY", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion}
	receipt, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, modify)
	accepted(t, receipt, err)
	captureObject(t, samples, "modify-input-command", modify, nil)
	inputSnapshot, err := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("capture-unprocessed-model-request"), TaskId: f.task.Name})
	captureObject(t, samples, "model-unprocessed-input-snapshot", inputSnapshot, err)
	if len(inputSnapshot.UnprocessedInputs) != 1 || len(inputSnapshot.AllowedPurposes) != 1 || inputSnapshot.AllowedPurposes[0] != "INTERPRET_INPUT" {
		t.Fatal("missing mandatory pending input")
	}
	task, err = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	process := &v1.ProcessInputCommand{Header: header("capture-process"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"}
	receipt, err = f.h.Tasks.ProcessInput(f.ctx, f.caller, process)
	accepted(t, receipt, err)
	captureObject(t, samples, "process-input-command", process, nil)
	history, err := f.h.Tasks.QueryInputs(f.ctx, f.caller, f.task.Name)
	captureObject(t, samples, "task-input-history", history, err)
	task, err = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	requirements := &v1.AcceptRequirementsCommand{Header: header("capture-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}}
	receipt, err = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, requirements)
	accepted(t, receipt, err)
	captureObject(t, samples, "accept-requirements-command", requirements, nil)
	request := &v1.RequestProposalCommand{Header: header("capture-request"), TaskId: f.task.Name}
	snapshot, err := f.h.Tasks.RequestProposal(f.ctx, f.caller, request)
	if err != nil {
		t.Fatal(err)
	}
	captureObject(t, samples, "request-proposal-command", request, nil)
	proposal, err := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one", CapabilityRef: f.capability, ParametersRef: f.parameters}}).Propose(f.ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receive := &v1.ReceiveProposalCommand{Header: header("capture-proposal"), Proposal: proposal}
	receipt, err = f.h.Tasks.ReceiveProposal(f.ctx, f.caller, receive)
	accepted(t, receipt, err)
	captureObject(t, samples, "receive-proposal-command", receive, nil)
	configure := &v1.ConfigureBudgetCommand{Header: header("capture-new-budget"), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: 80}
	receipt, err = f.h.Budget.Configure(f.ctx, f.caller, configure)
	captureObject(t, samples, "configure-budget-rejection", receipt, err)
	if receipt.GetError().GetCode() != "BUDGET_ALREADY_CONFIGURED" {
		t.Fatal(receipt)
	}
	captureObject(t, samples, "configure-budget-command", configure, nil)

	cap, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if err != nil {
		t.Fatal(err)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	capCommand := &v1.ConfigureCapabilityCommand{Header: header("capture-capability"), Capability: cap}
	receipt, err = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, capCommand)
	accepted(t, receipt, err)
	captureObject(t, samples, "configure-capability-command", capCommand, nil)
	grant, err := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if err != nil {
		t.Fatal(err)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grantCommand := &v1.ConfigureGrantCommand{Header: header("capture-grant"), Grant: grant}
	receipt, err = f.h.Grants.Configure(f.ctx, f.caller, grantCommand)
	accepted(t, receipt, err)
	captureObject(t, samples, "configure-grant-command", grantCommand, nil)
	jobs := &v1.JobCommand{Identity: header("capture-no-work").Identity, ContractVersion: 1, Action: "CLAIM", Module: "sessions", AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: 1000, ProcessInstance: "capture-worker"}
	receipt, err = f.h.Durable.ExecuteJob(f.ctx, f.caller, jobs)
	accepted(t, receipt, err)
	captureObject(t, samples, "claim-job-command", jobs, nil)
	if len(receipt.Jobs) != 0 || f.calls.Load() != 0 {
		t.Fatal("sample collection unexpectedly claimed work or sent")
	}
}
