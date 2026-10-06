package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	defaultreasoner "github.com/ruipengliu/lerna/defaults/reasoner"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G3、G4、G5、G9、G11、准入-10
func TestCaptureDefaultReasonerActionPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureDefaultReasonerAction(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ReasonerDescription"})
	requireCapturedFields(t, samples, []string{
		"lerna.v1.ReasonerDescription.contract_version", "lerna.v1.ReasonerDescription.implementation_version", "lerna.v1.ReasonerDescription.supported_kinds", "lerna.v1.ReasonerDescription.max_call_positions", "lerna.v1.ReasonerDescription.max_physical_sends", "lerna.v1.ReasonerDescription.max_plan_length",
		"lerna.v1.Capability.parameter_schema_json", "lerna.v1.Capability.schema_digest", "lerna.v1.ActionStep.arguments_json", "lerna.v1.ActionStep.schema_digest", "lerna.v1.ActionStep.expected_evidence",
		"lerna.v1.Proposal.basis_refs", "lerna.v1.Proposal.gaps", "lerna.v1.Proposal.prompt_version", "lerna.v1.Proposal.body_content_ref",
		"lerna.v1.ModelSettings.view_policy", "lerna.v1.ModelSettings.max_input_bytes", "lerna.v1.ModelSettings.max_input_tokens",
	})
	if len(samples) == 0 {
		t.Fatal("actual default ACTION producer absent")
	}
	roundTripClosingSamples(t, samples)
}

func requireCapturedFields(t *testing.T, samples []protobuf.Sample, required []string) {
	t.Helper()
	report, e := protobuf.Inspect(samples)
	if e != nil {
		t.Fatal(e)
	}
	fields := map[string]bool{}
	for _, field := range report.PopulatedFields {
		fields[field] = true
	}
	for _, field := range required {
		if !fields[field] {
			t.Errorf("missing real populated field %s", field)
		}
	}
}

func requireCapturedTypes(t *testing.T, samples []protobuf.Sample, required []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, sample := range samples {
		seen[string(sample.Message.ProtoReflect().Descriptor().FullName())] = true
	}
	for _, typ := range required {
		if !seen[typ] {
			t.Errorf("missing real top-level type %s", typ)
		}
	}
	if !t.Failed() {
		t.Logf("actual top-level types roundtrip-required: %v", required)
	}
}

// 规则：H1、G3、G4、G6、G9
func TestCaptureDefaultReasonerQuestionPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureDefaultReasonerQuestion(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.QuestionProposal", "lerna.v1.PublishProposalQuestionCommand"})
	requireCapturedFields(t, samples, []string{"lerna.v1.Proposal.question", "lerna.v1.QuestionProposal.question", "lerna.v1.QuestionProposal.options", "lerna.v1.QuestionProposal.condition_ids", "lerna.v1.QuestionProposal.changes_basis", "lerna.v1.QuestionProposal.involves_confirmation", "lerna.v1.PublishProposalQuestionCommand.header", "lerna.v1.PublishProposalQuestionCommand.proposal_ref", "lerna.v1.PublishProposalQuestionCommand.session_id"})
	roundTripClosingSamples(t, samples)
}

func captureDefaultReasonerQuestion(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "QUESTION", Question: &v1.QuestionProposal{Question: "Which destination?", Options: []string{"archive", "inbox"}, ConditionIds: []string{"created"}, ChangesBasis: true, InvolvesConfirmation: true}, BasisRefs: []*v1.Ref{f.parameters}, Gaps: []string{"destination missing"}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	full := captureGovernedReasonerRun(t, samples, "reasoner-question", f, modelRunCommand(t, f, 30000))
	captureObject(t, samples, "reasoner-question-payload", full.Question, nil)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	publish := &v1.PublishProposalQuestionCommand{Header: header("capture-publish-question"), ProposalRef: full.Ref, SessionId: goal.Receipt.SessionRef.Name}
	receipt, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-question-publish-command", publish, nil)
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "reasoner-question-pending", question, e)
	if question.Status != "PENDING" || !full.Question.InvolvesConfirmation || !question.ChangesBasis {
		t.Fatal("actual question semantics absent")
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, question.ContentRef)
	captureObject(t, samples, "reasoner-question-published-body", body, e)
	if !bytes.Contains(command.ContentBytes(body), []byte(full.Question.Question)) || body.SourceDescriptor.Kind != "DERIVED" {
		t.Fatal("question lost original governed body")
	}
	confirmations, e := f.h.Sessions.QueryTaskConfirmations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(confirmations) != 0 {
		t.Fatal("question wording invented confirmation authority", e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answer := &v1.SubmitInputCommand{Header: header("capture-question-answer"), SessionId: publish.SessionId, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: question.Ref}
	receipt, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, answer)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-question-answer-command", answer, nil)
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	answered, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, question.Ref)
	captureObject(t, samples, "reasoner-question-answered", answered, e)
	if answered.ResponseInputRef == nil {
		t.Fatal("actual original answer association absent")
	}
	again, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, again, e)
	if !proto.Equal(again.ResultRef, question.Ref) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("question replay resampled or changed identity")
	}
	t.Logf("actual default QUESTION: MODEL calls1/bills1; original question/answer with no confirmation authority; %d public objects", len(*samples))
}

// 规则：H1、G2、G3、G4、G6、G9
func TestCaptureDefaultReasonerRequirementsPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureDefaultReasonerRequirements(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.RequirementsProposal"})
	requireCapturedFields(t, samples, []string{"lerna.v1.Proposal.requirements_change", "lerna.v1.RequirementsProposal.original_version", "lerna.v1.RequirementsProposal.conditions", "lerna.v1.RequirementsProposal.reason", "lerna.v1.RequirementsProposal.impact"})
	roundTripClosingSamples(t, samples)
}

func captureDefaultReasonerRequirements(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	conditions := []*v1.Requirement{{ConditionId: "reviewed", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
	output, e := protojson.Marshal(&v1.Proposal{Kind: "REQUIREMENTS", RequirementsChange: &v1.RequirementsProposal{OriginalVersion: 1, Conditions: conditions, Reason: "destination was not specified", Impact: "replace created with reviewed"}, BasisRefs: []*v1.Ref{f.parameters}, Gaps: []string{"user must approve changed obligations"}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	full := captureGovernedReasonerRun(t, samples, "reasoner-requirements", f, modelRunCommand(t, f, 30000))
	captureObject(t, samples, "reasoner-requirements-payload", full.RequirementsChange, nil)
	before, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	captureObject(t, samples, "reasoner-requirements-original", before.Requirements, nil)
	if before.Requirements.RequirementsVersion != 1 || before.Requirements.Conditions[0].ConditionId != "created" {
		t.Fatal("model changed accepted conditions before explicit input")
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	publish := &v1.PublishProposalQuestionCommand{Header: header("capture-requirements-question"), ProposalRef: full.Ref, SessionId: goal.Receipt.SessionRef.Name}
	receipt, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-requirements-publish-command", publish, nil)
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "reasoner-requirements-question", question, e)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answer := &v1.SubmitInputCommand{Header: header("capture-requirements-answer"), SessionId: publish.SessionId, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, RequestRef: question.Ref, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, ExplicitConditions: full.RequirementsChange.Conditions}
	receipt, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, answer)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-requirements-answer-command", answer, nil)
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	process := &v1.ProcessInputCommand{Header: header("capture-accept-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Outcome: "REPLACE", Source: "USER_EXPLICIT", Conditions: full.RequirementsChange.Conditions, SourceInputRef: receipt.ResultRef}
	receipt, e = f.h.Tasks.ProcessInput(f.ctx, f.caller, process)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-requirements-process-command", process, nil)
	after, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	captureObject(t, samples, "reasoner-requirements-accepted", after.Requirements, nil)
	history, e := f.h.Tasks.QueryRequirements(f.ctx, f.caller, before.Requirements.Ref)
	captureObject(t, samples, "reasoner-requirements-history", history, e)
	if after.Requirements.RequirementsVersion != 2 || after.Requirements.Source != "USER_EXPLICIT" || !proto.Equal(after.Requirements.SourceInputRef, process.SourceInputRef) || !proto.Equal(history, before.Requirements) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("actual explicit requirements or original history drifted")
	}
	t.Logf("actual default REQUIREMENTS: MODEL calls1/bills1; original v1 retained, actual USER_EXPLICIT v2; %d public objects", len(*samples))
}

func captureDefaultReasonerAction(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.Action = "CREATE"
	cap.AdapterRef.Name.LocalId = "reference-v1"
	cap.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	configure := &v1.ConfigureCapabilityCommand{Header: header("capture-default-action-cap"), Capability: cap}
	receipt, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, configure)
	accepted(t, receipt, e)
	captureObject(t, samples, "reasoner-action-cap-command", configure, nil)
	cap, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "reasoner-action-capability", cap, e)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "one", CapabilityRef: cap.Ref, SchemaDigest: cap.SchemaDigest, ArgumentsJson: []byte(`{"destination":"archive"}`), ExpectedEvidence: []string{"record at archive"}}, BasisRefs: []*v1.Ref{f.parameters}, Gaps: []string{"target evidence still required"}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	run := modelRunCommand(t, f, 30000)
	full := captureGovernedReasonerRun(t, samples, "reasoner-action", f, run)
	captureObject(t, samples, "reasoner-action-step", full.Step, nil)
	parameters, e := f.h.Content.Read(f.ctx, f.caller, full.Step.ParametersRef)
	captureObject(t, samples, "reasoner-action-derived-parameters", parameters, e)
	if string(command.ContentBytes(parameters)) != `{"destination":"archive"}` || parameters.SourceDescriptor.Kind != "DERIVED" || full.Step.SchemaDigest != cap.SchemaDigest || !bytes.Equal(full.Step.ArgumentsJson, command.ContentBytes(parameters)) {
		t.Fatal("actual schema/derived parameters absent")
	}
	description, e := (&defaultreasoner.Reasoner{}).DescribeReasoner(1)
	captureObject(t, samples, "reasoner-default-description", description, e)
	if description.ImplementationVersion != "default-v1" || len(description.SupportedKinds) != 4 || description.MaxCallPositions != 1 || description.MaxPhysicalSends != 1 || description.MaxPlanLength != 1 || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("default description or original physical counters drifted")
	}
	t.Logf("actual default ACTION: MODEL calls1/bills1; governed schema/parameters/body; %d public objects", len(*samples))
}

func captureGovernedReasonerRun(t *testing.T, samples *[]protobuf.Sample, prefix string, f *fixture, run *v1.RunModelCallCommand) *v1.Proposal {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, prefix+"-"+name, m, e)
	}
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome == nil || outcome.ErrorCode != "" || outcome.ProposalRef == nil {
		t.Fatalf("actual reasoner outcome %v %v", outcome, e)
	}
	add("run-command", run, nil)
	add("outcome", outcome, nil)
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	add("request", request, e)
	snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
	add("snapshot", snapshot, e)
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	add("call", call, e)
	add("settings", call.Settings, nil)
	if call.Settings.ViewPolicy != "m1-default-v1" || call.Settings.MaxInputBytes != 262144 || call.Settings.MaxInputTokens != 65536 || call.InputRef == nil || call.Result.OutputRef == nil {
		t.Fatal("saved normalized model settings or original refs absent")
	}
	input, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	add("model-input", input, e)
	output, e := f.h.Content.Read(f.ctx, f.caller, call.Result.OutputRef)
	add("model-output", output, e)
	projection, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, outcome.ProposalRef)
	add("projection", projection, e)
	full, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
	add("full", full, e)
	body, e := f.h.Content.Read(f.ctx, f.caller, full.BodyContentRef)
	add("body", body, e)
	// 原回报保存的是未分配提议 Ref 的结构载荷；接纳的 Ref 独立放在 ProposalRef。
	returnedPayload := proto.Clone(projection).(*v1.Proposal)
	returnedPayload.Ref = nil
	if !proto.Equal(returnedPayload, outcome.Proposal) || !proto.Equal(projection.Ref, outcome.ProposalRef) || !proto.Equal(full.Ref, projection.Ref) || !proto.Equal(full.BodyContentRef, projection.BodyContentRef) || full.PromptVersion != "m1-default-v1" || body.SourceDescriptor.Kind != "DERIVED" || len(body.DerivedFrom) == 0 || len(command.ContentBytes(body)) == 0 {
		t.Fatal("original governed full/projection/body identity drifted")
	}
	if len(projection.Gaps) != 0 || projection.ResultDraft != "" || len(projection.GetStep().GetArgumentsJson()) != 0 || len(projection.GetStep().GetExpectedEvidence()) != 0 || projection.GetQuestion().GetQuestion() != "" || len(projection.GetQuestion().GetOptions()) != 0 || projection.GetRequirementsChange().GetReason() != "" || projection.GetRequirementsChange().GetImpact() != "" {
		t.Fatal("metadata copied governed text")
	}
	for _, v := range projection.Verdicts {
		if len(v.Gaps) != 0 {
			t.Fatal("metadata copied verdict gap text")
		}
	}
	parent := false
	for _, ref := range body.DerivedFrom {
		parent = parent || proto.Equal(ref, call.Result.OutputRef)
	}
	if !parent {
		t.Fatal("proposal body lost actual output parent")
	}
	again, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || !proto.Equal(again, outcome) {
		t.Fatalf("original outcome replay %v %v", again, e)
	}
	return full
}
