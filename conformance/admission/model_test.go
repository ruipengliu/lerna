package admission_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G6、G11、准入-1
func TestModelRequestPersistsAuthorityAndOriginalSnapshot(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snap.RequestRef)
	if e != nil || request == nil || !proto.Equal(request.SnapshotRef, snap.Ref) || request.MaxCallPositions != 1 || request.MaxPhysicalSends != 1 || request.Purpose != "PLAN" || request.JobRef == nil {
		t.Fatalf("request: %v %v", request, e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("claim-model").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "reasoner-a"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 || !proto.Equal(claim.Jobs[0].SpecificationRef, snap.RequestRef) {
		t.Fatalf("claim: %v", claim)
	}
	_, e = f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("new-model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	original, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snap.RequestRef)
	if e != nil || !proto.Equal(original.SnapshotRef, snap.Ref) {
		t.Fatalf("lost original request: %v %v", original, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("request sent model before admission")
	}
}

// 规则：G3、G4、G8、G9、准入-10
func TestModelPreparationCapturesActualInputsAndRejectsChangedPosition(t *testing.T) {
	f := modelFixture(t)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "model-worker"})
	accepted(t, claim, e)
	c := &v1.PrepareModelCallCommand{Header: header("model-prepare"), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{"temperature":0}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snap.ContentRefs}
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, c)
	if e != nil {
		t.Fatal(e)
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	derivation, e := f.h.Content.QueryDerivation(f.ctx, f.caller, call.DerivationRef)
	if e != nil || derivation.State != "COMMITTED" || len(derivation.ActualInputRefs) != len(snap.ContentRefs) || body.SourceDescriptor.Kind != "DERIVED" || call.DescriptorDigest == "" {
		t.Fatalf("input: %v %v %v", call, derivation, e)
	}
	again, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, c)
	if e != nil || !proto.Equal(again, call) {
		t.Fatalf("replay: %v %v", again, e)
	}
	changed := proto.Clone(c).(*v1.PrepareModelCallCommand)
	changed.Header = header("changed-model-prepare")
	changed.Settings.Model = "different-model"
	_, e = f.h.Tasks.PrepareModelCall(f.ctx, f.caller, changed)
	if e == nil || e.Error() != "MODEL_POSITION_CONFLICT" {
		t.Fatalf("changed position: %v", e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("preparation sent a model request")
	}
}

func modelFixture(t *testing.T) *fixture { return modelFixtureTarget(t, nil) }
func modelFixtureTarget(t *testing.T, target http.Handler) *fixture {
	t.Helper()
	f := newFixtureWithTarget(t, 100, 80, false, target)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	cap.Action = "MODEL_INFER"
	cap.AdapterRef.Name.LocalId = "model-reference-v1"
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("model-capability"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.Permissions[0].Action = "MODEL_INFER"
	g.UsePoolId = "model-pool"
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("model-grant"), Grant: g})
	accepted(t, r, e)
	f.grant = r.ResultRef
	return f
}

// 规则：G3、G4、G6、G10、准入-7、准入-8
func TestModelAdmissionOwnsPositionAndAppearsInCompleteTaskActionList(t *testing.T) {
	f := modelFixture(t)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "model-worker"})
	accepted(t, claim, e)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, &v1.PrepareModelCallCommand{Header: header("model-prepare"), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snap.ContentRefs})
	if e != nil {
		t.Fatal(e)
	}
	c := &v1.AdmitModelCallCommand{Header: header("model-admit"), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant}
	r, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, c)
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Reserved != 30 || len(p.AdmissionRefs) != 1 || !proto.Equal(p.AdmissionRefs[0], a.Ref) || !proto.Equal(a.ParametersRef, call.InputRef) || p.Proposal != nil || a.CapabilitySnapshot.MaxSends != 1 {
		t.Fatalf("admission %v planning %v budget %v error %v", a, p, budget, e)
	}
	c.Header = header("model-admit-replay")
	again, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, c)
	accepted(t, again, e)
	if !proto.Equal(again.ResultRef, r.ResultRef) || f.calls.Load() != 0 {
		t.Fatal("position replay created another operation")
	}
}

// 规则：G1、G3、G4、G5、G9、G10、G11、R7
func TestModelRestartReturnsOriginalOutputWithoutNewSendOrCharge(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "model-worker"})
	accepted(t, claim, e)
	prepare := &v1.PrepareModelCallCommand{Header: header("model-prepare"), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snap.ContentRefs}
	run := &v1.RunModelCallCommand{Preparation: prepare, GrantRef: f.grant}
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || result.Status != "COMPLETED" || result.OutputRef == nil || result.UsageRef == nil {
		t.Fatalf("output: %v %v", result, e)
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, result.OutputRef)
	if e != nil || string(command.ContentBytes(body)) != provider.Output || body.SourceDescriptor.Kind != "DERIVED" {
		t.Fatalf("derived output: %v %v", body, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, snap.RequestRef, 0)
	if e != nil {
		t.Fatal(e)
	}
	input, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	if e != nil || !bytes.Equal(command.ContentBytes(input), provider.Bodies()[0]) {
		t.Fatalf("sent bytes differ: %v", e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Settled != 7 || budget.Reserved != 0 {
		t.Fatalf("budget: %v %v", budget, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	recovered, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || !proto.Equal(result, recovered) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("recovery: %v %v calls=%d", recovered, e, provider.Calls())
	}
}

func modelRunCommand(t *testing.T, f *fixture, lease int64) *v1.RunModelCallCommand {
	t.Helper()
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("model-request" + f.suffix), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("model-claim" + f.suffix).Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: lease, ProcessInstance: "model-worker" + f.suffix})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatalf("claim %v", claim)
	}
	return &v1.RunModelCallCommand{Preparation: &v1.PrepareModelCallCommand{Header: header("model-prepare" + f.suffix), RequestRef: snap.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snap.ContentRefs}, GrantRef: f.grant}
}

// 规则：G4、G6、G10、准入-3、开始-2
func TestCoreOwnedModelPreparationCanInterpretDraftRequirements(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	goal, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("draft-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "clarify what completion should mean"})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	receipt, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
	if e != nil {
		t.Fatal(e)
	}
	f.task = receipt.Receipt.TaskRef
	r, e := f.h.Budget.Configure(f.ctx, f.caller, &v1.ConfigureBudgetCommand{Header: header("draft-budget"), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: 80})
	accepted(t, r, e)
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.Subject = f.task.Name
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("draft-grant"), Grant: grant})
	accepted(t, r, e)
	f.grant = r.ResultRef
	run := modelRunCommand(t, f, 30000)
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || result.Status != "COMPLETED" {
		t.Fatalf("draft model %v %v", result, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	if e != nil {
		t.Fatal(e)
	}
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, call.AdmissionRef)
	if e != nil || a.WorkCategory != "PREPARATION" {
		t.Fatalf("authority: %v %v", a, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT || provider.Calls() != 1 {
		t.Fatalf("model promoted draft requirements: %v %v", task, e)
	}
}

// 规则：G4、G5、G6、准入-1、准入-10
func TestOrdinaryProposalCannotBypassModelInputCapture(t *testing.T) {
	f := modelFixture(t)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("spoof-model"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.GetError().GetCode() != "MODEL_HOST_REQUIRED" {
		t.Fatalf("bypass: %v %v", r, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || len(planning.AdmissionRefs) != 0 || f.calls.Load() != 0 {
		t.Fatalf("leaked action: %v %v", planning, e)
	}
}

// 规则：G3、G6、G10、G11、R7
func TestModelOutcomeFencesOldWorkerButKeepsItsLateReport(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 1000)
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(1100 * time.Millisecond)
	fresh, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("new-reasoner").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "new-worker"})
	accepted(t, fresh, e)
	if len(fresh.Jobs) != 1 {
		t.Fatalf("takeover %v", fresh)
	}
	snap, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, func() *v1.Ref {
		r, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
		if e != nil {
			t.Fatal(e)
		}
		return r.SnapshotRef
	}())
	if e != nil {
		t.Fatal(e)
	}
	proposal := &v1.Proposal{TaskId: f.task.Name, ContextSnapshotRef: snap.Ref, RequestRef: snap.RequestRef, RequirementsVersion: snap.RequirementsVersion, InputVersion: snap.InputVersion, ControlGeneration: snap.ControlGeneration, PlanningGeneration: snap.PlanningGeneration, ReasonerRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "reasoner", ObjectKind: "reasoner", LocalId: "default"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}, Kind: "COMPLETE"}
	c := &v1.SubmitProposalOutcomeCommand{Header: header("old-model-outcome"), RequestRef: snap.RequestRef, Claim: run.Preparation.Claim, Proposal: proposal, ModelCallRef: result.CallRef, OutputRef: result.OutputRef, UsageRef: result.UsageRef}
	old, e := f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, c)
	accepted(t, old, e)
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, old.ResultRef)
	if e != nil || outcome.AcceptedForProgress || outcome.ReasonCode != "STALE_CLAIM" {
		t.Fatalf("old report: %v %v", outcome, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.Proposal != nil {
		t.Fatalf("old worker advanced: %v %v", planning, e)
	}
	c = proto.Clone(c).(*v1.SubmitProposalOutcomeCommand)
	c.Header = header("fresh-model-outcome")
	c.Claim = fresh.Jobs[0]
	acceptedResult, e := f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, c)
	accepted(t, acceptedResult, e)
	outcome, e = f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, acceptedResult.ResultRef)
	if e != nil || !outcome.AcceptedForProgress || outcome.ProposalRef == nil {
		t.Fatalf("fresh report: %v %v", outcome, e)
	}
	replay, e := f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, c)
	if e != nil || !proto.Equal(replay, acceptedResult) {
		t.Fatalf("outcome replay: %v %v", replay, e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snap.RequestRef)
	if e != nil || request.State != "REPORTED" || len(request.OutcomeRefs) != 2 || !proto.Equal(request.OutcomeReceipt, acceptedResult) {
		t.Fatalf("request outcomes: %v %v", request, e)
	}
	if provider.Calls() != 1 {
		t.Fatal("reporting called model again")
	}
}

// 规则：G1、G3、G10、G11、R7
func TestModelTimeoutReplacementPreservesUnknownAndLateCharge(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Drop = true
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	original, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || original.Status != "UNKNOWN" || original.OutputRef != nil {
		t.Fatalf("timeout: %v %v", original, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 30 || budget.Settled != 0 || len(provider.Bills()) != 1 {
		t.Fatalf("unknown charge: %v %v", budget, e)
	}
	f.suffix = "-replacement"
	replacement := modelRunCommand(t, f, 30000)
	old, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	if e != nil || old.State != "SUPERSEDED" {
		t.Fatalf("old responsibility: %v %v", old, e)
	}
	_, e = f.h.Tasks.RunModelCall(f.ctx, f.caller, replacement)
	if e == nil || e.Error() != "BLOCKING_OPERATION" {
		t.Fatalf("unknown bypass: %v", e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || !proto.Equal(original, again) || provider.Calls() != 1 {
		t.Fatalf("unknown replay: %v %v", again, e)
	}
	body, _ := json.Marshal(map[string]any{"billing": provider.Bills()[0]})
	evidence, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("model-late-bill").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(body)})
	if e != nil {
		t.Fatal(e)
	}
	execution, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, original.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("import-model-bill"), SendRef: execution.Send.Ref, EvidenceRef: evidence})
	accepted(t, receipt, e)
	budget, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Settled != 7 || budget.Reserved != 0 || provider.Calls() != 1 {
		t.Fatalf("late fee: %v %v", budget, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("bill changed output: %v %v", op, e)
	}
}

// 规则：G1、G4、G8、G9、准入-10
func TestModelSnapshotIncludesRequiredConditionTextAndUnknownProgress(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Drop = true
	f := modelFixtureTarget(t, provider)
	text, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("condition-text").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "The target must acknowledge its durable record"})
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("detailed-condition"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: text, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, e)
	run := modelRunCommand(t, f, 30000)
	found := false
	for _, ref := range run.Preparation.InputRefs {
		if proto.Equal(ref, text) {
			found = true
		}
	}
	if !found {
		t.Fatal("model snapshot omitted required condition body")
	}
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil {
		t.Fatal(e)
	}
	f.suffix = "-after-unknown"
	snap, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("snapshot-after-unknown"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.ProgressFacts) != 1 || snap.ProgressFacts[0].EffectOutcome != "UNKNOWN" || snap.ProgressFacts[0].LateEffect != "MAY_OCCUR" || !proto.Equal(snap.ProgressFacts[0].OperationRef.Name, result.OperationId) {
		t.Fatalf("lost unknown facts: %v", snap)
	}
}

// 规则：G3、G4、G5、准入-1
func TestModelCanonicalDescriptorPreservesLargeIntegerParameters(t *testing.T) {
	f := modelFixture(t)
	run := modelRunCommand(t, f, 30000)
	run.Preparation.Settings.ParametersJson = []byte(`{"seed":9007199254740992}`)
	first, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	if e != nil {
		t.Fatal(e)
	}
	changed := proto.Clone(run.Preparation).(*v1.PrepareModelCallCommand)
	changed.Header = header("different-large-integer")
	changed.Settings.ParametersJson = []byte(`{"seed":9007199254740993}`)
	_, e = f.h.Tasks.PrepareModelCall(f.ctx, f.caller, changed)
	if e == nil || e.Error() != "MODEL_POSITION_CONFLICT" {
		t.Fatalf("collapsed distinct parameters for %s: %v", first.DescriptorDigest, e)
	}
}

// 规则：G3、G4、G5、G10
func TestModelRefusalIncompleteAndMalformedOutputNeverRepairOrFallback(t *testing.T) {
	for _, tc := range []struct{ status, output, want string }{{"REFUSED", "policy refusal", "REFUSED"}, {"INCOMPLETE", "partial", "INCOMPLETE"}, {"COMPLETED", "broken json", "INVALID_OUTPUT"}, {"UNRECOGNIZED", "{}", "INVALID_OUTPUT"}} {
		t.Run(tc.want+tc.status, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Status = tc.status
			provider.Output = tc.output
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
			if e != nil || result.Status != tc.want {
				t.Fatalf("response %v %v", result, e)
			}
			again, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
			if e != nil || !proto.Equal(result, again) || provider.Calls() != 1 {
				t.Fatalf("automatic repair: %v %v", again, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || budget.Settled != 7 {
				t.Fatalf("lost fee: %v %v", budget, e)
			}
		})
	}
}

// 规则：G4、G11、开始-2
func TestStoppingModelRequestPreventsNewSendWithoutClosingExistingResponsibility(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, &v1.AdmitModelCallCommand{Header: header("model-admit:" + call.Ref.Name.LocalId), RequestRef: run.Preparation.RequestRef, DescriptorDigest: call.DescriptorDigest, Claim: run.Preparation.Claim, GrantRef: f.grant})
	accepted(t, r, e)
	stopped, e := f.h.Tasks.StopProposalRequest(f.ctx, f.caller, &v1.StopProposalRequestCommand{Header: header("stop-model-request"), RequestRef: run.Preparation.RequestRef})
	accepted(t, stopped, e)
	_, e = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e == nil {
		t.Fatal("stopped request sent model")
	}
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil || a == nil {
		t.Fatalf("lost admission: %v %v", a, e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	if e != nil || request.State != "STOPPED" || len(request.ModelOperationRefs) != 1 || provider.Calls() != 0 {
		t.Fatalf("stop: %v %v", request, e)
	}
}

// 规则：G3、G4、G7、准入-7、准入-8、准入-9
func TestModelConfirmationBindsSealedBytesAndConsumesOnce(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.ConfirmationRequired = true
	r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("confirmed-model-grant"), Grant: grant})
	accepted(t, r, e)
	f.grant = r.ResultRef
	run := modelRunCommand(t, f, 30000)
	_, e = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e == nil || e.Error() != "CONFIRMATION_INVALID" {
		t.Fatalf("missing confirmation: %v", e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	if e != nil {
		t.Fatal(e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 0 || call.AdmissionRef != nil {
		t.Fatalf("partial admission %v %v", budget, e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	pending, e := f.h.Tasks.RequestAdmissionConfirmation(f.ctx, f.caller, &v1.RequestAdmissionConfirmationCommand{Header: header("model-confirmation"), TaskId: f.task.Name, ProposalRef: call.Ref, GrantRef: f.grant, SessionId: goal.Receipt.SessionRef.Name})
	accepted(t, pending, e)
	confirmation, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, pending.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	approved, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve-model"), ConfirmationRef: confirmation.Ref, BindingDigest: confirmation.BindingDigest, Decision: "APPROVE"})
	accepted(t, approved, e)
	run.ConfirmationRef = approved.ResultRef
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || result.Status != "COMPLETED" {
		t.Fatalf("approved model: %v %v", result, e)
	}
	consumed, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, confirmation.Ref.Name)
	if e != nil || consumed.State != "CONSUMED" || provider.Calls() != 1 {
		t.Fatalf("confirmation: %v %v", consumed, e)
	}
	_, e = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || provider.Calls() != 1 {
		t.Fatalf("reconsumed: %v", e)
	}
}

// 规则：G3、G4、G6、G8、准入-1、准入-10
func TestModelPreparationRejectsAuthorityAndDescriptorViolations(t *testing.T) {
	for _, name := range []string{"position", "missing-input", "encoder", "duplicate-setting", "caller", "claim"} {
		t.Run(name, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 30000)
			caller := f.caller
			expected := ""
			switch name {
			case "position":
				run.Preparation.Position = 1
				expected = "MODEL_CALL_LIMIT"
			case "missing-input":
				run.Preparation.InputRefs = nil
				expected = "MISSING_REQUIRED_INPUT"
			case "encoder":
				run.Preparation.Settings.EncoderVersion = "unknown"
				expected = "PREPARATION_UNRECOVERABLE"
			case "duplicate-setting":
				run.Preparation.Settings.ParametersJson = []byte(`{"temperature":0,"temperature":1}`)
				expected = "INVALID_MODEL_SETTINGS"
			case "caller":
				caller = &v1.Caller{UserId: f.caller.UserId, IssuerId: "reasoner"}
				expected = "PERMISSION_DENIED"
			case "claim":
				run.Preparation.Claim.ClaimEpoch++
				expected = "STALE_CLAIM"
			}
			_, e := f.h.Tasks.RunModelCall(f.ctx, caller, run)
			if e == nil || e.Error() != expected {
				t.Fatalf("error %v, expected %s", e, expected)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
			if e != nil || budget.Reserved != 0 || budget.Settled != 0 || provider.Calls() != 0 {
				t.Fatalf("rejection leaked responsibility: %v %v calls=%d", budget, e, provider.Calls())
			}
		})
	}
}

// 规则：G1、G3、G6、G11、开始-2
func TestModelTakeoverBeforeSendContinuesOriginalAdmission(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 1000)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	if e != nil {
		t.Fatal(e)
	}
	admitted, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, &v1.AdmitModelCallCommand{Header: header("takeover-admission"), RequestRef: call.RequestRef, Position: call.Position, DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant, Claim: run.Preparation.Claim})
	accepted(t, admitted, e)
	time.Sleep(1100 * time.Millisecond)
	_, e = f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e == nil || e.Error() != "STALE_CLAIM" || provider.Calls() != 0 {
		t.Fatalf("old worker sent: %v calls=%d", e, provider.Calls())
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("model-takeover").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "replacement-worker"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatalf("claim: %v", claim)
	}
	run.Preparation.Claim = claim.Jobs[0]
	result, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || result.Status != "COMPLETED" || provider.Calls() != 1 {
		t.Fatalf("takeover: %v %v calls=%d", result, e, provider.Calls())
	}
	original, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, call.RequestRef, call.Position)
	if e != nil || !proto.Equal(original.AdmissionRef, admitted.ResultRef) {
		t.Fatalf("replaced admission: %v %v", original, e)
	}
}
