package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G2、G3、G4、G6、G9、完成-1、完成-5
func TestCaptureConditionConfirmationPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureConditionConfirmation(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ConditionConfirmationMatter", "lerna.v1.RequestConditionConfirmationCommand", "lerna.v1.SnapshotConfirmation", "lerna.v1.ConditionVerdict"})
	requireCapturedFields(t, samples, []string{
		"lerna.v1.ConditionConfirmationMatter.task_id", "lerna.v1.ConditionConfirmationMatter.requirements_ref", "lerna.v1.ConditionConfirmationMatter.condition_id", "lerna.v1.ConditionConfirmationMatter.input_version", "lerna.v1.ConditionConfirmationMatter.control_generation", "lerna.v1.ConditionConfirmationMatter.evidence_refs",
		"lerna.v1.RequestConditionConfirmationCommand.header", "lerna.v1.RequestConditionConfirmationCommand.task_id", "lerna.v1.RequestConditionConfirmationCommand.requirements_ref", "lerna.v1.RequestConditionConfirmationCommand.condition_id", "lerna.v1.RequestConditionConfirmationCommand.evidence_refs", "lerna.v1.RequestConditionConfirmationCommand.session_id",
		"lerna.v1.Confirmation.condition_evaluation", "lerna.v1.Confirmation.consumed_verification_ref", "lerna.v1.CompletionEvidence.confirmation_ref", "lerna.v1.ContextSnapshot.confirmations", "lerna.v1.Proposal.verdicts", "lerna.v1.ConditionVerdict.confirmation_ref",
		"lerna.v1.SnapshotConfirmation.ref", "lerna.v1.SnapshotConfirmation.matter_type", "lerna.v1.SnapshotConfirmation.state", "lerna.v1.SnapshotConfirmation.condition_id", "lerna.v1.SnapshotConfirmation.requirements_ref", "lerna.v1.SnapshotConfirmation.evidence_refs", "lerna.v1.SnapshotConfirmation.input_version", "lerna.v1.SnapshotConfirmation.control_generation", "lerna.v1.SnapshotConfirmation.expires_at_unix_ms",
	})
	roundTripClosingSamples(t, samples)
}

func captureConditionConfirmation(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "condition-"+name, m, e)
	}
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	accept := &v1.AcceptRequirementsCommand{Header: header("capture-subjective-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", Necessary: true, DescriptionRef: f.parameters, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}}
	receipt, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, accept)
	accepted(t, receipt, e)
	add("accept-requirements-command", accept, nil)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	request := &v1.RequestConditionConfirmationCommand{Header: header("capture-condition-confirmation"), TaskId: f.task.Name, RequirementsRef: receipt.ResultRef, ConditionId: "created", EvidenceRefs: []*v1.Ref{f.parameters}, SessionId: goal.Receipt.SessionRef.Name}
	receipt, e = f.h.Tasks.RequestConditionConfirmation(f.ctx, f.caller, request)
	accepted(t, receipt, e)
	add("request-command", request, nil)
	pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	add("pending", pending, e)
	add("matter", pending.GetConditionEvaluation(), nil)
	if pending.State != "PENDING" || !proto.Equal(pending.GetConditionEvaluation().RequirementsRef, request.RequirementsRef) {
		t.Fatal("actual pending matter absent")
	}
	captureTaskConfirmationSummaries(t, samples, "condition-pending", f)
	displayed, e := f.h.Sessions.ReadConfirmation(f.ctx, f.caller, pending.Ref)
	add("displayed", displayed, e)
	respond := &v1.RespondConfirmationCommand{Header: header("capture-user-condition-approval"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
	receipt, e = f.h.Sessions.RespondConfirmation(f.ctx, f.caller, respond)
	accepted(t, receipt, e)
	add("respond-command", respond, nil)
	approved, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, receipt.ResultRef)
	add("approved", approved, e)
	if approved.State != "APPROVED" || approved.Ref.Revision <= pending.Ref.Revision {
		t.Fatal("actual approval transition absent")
	}
	captureTaskConfirmationSummaries(t, samples, "condition-approved", f)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", ConfirmationRef: approved.Ref}}, BasisRefs: []*v1.Ref{f.parameters, approved.Ref}, ResultDraft: "The user approved the requested result."})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	full := captureGovernedReasonerRun(t, samples, "condition-complete", f, modelRunCommand(t, f, 30000))
	add("verdict", full.Verdicts[0], nil)
	add("completion-evidence", full.CompletionEvidence[0], nil)
	if !proto.Equal(full.Verdicts[0].ConfirmationRef, approved.Ref) || !proto.Equal(full.CompletionEvidence[0].ConfirmationRef, approved.Ref) {
		t.Fatal("host lost exact original approved confirmation")
	}
	if before, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name); e != nil || before != nil {
		t.Fatal("draft invented fixed Result", e)
	}
	begin := &v1.BeginCompletionCommand{Header: header("capture-condition-complete"), TaskId: f.task.Name, ProposalRef: full.Ref}
	started, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, started, e)
	add("begin-command", begin, nil)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	add("result", result, e)
	consumed, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, pending.Ref.Name)
	add("consumed", consumed, e)
	if result == nil || result.Outcome != "SUCCEEDED" || consumed.State != "CONSUMED" || !proto.Equal(consumed.GetConsumedVerificationRef(), started.ResultRef) || !proto.Equal(consumed.GetConsumedVerificationRef().Name, result.VerificationRef.Name) {
		t.Fatal("original confirmation was not consumed by original Begin verification")
	}
	originalVerification, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, consumed.GetConsumedVerificationRef())
	add("consuming-verification", originalVerification, e)
	verification, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, result.VerificationRef)
	add("verification", verification, e)
	if originalVerification.Round != verification.Round || originalVerification.Status != "VERIFYING" || verification.Status != "PASSED" || verification.Ref.Revision <= originalVerification.Ref.Revision {
		t.Fatal("same original verification history drifted")
	}
	captureTaskConfirmationSummaries(t, samples, "condition-consumed", f)
	oldPending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, pending.Ref)
	add("pending-history", oldPending, e)
	oldApproval, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, approved.Ref)
	add("approval-history", oldApproval, e)
	again, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, again, e)
	if !proto.Equal(oldPending, pending) || !proto.Equal(oldApproval, approved) || !proto.Equal(again, started) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("confirmation/completion replay altered original history or MODEL")
	}
	t.Logf("actual condition COMPLETE: PENDING→APPROVED→CONSUMED by original Verification; MODEL calls1/bills1; %d public objects", len(*samples))
}

func captureTaskConfirmationSummaries(t *testing.T, samples *[]protobuf.Sample, prefix string, f *fixture) {
	t.Helper()
	summaries, e := f.h.Sessions.QueryTaskConfirmations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(summaries) != 1 {
		t.Fatalf("actual confirmation summaries %v %v", summaries, e)
	}
	captureObject(t, samples, prefix+"-summary", summaries[0], nil)
}
