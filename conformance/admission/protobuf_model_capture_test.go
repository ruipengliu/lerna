package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"google.golang.org/protobuf/proto"
)

func captureModelCalls(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 60000)
	run.Preparation.Settings.ParametersJson = []byte(`{"seed":9007199254740993}`)
	request, err := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	captureObject(t, samples, "model-proposal-request", request, err)
	snapshot, err := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
	captureObject(t, samples, "model-context-snapshot", snapshot, err)
	call, err := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	captureObject(t, samples, "model-call-prepared", call, err)
	captureObject(t, samples, "model-prepare-command", run.Preparation, nil)
	input, err := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	captureObject(t, samples, "model-input-published", input, err)
	inputDerivation, err := f.h.Content.QueryDerivation(f.ctx, f.caller, call.DerivationRef)
	captureObject(t, samples, "model-input-derivation", inputDerivation, err)
	if inputDerivation.State != "COMMITTED" || len(inputDerivation.ActualInputRefs) != len(call.InputRefs) || input.SourceDescriptor.Kind != "DERIVED" || call.DescriptorDigest == "" {
		t.Fatal("model input not durably governed")
	}
	admit := &v1.AdmitModelCallCommand{Header: header("capture-model-admit"), RequestRef: request.Ref, Claim: run.Preparation.Claim, DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant}
	receipt, err := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, admit)
	accepted(t, receipt, err)
	captureObject(t, samples, "model-admit-command", admit, nil)
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "model-admission", admission, err)
	result, err := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	captureObject(t, samples, "model-call-result", result, err)
	captureObject(t, samples, "model-run-command", run, nil)
	if result.Status != "COMPLETED" || result.OutputRef == nil || result.UsageRef == nil {
		t.Fatal(result)
	}
	call, err = f.h.Tasks.QueryModelCall(f.ctx, f.caller, request.Ref, 0)
	captureObject(t, samples, "model-call-completed", call, err)
	execution, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, result.OperationId)
	captureObject(t, samples, "model-execution-descriptor", execution, err)
	if execution.CallDescriptor.BodyDigest == "" || execution.CallDescriptor.Digest == "" {
		t.Fatal("model descriptor missing body/digest binding")
	}
	output, err := f.h.Content.Read(f.ctx, f.caller, result.OutputRef)
	captureObject(t, samples, "model-output-published", output, err)
	outputDerivation, err := f.h.Content.QueryDerivation(f.ctx, f.caller, result.OutputDerivationRef)
	captureObject(t, samples, "model-output-derivation", outputDerivation, err)
	usage, err := f.h.Budget.QueryUsage(f.ctx, f.caller, result.UsageRef)
	captureObject(t, samples, "model-usage", usage, err)
	replay, err := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if err != nil || !proto.Equal(replay, result) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("model replay sent again: %v", err)
	}
	if !bytes.Equal(command.ContentBytes(input), provider.Bodies()[0]) || !bytes.Equal(call.Settings.ParametersJson, []byte(`{"seed":9007199254740993}`)) || string(command.ContentBytes(output)) != provider.Output {
		t.Fatal("model wire bytes do not match governed versions")
	}
	proposal, err := (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{}}).Propose(f.ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	outcomeCommand := &v1.SubmitProposalOutcomeCommand{Header: header("capture-model-outcome"), RequestRef: request.Ref, Claim: run.Preparation.Claim, Proposal: proposal, ModelCallRef: result.CallRef, OutputRef: result.OutputRef, UsageRef: result.UsageRef}
	receipt, err = f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, outcomeCommand)
	accepted(t, receipt, err)
	captureObject(t, samples, "model-outcome-command", outcomeCommand, nil)
	outcome, err := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "model-outcome-accepted", outcome, err)
	if !outcome.AcceptedForProgress || outcome.ProposalRef == nil {
		t.Fatal(outcome)
	}
	reported, err := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, request.Ref)
	captureObject(t, samples, "model-request-reported", reported, err)
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	captureObject(t, samples, "model-settled-budget", budget, err)
	if reported.State != "REPORTED" || budget.Settled != 7 || budget.Reserved != 0 || provider.Calls() != 1 {
		t.Fatal("report altered settled model responsibility")
	}

	// 独立未知场景保留原位置与费用；停止请求只关闭后续推进。
	provider = simulator.NewModelProvider()
	provider.Drop = true
	f = modelFixtureTarget(t, provider)
	run = modelRunCommand(t, f, 60000)
	result, err = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	captureObject(t, samples, "model-unknown-result", result, err)
	if result.Status != "UNKNOWN" {
		t.Fatal(result)
	}
	replay, err = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if err != nil || !proto.Equal(replay, result) || provider.Calls() != 1 {
		t.Fatalf("unknown resent: %v", err)
	}
	stop := &v1.StopProposalRequestCommand{Header: header("capture-stop-model"), RequestRef: run.Preparation.RequestRef}
	receipt, err = f.h.Tasks.StopProposalRequest(f.ctx, f.caller, stop)
	accepted(t, receipt, err)
	captureObject(t, samples, "model-stop-command", stop, nil)
	stopped, err := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	captureObject(t, samples, "model-request-stopped", stopped, err)
	if stopped.State != "STOPPED" || len(stopped.ModelOperationRefs) != 1 {
		t.Fatal(stopped)
	}
	late := &v1.SubmitProposalOutcomeCommand{Header: header("capture-stopped-outcome"), RequestRef: run.Preparation.RequestRef, Claim: run.Preparation.Claim, ErrorCode: "UNKNOWN", ModelCallRef: result.CallRef, OutputRef: result.OutputRef, UsageRef: result.UsageRef}
	receipt, err = f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, late)
	accepted(t, receipt, err)
	captureObject(t, samples, "model-stopped-outcome-command", late, nil)
	lateOutcome, err := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "model-stopped-late-outcome", lateOutcome, err)
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	captureObject(t, samples, "model-unknown-budget", budget, err)
	if lateOutcome.AcceptedForProgress || budget.Settled != 0 || budget.Reserved != 30 || provider.Calls() != 1 {
		t.Fatal("stop abandoned unknown responsibility or advanced stale outcome")
	}

	provider = simulator.NewModelProvider()
	provider.Status = "REFUSED"
	provider.Output = "policy refusal"
	f = modelFixtureTarget(t, provider)
	run = modelRunCommand(t, f, 60000)
	result, err = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	captureObject(t, samples, "model-refused-result", result, err)
	replay, err = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if err != nil || result.Status != "REFUSED" || !proto.Equal(replay, result) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("refusal repaired or resent")
	}
}
