package admission_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	port "github.com/ruipengliu/lerna/contracts/reasoner"
	"github.com/ruipengliu/lerna/core/tasks"
	defaultreasoner "github.com/ruipengliu/lerna/defaults/reasoner"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5、G9、G10、G11
func TestDefaultReasonerWholeRequestRestartDoesNotResample(t *testing.T) {
	for _, mode := range []string{"QUESTION", "MALFORMED", "UNKNOWN", "REFUSED", "INCOMPLETE"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?","changesBasis":true},"gaps":["destination missing"]}`
			if mode == "MALFORMED" {
				provider.Output = `{"kind":"QUESTION","question":`
			}
			if mode == "UNKNOWN" {
				provider.Drop = true
			}
			if mode == "REFUSED" || mode == "INCOMPLETE" {
				provider.Status = mode
				provider.Output = "partial or refused answer"
			}
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
			if e != nil || outcome == nil {
				t.Fatalf("outcome %v: %v", outcome, e)
			}
			if mode == "QUESTION" && (outcome.Proposal.GetBodyContentRef() == nil || outcome.OutputRef == nil || outcome.UsageRef == nil) {
				t.Fatalf("proposal %v", outcome)
			}
			if mode == "MALFORMED" && outcome.ErrorCode != "INVALID_OUTPUT" {
				t.Fatalf("malformed %v", outcome)
			}
			if (mode == "REFUSED" || mode == "INCOMPLETE") && (outcome.ErrorCode != mode || outcome.UsageRef == nil || outcome.ProposalRef != nil) {
				t.Fatalf("non-complete model result %v", outcome)
			}
			if mode == "UNKNOWN" && outcome.ErrorCode != "UNKNOWN" {
				t.Fatalf("unknown %v", outcome)
			}
			if mode == "UNKNOWN" {
				budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
				if e != nil || budget.Reserved != 30 || budget.Settled != 0 {
					t.Fatalf("unknown billing released reservation: %v %v", budget, e)
				}
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			again, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
			if e != nil || !proto.Equal(again, outcome) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
				t.Fatalf("restart %v %v calls %d", again, e, provider.Calls())
			}
		})
	}
}

// 规则：G4、G6、G9、准入-2、准入-10
func TestDefaultActionUsesExactSchemaAndDerivedConcreteParameters(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	cap.Action = "CREATE"
	cap.AdapterRef.Name.LocalId = "reference-v1"
	cap.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	receipt, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("reasoner-action-cap"), Capability: cap})
	accepted(t, receipt, e)
	cap, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, receipt.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	output, e := protojson.Marshal(&v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "one", CapabilityRef: cap.Ref, SchemaDigest: cap.SchemaDigest, ArgumentsJson: []byte(`{"destination":"archive"}`), ExpectedEvidence: []string{"record at archive"}}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	run := modelRunCommand(t, f, 30000)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome.Proposal.GetStep().GetParametersRef() == nil {
		t.Fatalf("proposal %v: %v", outcome, e)
	}
	parameters, e := f.h.Content.Read(f.ctx, f.caller, outcome.Proposal.Step.ParametersRef)
	if e != nil || string(command.ContentBytes(parameters)) != `{"destination":"archive"}` || parameters.SourceDescriptor.Kind != "DERIVED" {
		t.Fatalf("parameters %v: %v", parameters, e)
	}
	if provider.Calls() != 1 {
		t.Fatal("extra call")
	}
	assertReasonerSourceRoundTrip(t, f, []reasonerSourceExpectation{
		{"PROPOSAL_RECEIVED", outcome.ProposalRef, outcome.Proposal.BodyContentRef, []*v1.Ref{cap.Ref, parameters.Ref}},
		{"PROPOSAL_OUTCOME_ACCEPTED", outcome.Ref, outcome.Proposal.BodyContentRef, []*v1.Ref{cap.Ref, parameters.Ref}},
		{"CONTENT_PUBLISHED", parameters.Ref, parameters.Ref, []*v1.Ref{outcome.OutputRef}},
		{"CONTENT_PUBLISHED", outcome.Proposal.BodyContentRef, outcome.Proposal.BodyContentRef, []*v1.Ref{outcome.OutputRef, parameters.Ref}},
	}, "record at archive")
}

// 规则：G3、G4、G6、G9
func TestDefaultQuestionPublishesPersistentRequestAndAssociatesAnswer(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?","options":["archive","inbox"],"changesBasis":true,"involvesConfirmation":true}}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	publish := &v1.PublishProposalQuestionCommand{Header: header("publish-reasoner-question"), ProposalRef: outcome.ProposalRef, SessionId: goal.Receipt.SessionRef.Name}
	receipt, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, receipt, e)
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, receipt.ResultRef)
	if e != nil || question.Status != "PENDING" {
		t.Fatalf("question %v %v", question, e)
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, question.ContentRef)
	if e != nil || !bytes.Contains(command.ContentBytes(body), []byte("Which destination?")) || body.SourceDescriptor.Kind != "DERIVED" {
		t.Fatalf("body %v %v", body, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answer, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("answer-reasoner"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: question.Ref})
	accepted(t, answer, e)
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	question, e = f.h.Sessions.QueryQuestion(f.ctx, f.caller, question.Ref)
	if e != nil || question.ResponseInputRef == nil {
		t.Fatalf("answer association %v %v", question, e)
	}
	again, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, again, e)
	if !proto.Equal(again.ResultRef, receipt.ResultRef) || provider.Calls() != 1 {
		t.Fatal("duplicate question/model")
	}
}

// 规则：G2、G3、G4、G6、完成-1
func TestSubjectiveCompletionUsesActualBoundUserConfirmationOnce(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	acceptedRequirements, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("subjective-requirement"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", Necessary: true, DescriptionRef: f.parameters, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
	accepted(t, acceptedRequirements, e)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	request, e := f.h.Tasks.RequestConditionConfirmation(f.ctx, f.caller, &v1.RequestConditionConfirmationCommand{Header: header("condition-confirmation"), TaskId: f.task.Name, RequirementsRef: acceptedRequirements.ResultRef, ConditionId: "created", EvidenceRefs: []*v1.Ref{f.parameters}, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, request, e)
	confirmation, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, request.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	approved, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("user-approves-condition"), ConfirmationRef: confirmation.Ref, BindingDigest: confirmation.BindingDigest, Decision: "APPROVE"})
	accepted(t, approved, e)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", ConfirmationRef: approved.ResultRef}}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	run := modelRunCommand(t, f, 30000)
	snapshot, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	fixed, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, snapshot.SnapshotRef)
	if e != nil || len(fixed.Confirmations) != 1 || !proto.Equal(fixed.Confirmations[0].Ref, approved.ResultRef) {
		t.Fatalf("confirmation snapshot %v %v", fixed, e)
	}
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome.ProposalRef == nil {
		t.Fatalf("proposal %v %v", outcome, e)
	}
	begin := &v1.BeginCompletionCommand{Header: header("complete-subjective"), TaskId: f.task.Name, ProposalRef: outcome.ProposalRef}
	started, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, started, e)
	again, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, again, e)
	if !proto.Equal(started, again) {
		t.Fatal("completion replay differs")
	}
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || result.Conditions[0].Conclusion != "SATISFIED" {
		t.Fatalf("result %v %v", result, e)
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, approved.ResultRef.Name)
	if e != nil || current.State != "CONSUMED" || current.GetConsumedVerificationRef() == nil {
		t.Fatalf("consumption %v %v", current, e)
	}
	assertReasonerSourceRoundTrip(t, f, []reasonerSourceExpectation{
		{"CONDITION_CONFIRMATION_REQUESTED", confirmation.Ref, nil, []*v1.Ref{acceptedRequirements.ResultRef, f.parameters}},
		{"CONFIRMATION_PENDING", confirmation.Ref, nil, []*v1.Ref{acceptedRequirements.ResultRef, f.parameters}},
		{"CONFIRMATION_APPROVED", approved.ResultRef, nil, []*v1.Ref{acceptedRequirements.ResultRef, f.parameters}},
		{"CONFIRMATION_CONSUMED", current.Ref, nil, []*v1.Ref{acceptedRequirements.ResultRef, f.parameters, current.GetConsumedVerificationRef()}},
		{"PROPOSAL_REQUESTED", fixed.Ref, nil, []*v1.Ref{approved.ResultRef, acceptedRequirements.ResultRef, f.parameters}},
		{"PROPOSAL_RECEIVED", outcome.ProposalRef, outcome.Proposal.BodyContentRef, []*v1.Ref{approved.ResultRef}},
		{"VERIFICATION_CHANGED", started.ResultRef, nil, []*v1.Ref{current.Ref}},
		{"RESULT_FIXED", result.Ref, nil, []*v1.Ref{current.Ref, f.parameters}},
	})
}

// 规则：G2、G4、G5、G6、准入-2
func TestDefaultRejectsIncompleteAndForgedCompletionWithoutRepair(t *testing.T) {
	for name, body := range map[string]string{
		"missing-required":      `{"kind":"COMPLETE"}`,
		"invented-confirmation": `{"kind":"COMPLETE","verdicts":[{"conditionId":"created","conclusion":"SATISFIED","confirmationRef":{"name":{"userId":"u","authorityDomainId":"d","objectKind":"confirmation","localId":"made-up"},"revision":"2","schemaId":"lerna.v1.Confirmation"}}]}`,
		"invented-operation":    `{"kind":"COMPLETE","verdicts":[{"conditionId":"created","conclusion":"SATISFIED","operationId":{"userId":"u","authorityDomainId":"d/ledger","objectKind":"operation","localId":"made-up"},"evidenceRefs":[{"revision":"1"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Output = body
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
			if e != nil || outcome.ErrorCode != "INVALID_OUTPUT" || outcome.Proposal != nil || outcome.UsageRef == nil || provider.Calls() != 1 {
				t.Fatalf("outcome %v %v calls=%d", outcome, e, provider.Calls())
			}
			result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			if e != nil || result != nil {
				t.Fatal("model completed task")
			}
		})
	}
}

type testReasoner func(context.Context, *v1.ContextSnapshot, ...*port.Invocation) (*v1.Proposal, error)

func (f testReasoner) Propose(ctx context.Context, s *v1.ContextSnapshot, inv ...*port.Invocation) (*v1.Proposal, error) {
	return f(ctx, s, inv...)
}

// 规则：G4、G5、G7、G8
func TestReasonerNarrowCallerRejectsUnboundMaterialsAndNewPosition(t *testing.T) {
	for _, mode := range []string{"unbound-content", "second-position", "changed-model"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			extra, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("unrelated-material").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "unrelated private material"})
			if e != nil {
				t.Fatal(e)
			}
			impl := testReasoner(func(ctx context.Context, s *v1.ContextSnapshot, inv ...*port.Invocation) (*v1.Proposal, error) {
				request := &port.ModelRequest{Settings: proto.Clone(run.Preparation.Settings).(*v1.ModelSettings), InputRefs: s.ContentRefs}
				request.Settings.ViewPolicy = port.ViewPolicy
				request.Settings.MaxInputBytes = 262144
				request.Settings.MaxInputTokens = 65536
				switch mode {
				case "unbound-content":
					request.InputRefs = append(request.InputRefs, extra)
				case "second-position":
					request.Position = 1
				case "changed-model":
					request.Settings.Model = "not-allowed"
				}
				_, e := inv[0].Model.Call(ctx, request)
				return nil, e
			})
			_, _ = f.h.Tasks.RunReasoner(f.ctx, f.caller, run, impl)
			if provider.Calls() != 0 {
				t.Fatal("narrow model caller expanded authority")
			}
		})
	}
}

func (testReasoner) DescribeReasoner(version uint32) (*v1.ReasonerDescription, error) {
	return (&defaultreasoner.Reasoner{}).DescribeReasoner(version)
}

// 规则：G2、G3、G4、G6、准入-3
func TestRequirementsProposalWaitsForActualExplicitUserConditions(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	conditions := []*v1.Requirement{{ConditionId: "reviewed", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
	output, e := protojson.Marshal(&v1.Proposal{Kind: "REQUIREMENTS", RequirementsChange: &v1.RequirementsProposal{OriginalVersion: 1, Conditions: conditions, Reason: "destination was not specified", Impact: "replace created with reviewed"}, Gaps: []string{"user must approve changed obligations"}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	run := modelRunCommand(t, f, 30000)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome.ProposalRef == nil {
		t.Fatalf("proposal %v %v", outcome, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.Requirements.RequirementsVersion != 1 || planning.Requirements.Conditions[0].ConditionId != "created" {
		t.Fatal("model weakened accepted conditions")
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	question, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, &v1.PublishProposalQuestionCommand{Header: header("requirements-question"), ProposalRef: outcome.ProposalRef, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, question, e)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answer, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("explicit-condition-answer"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, RequestRef: question.ResultRef, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, ExplicitConditions: conditions})
	accepted(t, answer, e)
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	processed, e := f.h.Tasks.ProcessInput(f.ctx, f.caller, &v1.ProcessInputCommand{Header: header("accept-explicit-change"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Outcome: "REPLACE", Source: "USER_EXPLICIT", Conditions: conditions, SourceInputRef: answer.ResultRef})
	accepted(t, processed, e)
	planning, e = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.Requirements.RequirementsVersion != 2 || planning.Requirements.Source != "USER_EXPLICIT" || !proto.Equal(planning.Requirements.SourceInputRef, answer.ResultRef) {
		t.Fatalf("requirements %v %v", planning, e)
	}
	assertReasonerSourceRoundTrip(t, f, []reasonerSourceExpectation{
		{"PROPOSAL_RECEIVED", outcome.ProposalRef, outcome.Proposal.BodyContentRef, []*v1.Ref{f.parameters}},
		{"PROPOSAL_OUTCOME_ACCEPTED", outcome.Ref, outcome.Proposal.BodyContentRef, []*v1.Ref{f.parameters}},
		{"REQUIREMENTS_ACCEPTED", planning.Requirements.Ref, nil, []*v1.Ref{f.parameters, answer.ResultRef}},
	}, "destination was not specified", "replace created with reviewed")
}

// 规则：G2、G4、G5、G6、G9、完成-1、完成-5
func TestDefaultWholeTaskActsThenCompletesFromRealTargetEvidence(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	parameters, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("task-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`})
	if e != nil {
		t.Fatal(e)
	}
	f.parameters = parameters
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("task-action-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	targetCapability, targetGrant := f.capability, f.grant
	targetSchema := capability.SchemaDigest
	scopeRequirement(t, f)
	provider := simulator.NewModelProvider()
	endpoint := httptest.NewServer(provider)
	defer endpoint.Close()
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.Action = "MODEL_INFER"
	capability.Resource = endpoint.URL
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson = nil
	capability.SchemaDigest = ""
	configured, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("completion-model-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.UsePoolId = "completion-model"
	grant.Permissions[0].Action = "MODEL_INFER"
	grant.Permissions[0].Resource = endpoint.URL
	granted, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("completion-model-grant"), Grant: grant})
	accepted(t, granted, e)
	f.grant = granted.ResultRef
	output, e := protojson.Marshal(&v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: targetCapability, SchemaDigest: targetSchema, ParametersRef: parameters, ExpectedEvidence: []string{"target acknowledges archive record"}}, BasisRefs: []*v1.Ref{parameters}})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	actionRun := modelRunCommand(t, f, 30000)
	actionProposal, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, actionRun, &defaultreasoner.Reasoner{Settings: actionRun.Preparation.Settings})
	if e != nil || actionProposal.ProposalRef == nil || actionProposal.Proposal.Kind != "ACTION" {
		t.Fatalf("action proposal %v %v", actionProposal, e)
	}
	admitted, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("default-action-admit"), TaskId: f.task.Name, ProposalRef: actionProposal.ProposalRef, GrantRef: targetGrant})
	accepted(t, admitted, e)
	modelGrant := f.grant
	f.grant = targetGrant
	action, start := prepareAdmittedStart(t, f, admitted.ResultRef, 30000)
	f.grant = modelGrant
	sent, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, sent, e)
	operation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, action.OperationId)
	if e != nil || operation.GetEffect().GetOutcome() != "APPLIED" {
		t.Fatalf("target effect %v %v", operation, e)
	}
	output, e = protojson.Marshal(&v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", OperationId: action.OperationId, EvidenceRefs: operation.Effect.EvidenceRefs}}, BasisRefs: operation.Effect.EvidenceRefs, ResultDraft: "The requested record was created."})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	f.suffix = "-completion"
	completionRun := modelRunCommand(t, f, 30000)
	if proto.Equal(actionRun.Preparation.RequestRef, completionRun.Preparation.RequestRef) {
		t.Fatal("completion reused action request")
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, completionRun.Preparation.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
	if e != nil {
		t.Fatal(e)
	}
	observed := false
	for _, fact := range snapshot.ProgressFacts {
		observed = observed || (proto.Equal(fact.OperationRef.Name, action.OperationId) && fact.EffectOutcome == "APPLIED")
	}
	if !observed {
		t.Fatal("new request lacks actual changed evidence")
	}
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, completionRun, &defaultreasoner.Reasoner{Settings: completionRun.Preparation.Settings})
	if e != nil || outcome.ProposalRef == nil {
		t.Fatalf("complete proposal %v %v", outcome, e)
	}
	if result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name); e != nil || result != nil {
		t.Fatal("proposal directly completed task")
	}
	begun, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("machine-completion"), TaskId: f.task.Name, ProposalRef: outcome.ProposalRef})
	accepted(t, begun, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 3 || result.Conditions[0].Conclusion != "SATISFIED" {
		t.Fatalf("result %v %v", result, e)
	}
	for _, run := range []*v1.RunModelCallCommand{actionRun, completionRun} {
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
		if e != nil || call.Result.GetStatus() != "COMPLETED" {
			t.Fatalf("call %v %v", call, e)
		}
		unexpected, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 1)
		if e != nil || unexpected != nil {
			t.Fatalf("second call position %v %v", unexpected, e)
		}
		if _, e = f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings}); e != nil {
			t.Fatal(e)
		}
	}
	reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, f.task.Name)
	if e != nil || len(reservations) != 3 {
		t.Fatalf("reservations %v %v", reservations, e)
	}
	for _, reservation := range reservations {
		if reservation.ConsumedSends != 1 {
			t.Fatal("multiple physical sends")
		}
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || provider.Calls() != 2 || len(provider.Bills()) != 2 {
		t.Fatal("actual calls or effects duplicated")
	}
}

// 规则：G4、G6、准入-2、准入-6
func TestCoreAdmissionIndependentlyRejectsSchemaAndConcreteParameterViolations(t *testing.T) {
	for _, mode := range []string{"schema-mismatch", "non-json", "variable"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			capability.Ref = nil
			capability.ApprovedBy = nil
			capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
			configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("strict-action"), Capability: capability})
			accepted(t, configured, e)
			f.capability = configured.ResultRef
			capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "variable" {
				f.parameters, e = f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("variable-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"${previous.result}"}`})
				if e != nil {
					t.Fatal(e)
				}
			}
			proposal := f.propose(t, func(p *v1.Proposal) {
				p.Step.SchemaDigest = capability.SchemaDigest
				if mode == "schema-mismatch" {
					p.Step.SchemaDigest = "not-current-schema"
				}
			})
			receipt, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("strict-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
			if e != nil || receipt.Error == nil {
				t.Fatalf("invalid proposal admitted: %v %v", receipt, e)
			}
			assertNoAdmission(t, f)
		})
	}
}

// 规则：G8、G9
func TestProposalMetadataDoesNotDuplicateGovernedText(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"private generated question","options":["private option"]},"gaps":["private gap"]}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil {
		t.Fatal(e)
	}
	projection, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, outcome.ProposalRef)
	if e != nil || projection.BodyContentRef == nil || projection.Question.Question != "" || len(projection.Question.Options) > 0 || len(projection.Gaps) > 0 {
		t.Fatalf("metadata leaks body %v %v", projection, e)
	}
	restored, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
	if e != nil || restored.Question.Question != "private generated question" || len(restored.Question.Options) != 1 || len(restored.Gaps) != 1 {
		t.Fatalf("governed proposal %v %v", restored, e)
	}
	content, e := f.h.Content.Read(f.ctx, f.caller, projection.BodyContentRef)
	if e != nil || content.SourceDescriptor.Kind != "DERIVED" {
		t.Fatalf("proposal provenance %v %v", content, e)
	}
	if outcome.Proposal.Question.Question != "" {
		t.Fatal("outcome exposes unchecked text")
	}
}

// 规则：G8、G9
func TestDirectProposalCannotPersistUngovernedBody(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("raw-body-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Question: &v1.QuestionProposal{Question: "private question"}}).Propose(f.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("raw-body-proposal"), Proposal: p})
	if e != nil || receipt.GetError().GetCode() != "INVALID_PROPOSAL" {
		t.Fatalf("unguarded body persisted: %v %v", receipt, e)
	}
}

type unavailableProposalBody struct {
	tasks.ModelContent
	ref *v1.Ref
}

func (c unavailableProposalBody) Read(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.Content, error) {
	if proto.Equal(ref, c.ref) {
		return nil, command.Fail("CONTENT_UNUSABLE")
	}
	return c.ModelContent.Read(ctx, caller, ref)
}

// 规则：G8、G9
func TestUnavailableProposalBodyDoesNotLeakThroughPublicQueries(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"private question"},"gaps":["private gap"]}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil {
		t.Fatal(e)
	}
	f.h.Tasks.WithModelContent(unavailableProposalBody{ModelContent: f.h.Content, ref: outcome.Proposal.BodyContentRef})
	restored, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
	if restored != nil || e == nil {
		t.Fatalf("unavailable body read: %v %v", restored, e)
	}
	var displayed bytes.Buffer
	cli := interaction.CLI{Tasks: f.h.Tasks, Caller: f.caller, Domain: "d"}
	if e := cli.Run(f.ctx, []string{"read-proposal", "--json", writeReasonerCLIJSON(t, "unavailable-proposal.json", outcome.ProposalRef)}, &displayed); e == nil || displayed.Len() != 0 {
		t.Fatalf("CLI leaked unavailable body: %v %s", e, displayed.Bytes())
	}
	proposal, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, outcome.ProposalRef)
	if e != nil {
		t.Fatal(e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	queried, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, outcome.Ref)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil {
		t.Fatal(e)
	}
	for _, message := range []proto.Message{proposal, planning, queried, replay} {
		encoded, e := protojson.Marshal(message)
		if e != nil || bytes.Contains(encoded, []byte("private question")) || bytes.Contains(encoded, []byte("private gap")) {
			t.Fatalf("public query leaked unavailable text: %s %v", encoded, e)
		}
	}
	if provider.Calls() != 1 {
		t.Fatal("unavailable body resampled model")
	}
}

// 规则：G5、G9、G11
func TestReasonerMandatoryContextOverflowFailsBeforeAdmission(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	run.Preparation.Settings.MaxInputBytes = 1
	run.Preparation.Settings.MaxInputTokens = 1
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome.GetErrorCode() != "CONTEXT_TOO_LARGE" {
		t.Fatalf("overflow %v %v", outcome, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 || budget.Settled != 0 || provider.Calls() != 0 || len(provider.Bills()) != 0 {
		t.Fatalf("overflow admitted model: %v %v", budget, e)
	}
	replay, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || !proto.Equal(replay, outcome) {
		t.Fatalf("overflow replay %v %v", replay, e)
	}
}

// 规则：G3、G4、G5、G6、G9、R1
func TestReasonerImplementationsShareGovernedHostContract(t *testing.T) {
	for _, implementation := range []string{"scripted", "default"} {
		for _, kind := range []string{"ACTION", "QUESTION", "REQUIREMENTS", "COMPLETE"} {
			t.Run(implementation+"/"+kind, func(t *testing.T) {
				provider := simulator.NewModelProvider()
				f := modelFixtureTarget(t, provider)
				parameters, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("shared-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{}`})
				if e != nil {
					t.Fatal(e)
				}
				task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				conditions := []*v1.Requirement{{ConditionId: "reviewed", DescriptionRef: parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
				acceptedRequirements, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("shared-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: conditions})
				accepted(t, acceptedRequirements, e)
				capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
				if e != nil {
					t.Fatal(e)
				}
				capability.Ref = nil
				capability.ApprovedBy = nil
				capability.Action = "CREATE"
				capability.ParameterSchemaJson = []byte(`{"type":"object","additionalProperties":false}`)
				configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("shared-action"), Capability: capability})
				accepted(t, configured, e)
				capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, configured.ResultRef)
				if e != nil {
					t.Fatal(e)
				}
				template := &v1.Proposal{Kind: kind, BasisRefs: []*v1.Ref{parameters}}
				switch kind {
				case "ACTION":
					template.Step = &v1.ActionStep{StepId: "one", CapabilityRef: capability.Ref, SchemaDigest: capability.SchemaDigest, ParametersRef: parameters}
				case "QUESTION":
					template.Question = &v1.QuestionProposal{Question: "Please review the result", ChangesBasis: false}
				case "REQUIREMENTS":
					template.RequirementsChange = &v1.RequirementsProposal{OriginalVersion: 2, Conditions: conditions, Reason: "clarify review", Impact: "retain required review"}
				case "COMPLETE":
					template.Verdicts = []*v1.ConditionVerdict{{ConditionId: "reviewed", Conclusion: "UNKNOWN", Gaps: []string{"user has not confirmed"}}}
				}
				encoded, e := protojson.Marshal(template)
				if e != nil {
					t.Fatal(e)
				}
				provider.Output = string(encoded)
				run := modelRunCommand(t, f, 30000)
				var impl port.Reasoner = &scripted.Reasoner{Template: template}
				expectedCalls := 0
				if implementation == "default" {
					impl = &defaultreasoner.Reasoner{Settings: run.Preparation.Settings}
					expectedCalls = 1
				}
				outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, impl)
				if e != nil || outcome.ProposalRef == nil || outcome.Proposal.BodyContentRef == nil || outcome.Proposal.Kind != kind {
					t.Fatalf("host outcome %v %v", outcome, e)
				}
				full, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
				if e != nil || full.Kind != kind || full.RequirementsVersion != 2 {
					t.Fatalf("body %v %v", full, e)
				}
				replay, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, impl)
				if e != nil || !proto.Equal(replay, outcome) || provider.Calls() != expectedCalls {
					t.Fatalf("host replay %v %v calls=%d", replay, e, provider.Calls())
				}
			})
		}
	}
}

// 规则：G4、G8、G9
func TestProposalBodyRequiresActualDerivedProvenance(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("forged-body-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Question: &v1.QuestionProposal{Question: "unproven generated body"}}).Propose(f.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := protojson.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	body, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("forged-body").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(encoded)})
	if e != nil {
		t.Fatal(e)
	}
	p.Question.Question = ""
	p.BodyContentRef = body
	receipt, e := f.h.Tasks.ReceiveProposal(f.ctx, f.caller, &v1.ReceiveProposalCommand{Header: header("forged-body-submit"), Proposal: p})
	if e != nil || receipt.GetError().GetCode() != "INVALID_PROPOSAL" {
		t.Fatalf("unproven body accepted %v %v", receipt, e)
	}
}

// 规则：G4、G6、G8
func TestHostRejectsScriptedInventedEvidenceReference(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	template := &v1.Proposal{Kind: "QUESTION", Question: &v1.QuestionProposal{Question: "Evidence?"}, BasisRefs: []*v1.Ref{{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/content", ObjectKind: "content", LocalId: "invented"}, Revision: 1, SchemaId: "lerna.v1.Content"}}}
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &scripted.Reasoner{Template: template})
	if e != nil || outcome.GetErrorCode() != "INVALID_OUTPUT" || outcome.Proposal != nil || provider.Calls() != 0 {
		t.Fatalf("invented evidence accepted: %v %v", outcome, e)
	}
}

// 规则：G2、G3、G4、G6、完成-5
func TestSubjectiveConfirmationCannotCrossConditionOrSurviveWithdrawal(t *testing.T) {
	for _, mode := range []string{"wrong-condition", "withdrawn"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			f := modelFixtureTarget(t, provider)
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			conditions := []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}, {ConditionId: "other", DescriptionRef: f.parameters, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
			requirements, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("bound-subjective"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: conditions})
			accepted(t, requirements, e)
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			condition := "created"
			if mode == "wrong-condition" {
				condition = "other"
			}
			requested, e := f.h.Tasks.RequestConditionConfirmation(f.ctx, f.caller, &v1.RequestConditionConfirmationCommand{Header: header("bound-confirmation"), TaskId: f.task.Name, RequirementsRef: requirements.ResultRef, ConditionId: condition, EvidenceRefs: []*v1.Ref{f.parameters}, SessionId: goal.Receipt.SessionRef.Name})
			accepted(t, requested, e)
			pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, requested.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			approved, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("bound-approval"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
			accepted(t, approved, e)
			output, e := protojson.Marshal(&v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", ConfirmationRef: approved.ResultRef}}})
			if e != nil {
				t.Fatal(e)
			}
			provider.Output = string(output)
			run := modelRunCommand(t, f, 30000)
			outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "wrong-condition" {
				if outcome.ErrorCode != "INVALID_OUTPUT" || outcome.ProposalRef != nil {
					t.Fatalf("cross-condition approval accepted %v", outcome)
				}
			} else {
				if outcome.ProposalRef == nil {
					t.Fatal(outcome)
				}
				withdrawn, e := f.h.Sessions.WithdrawConfirmation(f.ctx, f.caller, &v1.WithdrawConfirmationCommand{Header: header("withdraw-bound"), ConfirmationRef: approved.ResultRef})
				accepted(t, withdrawn, e)
				begun, e := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("withdrawn-completion"), TaskId: f.task.Name, ProposalRef: outcome.ProposalRef})
				if e != nil || begun.GetError().GetCode() != "CONFIRMATION_INVALID" {
					t.Fatalf("withdrawn confirmation consumed %v %v", begun, e)
				}
				planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
				if e != nil || planning.VerificationRef != nil || planning.VerificationFreeze != 0 {
					t.Fatalf("partial verification %v %v", planning, e)
				}
			}
			current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, approved.ResultRef.Name)
			if e != nil || current.State == "CONSUMED" {
				t.Fatalf("invalid confirmation consumed %v %v", current, e)
			}
			if provider.Calls() != 1 {
				t.Fatal("confirmation rejection retried model")
			}
		})
	}
}

// 规则：G5、G8、G9、准入-10
func TestReasonerClipsOptionalViewButRetainsActualDerivationParents(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?"}}`
	f := modelFixtureTarget(t, provider)
	extra, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("optional-long-material").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(bytes.Repeat([]byte("large optional material "), 2700))})
	if e != nil {
		t.Fatal(e)
	}
	run := modelRunCommand(t, f, 30000)
	run.Preparation.InputRefs = append(run.Preparation.InputRefs, extra)
	outcome, e := f.h.Tasks.RunReasoner(f.ctx, f.caller, run, &defaultreasoner.Reasoner{Settings: run.Preparation.Settings})
	if e != nil || outcome.ProposalRef == nil {
		t.Fatalf("clipped proposal %v %v", outcome, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	if e != nil {
		t.Fatal(e)
	}
	content, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	var view struct {
		Inputs  []port.ViewMaterial `json:"inputs"`
		Omitted []*v1.Ref           `json:"omitted"`
	}
	if e = json.Unmarshal(command.ContentBytes(content), &view); e != nil {
		t.Fatal(e)
	}
	omitted := false
	for _, ref := range view.Omitted {
		omitted = omitted || proto.Equal(ref, extra)
	}
	if !omitted {
		t.Fatal("optional material not recorded as omitted")
	}
	for _, input := range view.Inputs {
		if proto.Equal(input.Ref, extra) {
			t.Fatal("optional body survived clipping")
		}
	}
	derivation, e := f.h.Content.QueryDerivation(f.ctx, f.caller, call.DerivationRef)
	if e != nil {
		t.Fatal(e)
	}
	actual := false
	sealed := false
	for _, ref := range derivation.ActualInputRefs {
		actual = actual || proto.Equal(ref, extra)
	}
	for _, ref := range derivation.SealedInputRefs {
		sealed = sealed || proto.Equal(ref, extra)
	}
	if !actual || !sealed || provider.Calls() != 1 {
		t.Fatal("clipping erased actual lineage or called hidden summarizer")
	}
}

// 规则：G4、G8、G9
func TestSubjectiveConfirmationKeepsGovernedEvidenceOutOfMetadata(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	requirements, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("governed-condition"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "review", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
	accepted(t, requirements, e)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	requested, e := f.h.Tasks.RequestConditionConfirmation(f.ctx, f.caller, &v1.RequestConditionConfirmationCommand{Header: header("governed-confirmation"), TaskId: f.task.Name, RequirementsRef: requirements.ResultRef, ConditionId: "review", EvidenceRefs: []*v1.Ref{f.parameters}, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, requested, e)
	metadata, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, requested.ResultRef)
	if e != nil || bytes.Contains([]byte(metadata.Description), []byte("create a record")) {
		t.Fatalf("confirmation duplicated governed evidence: %v %v", metadata, e)
	}
	view, e := f.h.Tasks.ReadConditionConfirmation(f.ctx, f.caller, requested.ResultRef)
	if e != nil || len(view.Materials) != 1 || string(command.ContentBytes(view.Materials[0])) != "create a record" {
		t.Fatalf("governed display %v %v", view, e)
	}

	replacement, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("new-condition-description").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "a different review requirement"})
	if e != nil {
		t.Fatal(e)
	}
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	changed, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("replace-governed-condition"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "review", DescriptionRef: replacement, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
	accepted(t, changed, e)
	view, e = f.h.Tasks.ReadConditionConfirmation(f.ctx, f.caller, requested.ResultRef)
	if e != nil || !proto.Equal(view.Condition.DescriptionRef, f.parameters) || string(command.ContentBytes(view.Materials[0])) != "create a record" {
		t.Fatalf("historical matter replaced by current condition %v %v", view, e)
	}
	f.h.Tasks.WithConfirmationRequests(f.h.Sessions, unavailableProposalBody{ModelContent: f.h.Content, ref: f.parameters})
	view, e = f.h.Tasks.ReadConditionConfirmation(f.ctx, f.caller, requested.ResultRef)
	if e == nil || view != nil {
		t.Fatalf("unavailable evidence displayed %v %v", view, e)
	}
	current, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, requested.ResultRef.Name)
	if e != nil || bytes.Contains([]byte(current.Description), []byte("create a record")) {
		t.Fatalf("current confirmation leaked body %v %v", current, e)
	}

}
