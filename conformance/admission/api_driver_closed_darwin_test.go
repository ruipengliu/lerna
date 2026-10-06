//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G5、G10、G11、R7、完成-4、完成-6
func TestProductionAPIDriverFailedClosingKeepsLateOriginalWorkAndFixedResult(t *testing.T) {
	testProductionAPIDriverClosingKeepsLateOriginalWork(t, "FAILED")
}

// 规则：G1、G3、G4、G5、G10、G11、R7、完成-4、完成-6
func TestProductionAPIDriverCancelledClosingKeepsLateOriginalWorkAndFixedResult(t *testing.T) {
	testProductionAPIDriverClosingKeepsLateOriginalWork(t, "CANCELLED")
}

func testProductionAPIDriverClosingKeepsLateOriginalWork(t *testing.T, outcome string) {
	type received struct{ method, operation, attempt, key, send, account, subjectOperation, subjectAttempt, subjectKey string }
	var mu sync.Mutex
	var requests []received
	effects := map[string]bool{}
	bills := map[string][]byte{}
	queued := make(chan string, 1)
	release, applied := make(chan struct{}), make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	// 真实目标接到的原请求进入业务队列；与客户端连接和出口临界区独立。
	go func() {
		select {
		case key := <-queued:
			select {
			case <-release:
				mu.Lock()
				effects[key] = true
				mu.Unlock()
				close(applied)
			case <-stop:
			}
		case <-stop:
		}
	}()
	assertTarget := func(posts, gets, effectCount, billCount int) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		p, g := 0, 0
		for _, request := range requests {
			switch request.method {
			case "POST":
				p++
			case "GET":
				g++
			default:
				t.Fatalf("unexpected request %v", request)
			}
		}
		if p != posts || g != gets || len(effects) != effectCount || len(bills) != billCount {
			t.Fatalf("actual POST/GET/effect/bill=%d/%d/%d/%d, want %d/%d/%d/%d", p, g, len(effects), len(bills), posts, gets, effectCount, billCount)
		}
	}
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-closed-secret" {
			t.Error("native credential missing")
			w.WriteHeader(401)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		got := received{r.Method, r.Header.Get("Lerna-Operation"), r.Header.Get("Lerna-Attempt"), r.Header.Get("Idempotency-Key"), r.Header.Get("Lerna-Send-Id"), r.Header.Get("Lerna-Account"), r.Header.Get("Lerna-Query-Operation"), r.Header.Get("Lerna-Query-Attempt"), r.Header.Get("Lerna-Query-Key")}
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, got)
		amount := int64(2)
		if r.Method == "POST" {
			amount = 7
		}
		bill := apiQueryBoundaryBill(r, amount)
		encoded, err := json.Marshal(map[string]any{"billing": bill})
		if err != nil {
			t.Error(err)
			return
		}
		bills[got.send] = encoded
		if r.Method == "POST" {
			if string(body) != `{"quantity":1,"value":"hello"}` {
				t.Error("noncanonical write")
			}
			queued <- got.key
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if r.Method != "GET" || len(body) != 0 {
			t.Error("unexpected query payload")
			w.WriteHeader(405)
			return
		}
		response := apiQueryResponse(r)
		response["applied"] = effects[got.subjectKey]
		response["billing"] = bill
		_ = json.NewEncoder(w).Encode(response)
	})
	x := newProductionAPIDriver(t, target, "synthetic-api-closed-secret")
	f := x.f
	d, cap, grant, keychain := x.descriptor, x.queryCapability, x.queryGrant, x.keychain
	driver := x.invoke(t)
	if driver.State != "WAITING" || driver.WaitingReason != "OPERATION_PENDING" || !proto.Equal(driver.RequestRef, x.snapshot.RequestRef) {
		t.Fatalf("driver did not own pending native API: %v", driver)
	}
	x.reopen(t)
	a, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, driver.AdmissionRef)
	if err != nil || !proto.Equal(a.CapabilityRef, x.targetCapability) {
		t.Fatalf("driver API admission: %v %v", a, err)
	}
	oldRequest, err := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
	if err != nil || len(oldRequest.ModelOperationRefs) != 1 || len(oldRequest.OutcomeRefs) != 1 {
		t.Fatalf("original request: %v %v", oldRequest, err)
	}
	assertDriverReceivedSnapshot(t, &rejectionDriver{f: f, provider: x.provider}, oldRequest)
	oldCall, err := f.h.Tasks.QueryModelCall(f.ctx, f.caller, driver.RequestRef, 0)
	if err != nil || oldCall == nil {
		t.Fatalf("original call: %v %v", oldCall, err)
	}
	oldOutcome, err := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, oldRequest.OutcomeRefs[0])
	if err != nil || oldOutcome == nil {
		t.Fatalf("original outcome: %v %v", oldOutcome, err)
	}
	oldModel, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, oldRequest.ModelOperationRefs[0].Name)
	if err != nil || oldModel.Effect.Outcome != "APPLIED" || oldModel.Lifecycle != "SETTLED" {
		t.Fatalf("original MODEL: %v %v", oldModel, err)
	}
	oldModelAdmission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, oldModel.AdmissionRef)
	if err != nil {
		t.Fatal(err)
	}
	oldModelSource, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, oldModel.Execution.Send.Ref)
	if err != nil || oldModelSource.Amount == nil || *oldModelSource.Amount != 7 || oldModelSource.Status != "SETTLED" {
		t.Fatalf("MODEL source: %v %v", oldModelSource, err)
	}
	assertOldModel := func() {
		t.Helper()
		request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
		if e != nil || !proto.Equal(request, oldRequest) {
			t.Fatalf("old request changed: %v %v", request, e)
		}
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, driver.RequestRef, 0)
		if e != nil || !proto.Equal(call, oldCall) {
			t.Fatalf("old call changed: %v %v", call, e)
		}
		extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, driver.RequestRef, 1)
		if e != nil || extra != nil {
			t.Fatalf("new call position: %v %v", extra, e)
		}
		outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, oldRequest.OutcomeRefs[0])
		if e != nil || !proto.Equal(outcome, oldOutcome) {
			t.Fatalf("old outcome changed: %v %v", outcome, e)
		}
		model, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, oldModel.Ref.Name)
		if e == nil && model != nil {
			normalized := proto.Clone(model).(*v1.Operation)
			normalized.Ref.Revision = oldModel.Ref.Revision
			normalized.ClosureEvidenceRefs = oldModel.ClosureEvidenceRefs
			if !proto.Equal(normalized, oldModel) {
				t.Fatal("closure changed original MODEL identity, input, execution or effect")
			}
		}
		if e != nil || model == nil {
			t.Fatalf("old MODEL changed: %v %v", model, e)
		}
		admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, oldModel.AdmissionRef)
		if e != nil || !proto.Equal(admission, oldModelAdmission) {
			t.Fatalf("old MODEL input changed: %v %v", admission, e)
		}
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, oldModel.Execution.Send.Ref)
		if e != nil || !proto.Equal(source, oldModelSource) {
			t.Fatalf("old MODEL fee changed: %v %v", source, e)
		}
		if x.provider.Calls() != 1 || len(x.provider.Bills()) != 1 {
			t.Fatal("closed driver resampled MODEL")
		}
	}
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("lost real write: %v %v", original, err)
	}
	originalRaw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, original.Execution.Send.ObservationRef)
	if err != nil || originalRaw.TransportError == "" || originalRaw.QuerySubject != nil {
		t.Fatalf("lost original raw: %v %v", originalRaw, err)
	}
	initialSource, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	if err != nil || initialSource.Status != "PENDING" || initialSource.Amount != nil {
		t.Fatalf("lost receipt invented known fee: %v %v", initialSource, err)
	}
	request := reconciliationCommand(f, a, cap, grant)
	r, err := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, err)
	originalPlanReceipt := proto.Clone(r).(*v1.CommandReceipt)
	originalPlan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || originalPlan.CheckCount != 0 || len(originalPlan.QueryRefs) != 0 {
		t.Fatalf("plan must precede CANCEL and actual read: %v %v", originalPlan, err)
	}
	r, err = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("pause-original-closed-plan"), OperationId: a.OperationId, ExpectedRevision: originalPlan.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, err)
	paused, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || paused.State != "PAUSED" || paused.CheckCount != 0 {
		t.Fatalf("original plan not paused: %v %v", paused, err)
	}
	jobs, err := f.h.Ledger.QueryJobs(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, job := range jobs {
		if proto.Equal(job.Ref, paused.JobRef) && job.State == "BLOCKED" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("paused original query has no BLOCKED owner job")
	}
	var scope *v1.Cancellation
	var cancel *v1.SubmitInputCommand
	var cancelReceipt *v1.CommandReceipt
	var cancelIntent *v1.CancellationClosureIntent
	var cancelSeal *v1.CancellationSeal
	if outcome == "CANCELLED" {
		task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
		if e != nil {
			t.Fatal(e)
		}
		goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
		if e != nil {
			t.Fatal(e)
		}
		cancel = &v1.SubmitInputCommand{Header: header("api-cancel-original-plan"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
		cancelReceipt, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
		accepted(t, cancelReceipt, err)
		scope, err = f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
		if err != nil || len(scope.AdmissionRefs) != 2 || !proto.Equal(scope.AdmissionRefs[1], a.Ref) || len(scope.ClosureIntentRefs) != 2 || !proto.Equal(scope.ControlIdentity, cancel.Header.Identity) {
			t.Fatalf("original cancel scope: %v %v", scope, err)
		}
		if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
			t.Fatal(err)
		}
		cancelIntent, err = f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[1])
		if err != nil || cancelIntent.RecipientReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("missing original cancel ACK: %v %v", cancelIntent, err)
		}
		cancelSeal, err = f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, cancelIntent.RecipientReceipt.ResultRef)
		if err != nil || cancelSeal.NoSendProven || !cancelSeal.PhysicalSendWasPossible || !proto.Equal(cancelSeal.CancellationRef, scope.Ref) || !proto.Equal(cancelSeal.AdmissionRef, a.Ref) || !proto.Equal(cancelSeal.OperationId, a.OperationId) {
			t.Fatalf("cancel erased possible original send: %v %v", cancelSeal, err)
		}
	}
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	closingCommand := &v1.BeginTaskCloseCommand{Header: header("api-current-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: outcome, CloseReason: "UNABLE_TO_COMPLETE", CancellationRef: scope.GetRef()}
	closeReceipt, err := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, closingCommand)
	accepted(t, closeReceipt, err)
	if err = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if err != nil || view.Result == nil || view.Result.Outcome != outcome || view.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || len(view.PendingClosureRefs) != 0 || len(view.ClosureIntents) != 2 || len(view.ClosureSeals) != 2 || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 2 {
		t.Fatalf("native close missing exact responsibility: %v %v", view, err)
	}
	seal := view.ClosureSeals[1]
	if seal.NoSendProven || !seal.PhysicalSendWasPossible || !proto.Equal(seal.AdmissionRef, a.Ref) || !proto.Equal(seal.OperationId, a.OperationId) || !proto.Equal(seal.TaskClosingRef, view.Closing.Ref) || len(seal.ClosedSendRefs) != 0 || view.ClosureIntents[1].RecipientReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("closing ACK erased sent original: %v", view)
	}
	completedDriver, err := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	if err != nil || completedDriver.State != "COMPLETED" {
		t.Fatalf("closed driver: %v %v", completedDriver, err)
	}
	assertOldModel()
	frozenInputs, err := f.h.Tasks.QueryInputs(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	fixedResult := proto.Clone(view.Result).(*v1.Result)
	fixedBytes, err := proto.Marshal(fixedResult)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixedResult.OperationRefs) != 2 || len(fixedResult.UnknownOperationRefs) != 1 || !proto.Equal(fixedResult.UnknownOperationRefs[0].Name, a.OperationId) || fixedResult.UsageSnapshot.Reserved != 30 || fixedResult.UsageSnapshot.Settled != 7 || len(fixedResult.Conditions) != 1 || fixedResult.Conditions[0].Conclusion != "UNKNOWN" {
		t.Fatalf("fixed Result lost original UNKNOWN/reserve: %v", fixedResult)
	}
	originalClosing := proto.Clone(view.Closing).(*v1.TaskClosing)
	originalExecutionFollowup := proto.Clone(view.ExecutionFollowups[0]).(*v1.ExecutionFollowup)
	originalSettlementFollowup := proto.Clone(view.SettlementFollowups[1]).(*v1.SettlementFollowup)
	if !proto.Equal(originalExecutionFollowup.AttemptRef.Name, original.Execution.Attempt.Ref.Name) || len(originalExecutionFollowup.SendRefs) != 1 || !proto.Equal(originalExecutionFollowup.SendRefs[0].Name, original.Execution.Send.Ref.Name) || originalExecutionFollowup.ExecutorEndpointId != a.ExecutorEndpointId || len(originalSettlementFollowup.BillingSourceRefs) != 1 || len(originalSettlementFollowup.ReservationRefs) != 1 {
		t.Fatal("followup identity lost original attempt/send/fee")
	}
	assertFixed := func(executionState, feeState string) {
		t.Helper()
		assertOldModel()
		currentDriver, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
		if e != nil || !proto.Equal(currentDriver, completedDriver) {
			t.Fatalf("completed driver changed: %v %v", currentDriver, e)
		}
		inputs, e := f.h.Tasks.QueryInputs(f.ctx, f.caller, f.task.Name)
		if e != nil || !proto.Equal(inputs, frozenInputs) {
			t.Fatalf("closed inputs changed: %v %v", inputs, e)
		}
		current, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
		if e != nil {
			t.Fatal(e)
		}
		body, e := proto.Marshal(current.Result)
		if e != nil || !bytes.Equal(body, fixedBytes) || !proto.Equal(current.Result.Ref, fixedResult.Ref) || !proto.Equal(current.Closing, originalClosing) || len(current.ExecutionFollowups) != 1 || !proto.Equal(current.ExecutionFollowups[0], originalExecutionFollowup) || len(current.SettlementFollowups) != 2 || !proto.Equal(current.SettlementFollowups[1], originalSettlementFollowup) || len(current.FollowupJobs) != 3 {
			t.Fatalf("late facts rewrote fixed result/scope: %v %v", current, e)
		}
		for _, j := range current.FollowupJobs {
			want := feeState
			if proto.Equal(j.Ref.Name, view.SettlementFollowups[0].JobRef.Name) {
				want = "COMPLETED"
			}
			if j.Module == "ledger" {
				want = executionState
			}
			if j.State != want {
				t.Fatalf("owner job %s: %s want %s", j.JobType, j.State, want)
			}
		}
	}
	processOwners := func() {
		t.Helper()
		if e := f.h.Ledger.ProcessExecutionFollowups(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
		if e := f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
	}
	processOwners()
	assertFixed("WAITING", "WAITING")
	assertTarget(1, 0, 0, 1)
	reopen := func() {
		t.Helper()
		if e := f.h.Close(); e != nil {
			t.Fatal(e)
		}
		next, e := assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
		if e != nil {
			t.Fatal(e)
		}
		f.h = next
	}
	reopen()
	assertFixed("WAITING", "WAITING")
	assertTarget(1, 0, 0, 1)
	// 在效果仍未发生时先交付目标真实 POST 生成的账单。
	mu.Lock()
	billBytes := append([]byte(nil), bills[original.Execution.Send.Ref.Name.LocalId]...)
	mu.Unlock()
	evidence, err := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("api-close-late-bill").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(billBytes)})
	if err != nil {
		t.Fatal(err)
	}
	imported := &v1.ImportBillCommand{Header: header("api-close-import-original-bill"), SendRef: original.Execution.Send.Ref, EvidenceRef: evidence}
	r, err = f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	accepted(t, r, err)
	originalBillReceipt := proto.Clone(r).(*v1.CommandReceipt)
	still, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || still.Dispatch != "SEALED" || still.Effect.Outcome != "UNKNOWN" || still.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(still.Execution, original.Execution) {
		t.Fatalf("original bill decided effect or changed execution: %v %v", still, err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 0 || budget.Settled != 14 {
		t.Fatalf("original fee not independently settled: %v %v", budget, err)
	}
	processOwners()
	assertFixed("WAITING", "COMPLETED")
	assertTarget(1, 0, 0, 1)
	close(release)
	<-applied
	assertTarget(1, 0, 1, 1)
	r, err = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("resume-original-closed-plan"), OperationId: a.OperationId, ExpectedRevision: paused.Ref.Revision, Action: "RESUME"})
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 || !proto.Equal(plan.Ref.Name, originalPlan.Ref.Name) || !proto.Equal(plan.OriginalAttemptRef, originalPlan.OriginalAttemptRef) || !proto.Equal(plan.QueryCapabilityRef, originalPlan.QueryCapabilityRef) || !proto.Equal(plan.GrantRef, originalPlan.GrantRef) || !proto.Equal(plan.RequestedBy, originalPlan.RequestedBy) {
		t.Fatalf("cancel replaced original plan: %v %v", plan, err)
	}
	relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(relation.Work.OwnerRef.Name, originalPlan.Ref.Name) || !proto.Equal(relation.Work.SourceRef.Name, relation.Ref.Name) || !proto.Equal(relation.Work.SourceIdentity, originalPlan.RequestedBy) || !proto.Equal(relation.Work.GrantRef, originalPlan.GrantRef) || !proto.Equal(relation.Work.ParametersRef, originalPlan.ParametersRef) {
		t.Fatalf("query lost original owner/source/authority: %v", relation)
	}
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, relation.AdmissionReceipt.ResultRef)
	if err != nil || admission.WorkCategory != "CLOSURE" || admission.ControlGeneration != originalClosing.ControlGeneration || !proto.Equal(admission.Origin, relation.Work.Ref) || !proto.Equal(admission.TaskId, a.TaskId) || admission.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("query not independently admitted in current control: %v %v", admission, err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
	if err != nil || query.Lifecycle != "SETTLED" || proto.Equal(query.Ref.Name, a.OperationId) || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, original.Execution.Attempt.Ref.Name) || query.QuerySubject.ExternalKey != original.Execution.Attempt.ExternalKey || query.QuerySubject.TargetScope != original.CapabilitySnapshot.Resource || query.QuerySubject.ExecutorEndpointId != a.ExecutorEndpointId || !proto.Equal(query.QuerySubject.CapabilityRef, original.CapabilitySnapshot.Ref) || !proto.Equal(query.Execution.CallDescriptor.QuerySubject, query.QuerySubject) || !proto.Equal(query.Execution.CallDescriptor.ApiDescriptor, original.Execution.CallDescriptor.ApiDescriptor) {
		t.Fatalf("independent query changed original binding: %v %v", query, err)
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, relation.ObservationRef)
	if err != nil || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.Ref.Name) || raw.ExternalKey != query.Execution.Attempt.ExternalKey || !proto.Equal(raw.QuerySubject, query.QuerySubject) || proto.Equal(raw.Ref, originalRaw.Ref) {
		t.Fatalf("query raw relabeled as original: %v %v", raw, err)
	}
	history, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	if err != nil || len(history.AdmissionRefs) != 3 || !proto.Equal(history.AdmissionRefs[0], oldModel.AdmissionRef) || !proto.Equal(history.AdmissionRefs[1], a.Ref) || !proto.Equal(history.AdmissionRefs[2], admission.Ref) {
		t.Fatalf("query missing from full task history: %v %v", history, err)
	}
	if outcome == "CANCELLED" {
		savedScope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
		if e != nil || !proto.Equal(savedScope, scope) {
			t.Fatalf("query rewrote cancellation scope: %v %v", savedScope, e)
		}
		i, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, cancelIntent.Ref)
		if e != nil || !proto.Equal(i, cancelIntent) {
			t.Fatal("query changed original cancellation ACK", e)
		}
		s, e := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, cancelSeal.Ref)
		if e != nil || !proto.Equal(s, cancelSeal) {
			t.Fatal("query changed original cancellation seal", e)
		}
	}
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Dispatch != "SEALED" || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || !proto.Equal(after.Execution, original.Execution) {
		t.Fatalf("original query proof: %v %v", after, err)
	}
	sources := map[string]bool{}
	for i, send := range []*v1.PhysicalSend{original.Execution.Send, query.Execution.Send} {
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		want := int64(7)
		if i == 1 {
			want = 2
		}
		if e != nil || source == nil || source.Amount == nil || *source.Amount != want || source.Status != "SETTLED" || source.Identity.NativeInstance != send.Ref.Name.LocalId || source.Identity.Account != d.Binding.Account || source.Identity.Namespace != "lerna-reference" || sources[source.Ref.Name.LocalId] {
			t.Fatalf("separate late original/query fees: %v %v", source, e)
		}
		if i == 0 && (!proto.Equal(source.Ref.Name, initialSource.Ref.Name) || !proto.Equal(source.ReservationRef.Name, initialSource.ReservationRef.Name) || !proto.Equal(source.SendRef.Name, original.Execution.Send.Ref.Name) || !proto.Equal(source.AdmissionRef, a.Ref)) {
			t.Fatal("late bill replaced original fee source")
		}
		sources[source.Ref.Name.LocalId] = true
	}
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 0 || budget.Settled != 16 {
		t.Fatalf("late original bill not settled exactly once: %v %v", budget, err)
	}
	r, err = f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	accepted(t, r, err)
	if !proto.Equal(r, originalBillReceipt) {
		t.Fatal("original bill receipt changed on replay")
	}
	if cancel != nil {
		r, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
		accepted(t, r, err)
		if !proto.Equal(r, cancelReceipt) {
			t.Fatal("CANCEL replay changed receipt")
		}
	}
	r, err = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, closingCommand)
	accepted(t, r, err)
	if !proto.Equal(r, closeReceipt) {
		t.Fatal("close replay changed receipt")
	}
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, err)
	if !proto.Equal(r, originalPlanReceipt) {
		t.Fatal("original plan receipt changed on replay")
	}
	processOwners()
	assertFixed("COMPLETED", "COMPLETED")
	reopen()
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(restored, after) {
		t.Fatalf("restart/bill changed original effect identity: %v %v", restored, err)
	}
	queryAfter, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, query.Ref.Name)
	if err != nil || !proto.Equal(queryAfter, query) {
		t.Fatalf("late original fee changed query: %v %v", queryAfter, err)
	}
	originalRawAfter, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, originalRaw.Ref)
	if err != nil || !proto.Equal(originalRawAfter, originalRaw) {
		t.Fatalf("query rewrote lost original raw: %v %v", originalRawAfter, err)
	}
	restoredBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || !proto.Equal(restoredBudget, budget) {
		t.Fatalf("restart changed fees: %v %v", restoredBudget, err)
	}
	processOwners()
	assertFixed("COMPLETED", "COMPLETED")
	view, err = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	assertClosingTraceSources(t, f, view)
	assertAPIQueryTrace(t, f, plan)
	assertAPITraceExcludes(t, f, "synthetic-api-closed-secret", keychain.Path(), d.Binding.Origin)
	assertTarget(1, 1, 1, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0].method != "POST" || requests[1].method != "GET" || len(effects) != 1 || len(bills) != 2 {
		t.Fatalf("actual request/effect/bill count: %v %d %d", requests, len(effects), len(bills))
	}
	write, read := requests[0], requests[1]
	if write.operation != a.OperationId.LocalId || write.attempt != original.Execution.Attempt.Ref.Name.LocalId || write.key != original.Execution.Attempt.ExternalKey || write.send != original.Execution.Send.Ref.Name.LocalId || write.account != d.Binding.Account || read.operation != query.Ref.Name.LocalId || read.attempt != query.Execution.Attempt.Ref.Name.LocalId || read.key != query.Execution.Attempt.ExternalKey || read.send != query.Execution.Send.Ref.Name.LocalId || read.account != write.account || read.subjectOperation != write.operation || read.subjectAttempt != write.attempt || read.subjectKey != write.key {
		t.Fatalf("actual original/query identity drift: %v", requests)
	}
	t.Logf("actual outcome=%s MODEL=1 POST=1 GET=1 requests=%d effect=%d bills=%d sources=%d current_settled=%d current_reserved=%d fixed_settled=%d fixed_reserved=%d original_unknown=%d final_original=%s Result_bytes_unchanged=true", outcome, len(requests), len(effects), len(bills), len(sources), budget.Settled, budget.Reserved, fixedResult.UsageSnapshot.Settled, fixedResult.UsageSnapshot.Reserved, len(fixedResult.UnknownOperationRefs), after.Effect.Outcome)
}
