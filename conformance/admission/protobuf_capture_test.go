package admission_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G4、G10、完成-4
func TestCaptureProtobufMainline(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	var samples []protobuf.Sample
	add := func(name string, m proto.Message, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if m == nil || !m.ProtoReflect().IsValid() {
			t.Fatalf("%s absent", name)
		}
		samples = append(samples, protobuf.Sample{Name: name, Message: proto.Clone(m)})
	}
	add("start-command", start, nil)
	add("admission", a, nil)
	handoff, err := f.h.Tasks.QueryHandoff(f.ctx, f.caller, a.Ref)
	add("operation-handoff", handoff, err)
	planning, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	add("planning-before-send", planning, err)
	grant, err := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	add("grant", grant, err)
	use, err := f.h.Grants.QueryUse(f.ctx, f.caller, a.OperationId)
	add("grant-use", use, err)
	credential, err := f.h.Grants.QueryCredential(f.ctx, f.caller, start.CredentialRef)
	add("exit-credential", credential, err)
	prepare := &v1.PrepareExecutionCommand{Header: ledgerHeader("prepare"), OperationId: a.OperationId, ProcessInstance: "worker", Claim: start.Claim}
	prepared, err := f.h.Ledger.Prepare(f.ctx, f.caller, prepare)
	accepted(t, prepared, err)
	add("prepare-command", prepare, nil)
	issue := &v1.IssueExitCredentialCommand{Header: header("credential"), AdmissionRef: a.Ref, Binding: start.Binding, ExpiresAtUnixMs: credential.ExpiresAtUnixMs}
	issued, err := f.h.Grants.IssueCredential(f.ctx, f.caller, issue)
	accepted(t, issued, err)
	add("issue-exit-command", issue, nil)
	accept := &v1.AcceptOperationCommand{Header: &v1.CommandHeader{Identity: a.HandoffIdentity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, Admission: a}
	acceptedOperation, err := f.h.Ledger.Accept(f.ctx, &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}, accept)
	accepted(t, acceptedOperation, err)
	add("accept-operation-command", accept, nil)
	before, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	add("execution-before-send", before, err)
	reservation, err := f.h.Budget.QueryReservation(f.ctx, f.caller, a.BudgetBasis.ReservationRef)
	add("reservation", reservation, err)
	receipt, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	add("send-receipt", receipt, nil)
	execution, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	add("execution-after-send", execution, err)
	operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	add("operation-after-send", operation, err)
	observation, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, execution.Send.ObservationRef)
	add("observation", observation, err)
	dispatchHeader := ledgerHeader("dispatch:" + execution.Send.Ref.Name.LocalId)
	dispatchHeader.Identity.IssuerId = "egress"
	dispatch := &v1.DispatchCommand{Header: dispatchHeader, OperationId: a.OperationId, StartReceipt: execution.Send.StartReceipt, Claim: start.Claim}
	dispatchReceipt, fresh, err := f.h.Ledger.RecordDispatch(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, dispatch)
	accepted(t, dispatchReceipt, err)
	if fresh {
		t.Fatal("replayed dispatch was fresh")
	}
	add("dispatch-command", dispatch, nil)
	observationBody, err := f.h.Content.Read(f.ctx, f.caller, observation.BodyRef)
	if err != nil {
		t.Fatal(err)
	}
	raw := proto.Clone(observation).(*v1.RawObservation)
	raw.BodyRef = nil
	observeHeader := header("observe:" + raw.Ref.Name.LocalId)
	observeHeader.Identity.IssuerId = "egress-io"
	observeHeader.Identity.TargetDomainId = f.parameters.Name.AuthorityDomainId
	register := &v1.RegisterObservationCommand{Header: observeHeader, Observation: raw, Body: observationBody.RawBody}
	registered, err := f.h.Content.RegisterObservation(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress-io"}, register)
	accepted(t, registered, err)
	add("register-observation-command", register, nil)
	acceptHeader := ledgerHeader("observe:" + raw.Ref.Name.LocalId)
	acceptHeader.Identity.IssuerId = "content-observation"
	acceptObservation := &v1.AcceptObservationCommand{Header: acceptHeader, Observation: observation}
	observed, err := f.h.Ledger.AcceptObservation(f.ctx, &v1.Caller{UserId: "u", IssuerId: "content-observation"}, acceptObservation)
	accepted(t, observed, err)
	add("accept-observation-command", acceptObservation, nil)
	interpret := &v1.InterpretObservationCommand{Header: ledgerHeader("capture-interpret"), ObservationRef: observation.Ref}
	interpreted, err := f.h.Ledger.InterpretObservation(f.ctx, f.caller, interpret)
	accepted(t, interpreted, err)
	add("interpret-observation-command", interpret, nil)
	interpretation, err := f.h.Ledger.QueryInterpretation(f.ctx, f.caller, interpreted.ResultRef)
	add("effect-interpretation", interpretation, err)
	reports, err := f.h.Ledger.QueryReports(f.ctx, f.caller, execution.Send.ObservationRef)
	add("observation-reports", reports, err)
	usage, err := f.h.Budget.QueryUsage(f.ctx, f.caller, reports.Usage.Usage.Ref)
	add("usage", usage, err)
	event, err := f.h.Trace.QueryEvent(f.ctx, f.caller, reports.Trace.Event.Ref)
	add("trace-event", event, err)
	credentialUse, err := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, start.CredentialRef)
	add("credential-use", credentialUse, err)
	billing, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, execution.Send.Ref)
	add("billing-source", billing, err)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	begin := &v1.BeginCompletionCommand{Header: header("capture-complete"), TaskId: f.task.Name, ProposalRef: p}
	add("begin-completion-command", begin, nil)
	receipt, err = f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, receipt, err)
	if err = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	add("result", result, err)
	if result.Outcome != "SUCCEEDED" {
		t.Fatal(result)
	}
	round, err := f.h.Tasks.QueryVerification(f.ctx, f.caller, result.VerificationRef)
	add("verification", round, err)
	for i, ref := range round.ClosureIntentRefs {
		intent, err := f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, ref)
		add(fmt.Sprintf("completion-intent-%d", i), intent, err)
		seal, err := f.h.Ledger.QueryCompletionSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
		add(fmt.Sprintf("completion-seal-%d", i), seal, err)
	}
	recheck := &v1.RecheckCompletionCommand{Header: header("capture-recheck"), VerificationRef: round.Ref}
	rechecked, err := f.h.Tasks.RecheckCompletion(f.ctx, f.caller, recheck)
	add("closed-recheck-rejection", rechecked, err)
	if rechecked.GetError().GetCode() != "STALE_VERIFICATION" {
		t.Fatal(rechecked)
	}
	add("recheck-completion-command", recheck, nil)
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	add("closed-task", task, err)
	planning, err = f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	add("planning-after-completion", planning, err)
	userBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	add("user-budget", userBudget, err)
	taskBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	add("task-budget", taskBudget, err)
	q, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	add("goal-receipt-query", q, err)
	session, err := f.h.Sessions.QuerySession(f.ctx, f.caller, q.Receipt.SessionRef.Name)
	add("session", session, err)
	goal := &v1.SubmitGoalCommand{Identity: header("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create a record"}
	replay, err := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
	accepted(t, replay, err)
	add("goal-command", goal, nil)
	largeGoal := proto.Clone(goal).(*v1.SubmitGoalCommand)
	largeGoal.Identity.CommandId = "capture-large-goal"
	largeGoal.Goal = strings.Repeat("synthetic input ", 4096)
	largeReceipt, err := f.h.Sessions.SubmitGoal(f.ctx, f.caller, largeGoal)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Sessions.ProcessPending(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	largeQuery, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, largeReceipt.Identity)
	if err != nil {
		t.Fatal(err)
	}
	accepted(t, largeQuery.Receipt, nil)
	add("large-goal-command", largeGoal, nil)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("requests=%d effects=%d", len(requests), len(effects))
	}
	captureConfirmations(t, &samples)
	captureRevocationRelease(t, &samples)
	captureBilling(t, &samples)
	captureInputsAndPlanning(t, &samples)
	capturePhysicalIO(t, &samples)
	captureContentGovernance(t, &samples)
	captureModelCalls(t, &samples)
	captureReconciliation(t, &samples)
	captureResendHistory(t, &samples)
	captureTraceHandoff(t, &samples)
	captureManagedFiles(t, &samples)
	captureCancellation(t, &samples)
	captureTaskClosing(t, &samples)
	captureRejectedContinuation(t, &samples)
	captureDefaultReasonerAction(t, &samples)
	captureDefaultReasonerQuestion(t, &samples)
	captureDefaultReasonerRequirements(t, &samples)
	captureConditionConfirmation(t, &samples)
	captureDefaultNegativeVerdict(t, &samples)
	captureReasonerQuestionDriver(t, &samples)
	captureReasonerApprovedActionDriver(t, &samples)
	captureNativeAPI(t, &samples)
	captureDefaultModelAPIDriver(t, &samples)
	capturePendingGoal(t, &samples)
	captureLocalMetrics(t, &samples)
	if output := os.Getenv("LERNA_PROTOBUF_CAPTURE"); output != "" {
		if err := protobuf.Save(output, samples); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("captured %d public objects", len(samples))
}
