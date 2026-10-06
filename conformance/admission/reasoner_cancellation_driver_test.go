package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G9、G10、G11、R7、开始-2
func TestProductionDriverCancellationPreservesModelAndSealsUnsentAction(t *testing.T) {
	f, provider, target, configuration, binary := cancellationDriverFixture(t)
	configured := new(v1.CommandReceipt)
	if e := protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "configure-reasoner", "--json", configuration), configured); e != nil {
		t.Fatal(e)
	}
	accepted(t, configured, nil)
	if provider.Calls() != 0 || len(provider.Bills()) != 0 || f.calls.Load() != 0 {
		t.Fatal("configuration sent business I/O")
	}
	var driver *v1.ReasonerDriver
	for range 3 {
		var e error
		driver, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
	}
	if driver.AdmissionRef == nil || driver.OutcomeRef == nil || driver.RequestRef == nil || !proto.Equal(driver.Ref.Name, configured.ResultRef.Name) {
		t.Fatalf("not actual admitted action frontier: %v", driver)
	}
	originalDriver := proto.Clone(driver).(*v1.ReasonerDriver)
	action, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, driver.AdmissionRef)
	if e != nil || action == nil {
		t.Fatalf("original driver admission: %v %v", action, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, driver.RequestRef, 0)
	if e != nil || call == nil || call.Result.GetStatus() != "COMPLETED" || call.InputRef == nil || call.AdmissionRef == nil {
		t.Fatalf("original model position: %v %v", call, e)
	}
	originalCall := proto.Clone(call).(*v1.ModelCall)
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
	if e != nil || !proto.Equal(outcome.ModelCallRef, call.Ref) || !proto.Equal(action.Origin, outcome.ProposalRef) {
		t.Fatalf("original outcome and action: %v %v", outcome, e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
	if e != nil || len(request.OutcomeRefs) != 1 || len(request.ModelOperationRefs) != 1 {
		t.Fatalf("original request: %v %v", request, e)
	}
	modelAdmission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, call.AdmissionRef)
	if e != nil || !proto.Equal(modelAdmission.Origin, call.Ref) || modelAdmission.ModelDescriptorDigest != call.DescriptorDigest {
		t.Fatalf("original model admission: %v %v", modelAdmission, e)
	}
	modelOperation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, modelAdmission.OperationId)
	if e != nil || modelOperation.Execution == nil || modelOperation.Effect.Outcome != "APPLIED" {
		t.Fatalf("original model send: %v %v", modelOperation, e)
	}
	if op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, action.OperationId); e != nil || op != nil {
		t.Fatalf("action already crossed original handoff: %v %v", op, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Reserved != 30 || budget.Settled != 7 || provider.Calls() != 1 || len(provider.Bills()) != 1 || f.calls.Load() != 0 {
		t.Fatalf("frontier counts and fees: %v %v", budget, e)
	}
	control, receipt := submitDriverCancellation(t, f, "driver-cancel-before-target-io")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || len(scope.AdmissionRefs) != 2 || !slices.ContainsFunc(scope.AdmissionRefs, func(r *v1.Ref) bool { return proto.Equal(r, action.Ref) }) || !slices.ContainsFunc(scope.AdmissionRefs, func(r *v1.Ref) bool { return proto.Equal(r, call.AdmissionRef) }) || !proto.Equal(scope.ControlIdentity, control.Header.Identity) {
		t.Fatalf("complete original cancellation scope: %v %v", scope, e)
	}
	pending := driverCancellationView(t, f)
	if len(pending.PendingClosureRefs) != 2 || len(pending.ClosureIntents) != 2 || len(pending.ClosureSeals) != 0 || pending.Task.ResultRef != nil || pending.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING {
		t.Fatalf("source ACK claimed endpoint or final closure: %v", pending)
	}
	// 先由公开原负责方交付封闭，准确观察尚未到达 P2 的永久标记。
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	tombstone := driverCancellationView(t, f)
	if len(tombstone.PendingClosureRefs) != 0 || len(tombstone.ClosureSeals) != 2 || len(tombstone.AwaitingOperationAdmissionRefs) != 1 || !proto.Equal(tombstone.AwaitingOperationAdmissionRefs[0], action.Ref) || len(tombstone.Operations) != 1 {
		t.Fatalf("pre-P2 tombstone qualification: %v", tombstone)
	}
	advance := writeReasonerCLIJSON(t, "cancel-advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		state := new(v1.ReasonerDriver)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "advance-task", "--json", advance), state); e != nil || state.State != "WAITING" || state.WaitingReason != "TASK_NOT_ACTIVE" || !proto.Equal(state.RequestRef, originalDriver.RequestRef) || !proto.Equal(state.OutcomeRef, originalDriver.OutcomeRef) || !proto.Equal(state.AdmissionRef, action.Ref) {
			t.Fatalf("cancelled startup/advance changed original responsibility: %v %v", state, e)
		}
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	sealed := driverCancellationView(t, f)
	if !proto.Equal(sealed.Cancellation, scope) || len(sealed.PendingClosureRefs) != 0 || len(sealed.ClosureSeals) != 2 || len(sealed.AwaitingOperationAdmissionRefs) != 0 || len(sealed.Operations) != 2 || sealed.Task.ResultRef != nil || sealed.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
		t.Fatalf("endpoint ACK lost limited original facts: %v", sealed)
	}
	var targetSeal *v1.CancellationSeal
	for _, intent := range sealed.ClosureIntents {
		if intent.RecipientReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("original endpoint ACK missing: %v", intent)
		}
		proof, e := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
		if e != nil || proof == nil || !proto.Equal(proof.IntentRef, intent.Ref) || !proto.Equal(proof.CancellationRef, scope.Ref) || !proto.Equal(proof.AdmissionRef, intent.Command.AdmissionRef) {
			t.Fatalf("original endpoint proof: %v %v", proof, e)
		}
		job, e := f.h.Durable.QueryJob(f.ctx, f.caller, intent.JobRef.Name)
		if e != nil || job.State != "COMPLETED" || !proto.Equal(job.SpecificationRef, intent.Ref) {
			t.Fatalf("original closure responsibility: %v %v", job, e)
		}
		if proto.Equal(proof.AdmissionRef, action.Ref) {
			targetSeal = proof
			if !proof.NoSendProven || proof.PhysicalSendWasPossible || proof.OperationRef != nil {
				t.Fatalf("unsent action proof invented operation or send: %v", proof)
			}
		} else if !proto.Equal(proof.AdmissionRef, call.AdmissionRef) || proof.NoSendProven || !proof.PhysicalSendWasPossible || !proto.Equal(proof.OperationRef.Name, modelAdmission.OperationId) {
			t.Fatalf("already sent model claimed no send: %v", proof)
		}
	}
	if targetSeal == nil {
		t.Fatal("original target tombstone missing")
	}
	// CLI 启动已恢复原交接与预算；测试不手动替代这些生产恢复阶段。
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, action.OperationId)
	if e != nil || !proto.Equal(op.AdmissionRef, action.Ref) || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Execution != nil || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("original late P2 ran target: %v %v", op, e)
	}
	release, e := f.h.Budget.QueryReservationRelease(f.ctx, f.caller, action.BudgetBasis.ReservationRef)
	if e != nil || release == nil || release.Released != 30 || !proto.Equal(release.ClosureRef, targetSeal.Ref) {
		t.Fatalf("unused original action reserve: %v %v", release, e)
	}
	for range 2 {
		replay, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, control)
		accepted(t, replay, e)
		if !proto.Equal(replay, receipt) {
			t.Fatal("original control decision changed")
		}
		currentCall, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, originalDriver.RequestRef, 0)
		if e != nil || !proto.Equal(currentCall, originalCall) {
			t.Fatalf("original model call/input/result changed: %v %v", currentCall, e)
		}
		currentOutcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, originalDriver.OutcomeRef)
		if e != nil || !proto.Equal(currentOutcome, outcome) {
			t.Fatalf("original model outcome changed: %v %v", currentOutcome, e)
		}
		currentRequest, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, originalDriver.RequestRef)
		if e != nil || !proto.Equal(currentRequest.SnapshotRef, request.SnapshotRef) || len(currentRequest.OutcomeRefs) != 1 || len(currentRequest.ModelOperationRefs) != 1 {
			t.Fatalf("original request identity or position expanded: %v %v", currentRequest, e)
		}
		if next, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, originalDriver.RequestRef, 1); e != nil || next != nil {
			t.Fatalf("extra model position: %v %v", next, e)
		}
		modelNow, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, modelAdmission.OperationId)
		if e != nil || !proto.Equal(modelNow.Execution, modelOperation.Execution) || modelNow.Effect.Outcome != "APPLIED" {
			t.Fatalf("original model send facts changed: %v %v", modelNow, e)
		}
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, modelOperation.Execution.Send.Ref)
		if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 7 {
			t.Fatalf("model fee erased by cancellation: %v %v", source, e)
		}
		budget, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
		requests, effects := target.Target.Snapshot()
		if e != nil || budget.Reserved != 0 || budget.Settled != 7 || provider.Calls() != 1 || len(provider.Bills()) != 1 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 || f.calls.Load() != 0 {
			t.Fatalf("cancellation/replay/restart sent or changed fees: %v %v", budget, e)
		}
		view := driverCancellationView(t, f)
		if len(view.Operations) != 2 || len(view.AwaitingOperationAdmissionRefs) != 0 || len(view.UnresolvedEffectOperationRefs) != 0 || view.Task.ResultRef != nil || view.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING {
			t.Fatalf("cancelled driver fabricated final Result or effect: %v", view)
		}
		if e = f.h.Close(); e != nil {
			t.Fatal(e)
		}
		state := new(v1.ReasonerDriver)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "advance-task", "--json", advance), state); e != nil || state.WaitingReason != "TASK_NOT_ACTIVE" || !proto.Equal(state.AdmissionRef, action.Ref) {
			t.Fatalf("restart continued cancelled driver: %v %v", state, e)
		}
		f.h, e = assembly.Open(f.path, "u", "d")
		if e != nil {
			t.Fatal(e)
		}
	}
	// 原 CANCEL 的端点 ACK 仅封闭入口；当前受信关闭命令另行固定最终 Result。
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	closeCommand := &v1.BeginTaskCloseCommand{Header: header("driver-cancel-final-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
	closePath := writeReasonerCLIJSON(t, "cancel-close.json", closeCommand)
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	closeReceipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "close-task", "--json", closePath), closeReceipt); e != nil {
		t.Fatal(e)
	}
	accepted(t, closeReceipt, nil)
	completed := new(v1.ReasonerDriver)
	if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "advance-task", "--json", advance), completed); e != nil || completed.State != "COMPLETED" || completed.WaitingReason != "" || !proto.Equal(completed.RequestRef, originalDriver.RequestRef) || !proto.Equal(completed.OutcomeRef, originalDriver.OutcomeRef) || !proto.Equal(completed.AdmissionRef, action.Ref) {
		t.Fatalf("real trusted closing did not finish original driver: %v %v", completed, e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	closing, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || closing.Result == nil || closing.Result.Outcome != "CANCELLED" || closing.Result.CloseReason != "USER_STOPPED" || closing.Result.VerificationRef != nil || !proto.Equal(closing.Result.TaskClosingRef, closeReceipt.ResultRef) || closing.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || !proto.Equal(closing.Closing.CancellationRef, scope.Ref) || !proto.Equal(closing.Closing.RequirementsRef, planning.Requirements.Ref) || closing.Closing.InputVersion != task.InputVersion || closing.Closing.ControlGeneration != task.ControlGeneration+1 || len(closing.Closing.AdmissionRefs) != 2 || len(closing.ClosureSeals) != 2 || len(closing.PendingClosureRefs) != 0 || len(closing.Result.OperationRefs) != 2 || len(closing.SettlementFollowups) != 2 {
		t.Fatalf("actual fixed CANCELLED basis/ACK: %v %v", closing, e)
	}
	for _, intent := range closing.ClosureIntents {
		if intent.RecipientReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("task closing endpoint ACK missing: %v", intent)
		}
		seal, err := f.h.Ledger.QueryTaskClosureSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
		if err != nil || seal == nil || !proto.Equal(seal.TaskClosingRef, closeReceipt.ResultRef) || !proto.Equal(seal.IntentRef, intent.Ref) || seal.OperationRef == nil {
			t.Fatalf("actual task closing seal: %v %v", seal, err)
		}
		if proto.Equal(seal.AdmissionRef, action.Ref) && (!seal.NoSendProven || seal.PhysicalSendWasPossible || !proto.Equal(seal.OperationRef.Name, action.OperationId)) {
			t.Fatalf("closing invented target send: %v", seal)
		}
		if proto.Equal(seal.AdmissionRef, call.AdmissionRef) && (seal.NoSendProven || !seal.PhysicalSendWasPossible || !proto.Equal(seal.OperationRef.Name, modelAdmission.OperationId)) {
			t.Fatalf("closing erased original model send: %v", seal)
		}
		job, err := f.h.Durable.QueryJob(f.ctx, f.caller, intent.JobRef.Name)
		if err != nil || job.State != "COMPLETED" || !proto.Equal(job.SpecificationRef, intent.Ref) {
			t.Fatalf("task closing owner responsibility: %v %v", job, err)
		}
	}
	modelSource, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, modelOperation.Execution.Send.Ref)
	if e != nil || modelSource.Status != "SETTLED" || modelSource.Amount == nil || *modelSource.Amount != 7 {
		t.Fatalf("CLOSED original model source: %v %v", modelSource, e)
	}
	for _, followup := range closing.SettlementFollowups {
		original := modelAdmission
		if proto.Equal(followup.AdmissionRef, action.Ref) {
			original = action
		}
		if !proto.Equal(followup.AdmissionRef, original.Ref) || !proto.Equal(followup.OperationId, original.OperationId) || !proto.Equal(followup.TaskClosingRef, closeReceipt.ResultRef) || !slices.ContainsFunc(followup.ReservationRefs, func(ref *v1.Ref) bool { return proto.Equal(ref.Name, original.BudgetBasis.ReservationRef.Name) }) {
			t.Fatalf("closing lost original settlement scope: %v", followup)
		}
		if original == modelAdmission && (len(followup.BillingSourceRefs) != 1 || !proto.Equal(followup.BillingSourceRefs[0], modelSource.Ref)) || original == action && len(followup.BillingSourceRefs) != 0 {
			t.Fatalf("closing invented or erased supplier billing source: %v", followup)
		}
		job, err := f.h.Durable.QueryJob(f.ctx, f.caller, followup.JobRef.Name)
		if err != nil || job.State != "COMPLETED" || !proto.Equal(job.SpecificationRef, followup.Ref) {
			t.Fatalf("original settlement owner job: %v %v", job, err)
		}
	}
	unchangedRelease, e := f.h.Budget.QueryReservationRelease(f.ctx, f.caller, action.BudgetBasis.ReservationRef)
	if e != nil || !proto.Equal(unchangedRelease, release) {
		t.Fatalf("task closing replaced original cancellation reserve release: %v %v", unchangedRelease, e)
	}
	fixed, e := proto.Marshal(closing.Result)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		originalControl, err := f.h.Sessions.SubmitInput(f.ctx, f.caller, control)
		accepted(t, originalControl, err)
		if !proto.Equal(originalControl, receipt) {
			t.Fatal("CLOSED original CANCEL receipt changed")
		}
		currentCall, err := f.h.Tasks.QueryModelCall(f.ctx, f.caller, originalDriver.RequestRef, 0)
		if err != nil || !proto.Equal(currentCall, originalCall) {
			t.Fatalf("CLOSED rewrote original call: %v %v", currentCall, err)
		}
		currentOutcome, err := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, originalDriver.OutcomeRef)
		if err != nil || !proto.Equal(currentOutcome, outcome) {
			t.Fatalf("CLOSED rewrote original outcome: %v %v", currentOutcome, err)
		}
		currentRequest, err := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, originalDriver.RequestRef)
		if err != nil || !proto.Equal(currentRequest.SnapshotRef, request.SnapshotRef) || len(currentRequest.ModelOperationRefs) != 1 || len(currentRequest.OutcomeRefs) != 1 {
			t.Fatalf("CLOSED original request expanded: %v %v", currentRequest, err)
		}
		if extra, err := f.h.Tasks.QueryModelCall(f.ctx, f.caller, originalDriver.RequestRef, 1); err != nil || extra != nil {
			t.Fatalf("CLOSED extra model position: %v %v", extra, err)
		}
		modelNow, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, modelAdmission.OperationId)
		if err != nil || !proto.Equal(modelNow.Execution, modelOperation.Execution) || modelNow.Effect.Outcome != "APPLIED" {
			t.Fatalf("CLOSED rewrote original model send: %v %v", modelNow, err)
		}
		originalProof, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, targetSeal.Ref)
		if err != nil || !proto.Equal(originalProof, targetSeal) {
			t.Fatalf("CLOSED rewrote pre-P2 original tombstone: %v %v", originalProof, err)
		}
		if e = f.h.Close(); e != nil {
			t.Fatal(e)
		}
		replayed := new(v1.CommandReceipt)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "close-task", "--json", closePath), replayed); e != nil || !proto.Equal(replayed, closeReceipt) {
			t.Fatalf("trusted closing replay changed: %v %v", replayed, e)
		}
		state := new(v1.ReasonerDriver)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "advance-task", "--json", advance), state); e != nil || !proto.Equal(state, completed) {
			t.Fatalf("CLOSED restart advanced original driver: %v %v", state, e)
		}
		f.h, e = assembly.Open(f.path, "u", "d")
		if e != nil {
			t.Fatal(e)
		}
		result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
		if err != nil {
			t.Fatal(err)
		}
		later, err := proto.Marshal(result)
		if err != nil || !bytes.Equal(fixed, later) {
			t.Fatal("CANCELLED Result changed on restart")
		}
	}
	budget, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects := target.Target.Snapshot()
	if e != nil || budget.Reserved != 0 || budget.Settled != 7 || provider.Calls() != 1 || len(provider.Bills()) != 1 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 || f.calls.Load() != 0 {
		t.Fatalf("fixed CANCELLED replay sent or changed original fees: %v %v", budget, e)
	}
	t.Log("actual fixed CANCELLED: original MODEL calls1/bills1/fee7; target requests0/effects0/bills0; both cancellation and task closing original ACKs; driver COMPLETED")
}

// 规则：G1、G3、G4、G5、G9、G10、G11、R7、开始-2
func TestProductionDriverCancellationBeforeModelSendsNothing(t *testing.T) {
	f, provider, target, configuration, binary := cancellationDriverFixture(t)
	configured := new(v1.CommandReceipt)
	if e := protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "configure-reasoner", "--json", configuration), configured); e != nil {
		t.Fatal(e)
	}
	accepted(t, configured, nil)
	driver, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1})
	if e != nil || !driver.Enabled || driver.RequestRef == nil || driver.OutcomeRef != nil || driver.AdmissionRef != nil || provider.Calls() != 0 {
		t.Fatalf("enabled original request before model: %v %v", driver, e)
	}
	originalRequest := proto.Clone(driver.RequestRef).(*v1.Ref)
	control, receipt := submitDriverCancellation(t, f, "driver-cancel-before-model")
	savedTask, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	advance := writeReasonerCLIJSON(t, "cancel-before-model-advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		state := new(v1.ReasonerDriver)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "advance-task", "--json", advance), state); e != nil || state.State != "WAITING" || state.WaitingReason != "TASK_NOT_ACTIVE" || !state.Enabled || !proto.Equal(state.RequestRef, originalRequest) || state.OutcomeRef != nil || state.AdmissionRef != nil {
			t.Fatalf("cancelled startup created model responsibility: %v %v", state, e)
		}
		configurationReplay := new(v1.CommandReceipt)
		if e = protojson.Unmarshal(cancellationDriverCLI(t, f, binary, "configure-reasoner", "--json", configuration), configurationReplay); e != nil || !proto.Equal(configurationReplay, configured) {
			t.Fatalf("original configuration replay changed authority: %v %v", configurationReplay, e)
		}
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, control)
	accepted(t, replay, e)
	if !proto.Equal(replay, receipt) {
		t.Fatal("saved control decision changed")
	}
	view := driverCancellationView(t, f)
	if len(view.Cancellation.AdmissionRefs) != 0 || len(view.PendingClosureRefs) != 0 || len(view.ClosureIntents) != 0 || len(view.ClosureSeals) != 0 || len(view.Operations) != 0 || len(view.UnresolvedEffectOperationRefs) != 0 || view.Task.ResultRef != nil || view.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || view.Task.PlanningGeneration != savedTask.PlanningGeneration {
		t.Fatalf("empty original cancellation scope or final facts fabricated: %v", view)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, originalRequest)
	if e != nil || request == nil || len(request.ModelOperationRefs) != 0 || len(request.OutcomeRefs) != 0 {
		t.Fatalf("cancel created request/model outcome: %v %v", request, e)
	}
	for _, position := range []uint32{0, 1} {
		if call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, originalRequest, position); e != nil || call != nil {
			t.Fatalf("cancel created model position%d: %v %v", position, call, e)
		}
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects := target.Target.Snapshot()
	if e != nil || budget.Reserved != 0 || budget.Settled != 0 || provider.Calls() != 0 || len(provider.Bills()) != 0 || f.calls.Load() != 0 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatalf("cancel/replay/restart sent model or target: %v %v", budget, e)
	}
}

func cancellationDriverFixture(t *testing.T) (*fixture, *simulator.ModelProvider, *simulator.BillingTarget, string, string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	parameters, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("cancel-driver-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`})
	if e != nil {
		t.Fatal(e)
	}
	f.parameters = parameters
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref, capability.ApprovedBy = nil, nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("cancel-driver-target-cap"), Capability: capability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	targetCapability, targetGrant := f.capability, f.grant
	scopeRequirement(t, f)
	proposal := &v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: targetCapability, SchemaDigest: capability.SchemaDigest, ParametersRef: parameters}, BasisRefs: []*v1.Ref{parameters}}
	output, e := protojson.Marshal(proposal)
	if e != nil {
		t.Fatal(e)
	}
	provider := simulator.NewModelProvider()
	provider.Output = string(output)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if e != nil || json.Unmarshal(body, &view) != nil || len(view.Facts.Facts) == 0 {
			t.Error("model did not receive original serialized snapshot")
			http.Error(w, "snapshot", 500)
			return
		}
		snapshot := new(v1.ContextSnapshot)
		if e = protojson.Unmarshal(view.Facts.Facts[0], snapshot); e != nil || !proto.Equal(snapshot.TaskRef.GetName(), f.task.Name) || snapshot.RequestRef == nil || snapshot.RequirementsRef == nil {
			t.Errorf("model snapshot: %v %v", snapshot, e)
			http.Error(w, "snapshot", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		provider.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	capability.Ref, capability.ApprovedBy = nil, nil
	capability.Action, capability.Resource = "MODEL_INFER", endpoint.URL
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson, capability.SchemaDigest = nil, ""
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("cancel-driver-model-cap"), Capability: capability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref, grant.Issuer, grant.Status = nil, nil, ""
	grant.UsePoolId = "cancel-driver-model"
	grant.Permissions[0].Action, grant.Permissions[0].Resource = "MODEL_INFER", endpoint.URL
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("cancel-driver-model-grant"), Grant: grant})
	accepted(t, r, e)
	f.grant = r.ResultRef
	if _, e = f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("cancel-driver-request"), TaskId: f.task.Name}); e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.Actions = []*v1.ReasonerActionAuthority{{CapabilityRef: targetCapability, GrantRef: targetGrant}}
	configuration := writeReasonerCLIJSON(t, "cancel-driver.json", &v1.ConfigureReasonerDriverCommand{Header: header("cancel-driver-config"), TaskId: f.task.Name, Policy: policy})
	return f, provider, target, configuration, binary
}

func cancellationDriverCLI(t *testing.T, f *fixture, binary string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, append([]string{"--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host"}, args...)...)
	body, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("production CLI %v: %v %s", args, e, body)
	}
	return body
}

func submitDriverCancellation(t *testing.T, f *fixture, id string) (*v1.SubmitInputCommand, *v1.CommandReceipt) {
	t.Helper()
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	control := &v1.SubmitInputCommand{Header: header(id), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
	receipt, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, control)
	accepted(t, receipt, e)
	return control, receipt
}

func driverCancellationView(t *testing.T, f *fixture) *v1.CancellationView {
	t.Helper()
	cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e := cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	view := new(v1.CancellationView)
	if e := protojson.Unmarshal(out.Bytes(), view); e != nil {
		t.Fatal(e)
	}
	return view
}
