package admission_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G11、完成-4
func TestCLIFailedClosingHasIndependentBasisAndImmutableResult(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	cmd := &v1.BeginTaskCloseCommand{Header: header("failed-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "FAILED", CloseReason: "UNABLE_TO_COMPLETE"}
	body, e := protojson.Marshal(cmd)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "close.json")
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Tasks: f.h.Tasks, Sessions: f.h.Sessions, Durable: f.h.Durable, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"close-task", "--json", path}, &out); e != nil {
		t.Fatal(e)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, &out); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "FAILED" || result.VerificationRef != nil || result.TaskClosingRef == nil || result.CloseReason != "UNABLE_TO_COMPLETE" {
		t.Fatalf("non-success invented verification %v %v", result, e)
	}
	fixed, e := proto.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	later, e := proto.Marshal(again)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatalf("Result changed after restart %v %v", again, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("closing sent target action")
	}
}

// 规则：G3、G4、G10、G11、R7、开始-2、开始-5
func TestFailedClosingCannotLoseOrForgeEndpointResponsibility(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, _ := prepareStart(t, f)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header("guarded-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "FAILED", CloseReason: "USER_STOPPED"})
	accepted(t, r, e)
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result != nil || len(view.PendingClosureRefs) != 1 || len(view.SettlementFollowups) != 1 {
		t.Fatalf("unacknowledged close hidden %v %v", view, e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("task-close-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_TASK_CLOSURE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "close-worker"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatalf("responsibility missing %v", claim)
	}
	job := claim.Jobs[0]
	for _, action := range []string{"CONTROL", "PROGRESS"} {
		next := "CLOSED"
		if action == "PROGRESS" {
			next = "COMPLETED"
		}
		r, e = f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("task-close-abandon-" + action).Identity, ContractVersion: 1, Action: action, Module: "tasks", JobRef: job.Ref, ClaimEpoch: job.ClaimEpoch, ProcessInstance: job.ProcessInstance, NextState: next})
		if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
			t.Fatalf("generic job discarded closure %v %v", r, e)
		}
	}
	for _, kind := range []string{"version", "scope", "owner"} {
		wrong := proto.Clone(job).(*v1.Job)
		switch kind {
		case "version":
			wrong.ContractVersion = 2
		case "scope":
			wrong.SpecificationRef = &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"}
		case "owner":
			wrong.ExecutorEndpointId = "replacement-endpoint"
		}
		if e = f.h.Tasks.ProcessTaskClosureClaim(f.ctx, wrong); e == nil {
			t.Fatalf("forged %s claim acknowledged closure", kind)
		}
	}
	if e = f.h.Tasks.ProcessTaskClosureClaim(f.ctx, job); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Reserved != 0 || budget.Settled != 0 {
		t.Fatalf("proved unused hold not released %v %v", budget, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" {
		t.Fatalf("old original escaped %v %v", op, e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("closing sent target")
	}
}

// 规则：G1、G2、G3、G10、G11、R7、完成-6
func TestFailedClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill(t *testing.T) {
	testClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill(t, "FAILED")
}

// 规则：G1、G2、G3、G4、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill(t *testing.T) {
	testClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill(t, "CANCELLED")
}

func testClosingRetainsUnknownAndAcceptsOriginalLateEffectAndBill(t *testing.T, outcome string) {
	target := simulator.NewBillingTarget(120)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	late := performWithoutObservation(t, f, a, start)
	r, e := beginNonSuccessClose(t, f, "unknown-failed-close", outcome, "UNABLE_TO_COMPLETE")
	accepted(t, r, e)
	mutate, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	resume, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("resume-closing"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "CONTROL", Control: "RESUME", ExpectedControlGeneration: mutate.ControlGeneration})
	if e != nil || resume.GetError().GetCode() != "TASK_CLOSING" {
		t.Fatalf("closing target reopened %v %v", resume, e)
	}
	if e = processNonSuccessClosings(f, outcome); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != outcome || len(result.UnknownOperationRefs) != 1 || len(result.ExecutionFollowupRefs) != 1 || result.UsageSnapshot.Reserved != 30 {
		t.Fatalf("unknown responsibility discarded %v %v", result, e)
	}
	fixed, e := proto.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || op.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("unknown converted or reassigned %v %v", op, e)
	}
	jobs, e := f.h.Ledger.QueryJobs(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	retained := false
	for _, j := range jobs {
		if j.JobType == "RECONCILE_UNRESOLVED_OPERATION" && j.State == "WAITING" {
			retained = true
		}
	}
	if !retained {
		t.Fatalf("only a descriptive unknown list survived %v", jobs)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 1 || len(view.FollowupJobs) != 2 {
		t.Fatalf("owner responsibilities hidden %v %v", view, e)
	}
	for i, j := range view.FollowupJobs {
		work := f.h.Durable
		identity := header("abandon-followup-" + j.JobType).Identity
		if j.Module == "ledger" {
			work = f.h.LedgerWork
			identity.TargetDomainId = "d/ledger"
		}
		for _, action := range []string{"CONTROL", "PROGRESS"} {
			id := proto.Clone(identity).(*v1.CommandIdentity)
			id.CommandId += action
			next := "CLOSED"
			if action == "PROGRESS" {
				next = "COMPLETED"
			}
			guard, e := work.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: id, ContractVersion: 1, Action: action, Module: j.Module, JobRef: j.Ref, ClaimEpoch: j.ClaimEpoch, ProcessInstance: j.ProcessInstance, NextState: next})
			if e != nil || guard.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
				t.Fatalf("generic followup %d discarded responsibility %v %v", i, guard, e)
			}
		}
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	evidence := stageBill(t, f, target.Bills()[0], "failed-late-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("failed-import"), SendRef: op.Execution.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	still, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || still.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("bill decided effect %v %v", still, e)
	}
	savePhysicalObservation(t, f, late)
	current, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || current.Effect.Outcome != "APPLIED" || current.ExecutorEndpointId != a.ExecutorEndpointId || !proto.Equal(current.Execution.Attempt.Ref.Name, op.Execution.Attempt.Ref.Name) {
		t.Fatalf("original late effect missing %v %v", current, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || b.Settled != 120 || b.Reserved != 0 || b.Deficit != 40 {
		t.Fatalf("late original cost %v %v", b, e)
	}
	again, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	later, e := proto.Marshal(again)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatalf("late facts rewrote Result %v %v", again, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	for _, j := range view.FollowupJobs {
		if j.State != "COMPLETED" {
			t.Fatalf("known late facts stranded actual owner responsibility %v", j)
		}
	}
	if len(view.Result.UnknownOperationRefs) != 1 || view.Operations[0].Effect.Outcome != "APPLIED" {
		t.Fatalf("fixed and current facts conflated %v", view)
	}
	assertClosingTraceSources(t, f, view)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("closing resent: calls=%d effects=%d bills=%d", len(requests), len(effects), len(target.Bills()))
	}
}

// 规则：G2、G3、G11、完成-4、完成-5
func TestFailedClosingRecordsReliableUnsatisfiedRequirement(t *testing.T) {
	target := simulator.New("idempotent")
	target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header("reliable-failure"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "FAILED", CloseReason: "UNABLE_TO_COMPLETE"})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || len(result.Conditions) != 1 || result.Conditions[0].Conclusion != "UNSATISFIED" || result.Conditions[0].Gap != "REQUIRED_ACTION_NOT_APPLIED" || len(result.Conditions[0].EvidenceRefs) == 0 || !proto.Equal(result.Conditions[0].OperationRef.Name, a.OperationId) {
		t.Fatalf("reliable failure converted to missing evidence %v %v", result, e)
	}
	if result.Outcome != "FAILED" || result.VerificationRef != nil {
		t.Fatal("failure invented successful verification")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 0 {
		t.Fatal("failure performed a substitute action")
	}
}

// 规则：G1、G3、G4、G10、G11、开始-2、开始-5、完成-6
func TestFailedClosingKeepsAllHistoricalSendAndSettlementResponsibilities(t *testing.T) {
	testClosingKeepsAllHistoricalSendAndSettlementResponsibilities(t, "FAILED")
}

// 规则：G1、G2、G3、G4、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingKeepsAllHistoricalSendAndSettlementResponsibilities(t *testing.T) {
	testClosingKeepsAllHistoricalSendAndSettlementResponsibilities(t, "CANCELLED")
}

func testClosingKeepsAllHistoricalSendAndSettlementResponsibilities(t *testing.T, outcome string) {
	target := simulator.NewBillingTarget(12)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	late := performWithoutObservation(t, f, a, first)
	old, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("historical-close-resend"), OperationId: a.OperationId, PreviousSendRef: old.Send.Ref, Claim: first.Claim})
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	second := resendStart(t, f, a, first, x, first.Claim)
	r, e = f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	accepted(t, r, e)
	p4 := proto.Clone(r).(*v1.CommandReceipt)
	r, e = beginNonSuccessClose(t, f, "historical-failed-close", outcome, "USER_STOPPED")
	accepted(t, r, e)
	if e = processNonSuccessClosings(f, outcome); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || len(view.ExecutionFollowups) != 1 || len(view.ExecutionFollowups[0].SendRefs) != 2 || len(view.SettlementFollowups[0].ReservationRefs) != 2 || len(view.SettlementFollowups[0].BillingSourceRefs) != 2 {
		t.Fatalf("historical responsibility lost %v %v", view, e)
	}
	followup := view.ExecutionFollowups[0]
	if !proto.Equal(followup.AttemptRef.Name, old.Attempt.Ref.Name) || !proto.Equal(followup.SendRefs[0].Name, x.Send.Ref.Name) || !proto.Equal(followup.SendRefs[1].Name, old.Send.Ref.Name) {
		t.Fatal("historical identity reassigned")
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Reserved != 30 || budget.Settled != 0 {
		t.Fatalf("old unknown fee released %v %v", budget, e)
	}
	fixed, e := proto.Marshal(view.Result)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	q, e := f.h.Tasks.QueryStartReceipt(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second.Header.Identity)
	if e != nil || !proto.Equal(q.Receipt, p4) {
		t.Fatalf("old P4 receipt replaced %v %v", q, e)
	}
	use, e := f.h.Grants.QueryCredentialUse(f.ctx, f.caller, second.CredentialRef)
	if e != nil || use == nil {
		t.Fatal("consumed P4 confirmation disappeared")
	}
	_, _ = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	evidence := stageBill(t, f, target.Bills()[0], "historical-original-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("historical-import"), SendRef: old.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	savePhysicalObservation(t, f, late)
	if e = f.h.Ledger.ProcessExecutionFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	for _, job := range view.FollowupJobs {
		if job.State != "COMPLETED" {
			t.Fatalf("old owner stranded terminal facts %v", job)
		}
	}
	later, e := proto.Marshal(view.Result)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatal("Result rewritten by historical receipt")
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("historical close resent target: %d/%d/%d", len(requests), len(effects), len(target.Bills()))
	}
}

// 规则：G2、G3、G4、G11、完成-4、完成-7
func TestFailedClosingFencesPendingInputProcessing(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, _ = prepareStart(t, f)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("close-pending-modify"), SessionId: goal.Receipt.SessionRef.Name, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: f.parameters, ExpectedRequirementsVersion: task.RequirementsVersion, ExpectedInputVersion: task.InputVersion})
	accepted(t, r, e)
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header("close-before-input-processing"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "FAILED", CloseReason: "USER_STOPPED"})
	accepted(t, r, e)
	closing := r.ResultRef
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.ProcessInput(f.ctx, f.caller, &v1.ProcessInputCommand{Header: header("late-close-input-processing"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.BoundInputVersion + 1, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"})
	if e != nil || r.GetError().GetCode() != "TASK_CLOSING" {
		t.Fatalf("late input changed permanent close basis %v %v", r, e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || !proto.Equal(view.Result.TaskClosingRef, closing) || !proto.Equal(view.Result.RequirementsRef, view.Closing.RequirementsRef) {
		t.Fatalf("permanent close stranded %v %v", view, e)
	}
	if view.Result.Conditions[0].Conclusion != "UNKNOWN" || view.Result.Conditions[0].Gap != "REQUIREMENTS_STALE_INPUT" {
		t.Fatalf("old input requirements asserted as current %v", view.Result.Conditions)
	}
	if f.calls.Load() != 0 {
		t.Fatal("closed old action sent")
	}
}

// 规则：G3、G4、G10、G11、R7、开始-5、完成-6
func TestFailedClosingWaitsForActualInFlightTargetUse(t *testing.T) {
	testClosingWaitsForActualInFlightTargetUse(t, "FAILED")
}

// 规则：G1、G2、G3、G4、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingWaitsForActualInFlightTargetUse(t *testing.T) {
	testClosingWaitsForActualInFlightTargetUse(t, "CANCELLED")
}

func testClosingWaitsForActualInFlightTargetUse(t *testing.T, outcome string) {
	target := simulator.NewBillingTarget(25)
	entered, release := make(chan struct{}), make(chan struct{})
	target.Target.SetWriteResponseGate(entered, release)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	sent := make(chan error, 1)
	go func() {
		_, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		sent <- e
	}()
	<-entered
	r, e := beginNonSuccessClose(t, f, "in-flight-failure", outcome, "USER_STOPPED")
	if e != nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		close(release)
		<-sent
		t.Fatalf("close source %v %v", r, e)
	}
	closed := make(chan error, 1)
	go func() { closed <- processNonSuccessClosings(f, outcome) }()
	select {
	case e = <-closed:
		close(release)
		<-sent
		t.Fatalf("endpoint acknowledged during original physical use: %v", e)
	case <-time.After(30 * time.Millisecond):
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result != nil || len(view.PendingClosureRefs) != 1 {
		close(release)
		<-sent
		<-closed
		t.Fatalf("source control treated as actual stop %v %v", view, e)
	}
	close(release)
	if e = <-sent; e != nil {
		<-closed
		t.Fatal(e)
	}
	if e = <-closed; e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || view.Result.Outcome != outcome || len(view.PendingClosureRefs) != 0 || view.Operations[0].Dispatch != "SEALED" || !proto.Equal(view.Operations[0].AdmissionRef, a.Ref) {
		t.Fatalf("original actual use stranded %v %v", view, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatal("closing duplicated target operation")
	}
}

// 规则：G3、G10、G11、完成-6
func TestFailedClosingRetainsOriginalSettlementResponsibilityAfterConflict(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header("conflict-failed-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "FAILED", CloseReason: "USER_STOPPED"})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || len(view.FollowupJobs) != 1 || view.FollowupJobs[0].State != "COMPLETED" {
		t.Fatalf("original settlement not complete %v %v", view, e)
	}
	fixed, e := proto.Marshal(view.Result)
	if e != nil {
		t.Fatal(e)
	}
	// 篡改负对照：供应商独立原账单仍是 25；相同原收费身份的不同金额必须形成冲突。
	invalid := target.Bills()[0]
	invalid.Amount++
	evidence := stageBill(t, f, invalid, "conflicting-closed-statement")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("conflicting-closed-import"), SendRef: view.Operations[0].Execution.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.FollowupJobs[0].State != "WAITING" || view.FollowupJobs[0].WaitingReason == "" || !proto.Equal(view.SettlementFollowups[0].OperationId, a.OperationId) {
		t.Fatalf("conflict lost original settlement responsibility %v %v", view, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Settled != 25 || !budget.BillingBlocked {
		t.Fatalf("tampered negative changed original billed amount %v %v", budget, e)
	}
	later, e := proto.Marshal(view.Result)
	if e != nil || !bytes.Equal(fixed, later) {
		t.Fatal("conflict rewrote Result")
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || target.Bills()[0].Amount != 25 {
		t.Fatal("conflict replaced original target facts")
	}
}
