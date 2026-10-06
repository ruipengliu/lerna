//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、R7、准入-7、开始-2
func TestAPICancellationContinuesSavedOriginalQueryUnderCurrentControl(t *testing.T) {
	type received struct{ method, operation, attempt, key, send, account, subjectOperation, subjectAttempt, subjectKey string }
	var mu sync.Mutex
	var requests []received
	effects := map[string]bool{}
	bills := map[string][]byte{}
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-cancel-secret" {
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
			effects[got.key] = true
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
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, cap, grant := configureAPIQueryable(t, f, true)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-cancel-secret"))
	a, start := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, err := f.h.Egress.Invoke(f.ctx, actor, start)
	accepted(t, r, err)
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("lost real write: %v %v", original, err)
	}
	originalRaw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, original.Execution.Send.ObservationRef)
	if err != nil || originalRaw.TransportError == "" || originalRaw.QuerySubject != nil {
		t.Fatalf("lost original raw: %v %v", originalRaw, err)
	}
	request := reconciliationCommand(f, a, cap, grant)
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, err)
	originalPlan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || originalPlan.CheckCount != 0 || len(originalPlan.QueryRefs) != 0 {
		t.Fatalf("plan must precede CANCEL and actual read: %v %v", originalPlan, err)
	}
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if err != nil {
		t.Fatal(err)
	}
	cancel := &v1.SubmitInputCommand{Header: header("api-cancel-original-plan"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
	r, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
	accepted(t, r, err)
	scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
	if err != nil || len(scope.AdmissionRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], a.Ref) || len(scope.ClosureIntentRefs) != 1 || !proto.Equal(scope.ControlIdentity, cancel.Header.Identity) {
		t.Fatalf("original cancel scope: %v %v", scope, err)
	}
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	intent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if err != nil || intent.RecipientReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || !proto.Equal(intent.Command.OperationId, a.OperationId) || intent.Command.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("exact endpoint ACK: %v %v", intent, err)
	}
	seal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
	if err != nil || seal.NoSendProven || !seal.PhysicalSendWasPossible || !proto.Equal(seal.CancellationRef, scope.Ref) || !proto.Equal(seal.AdmissionRef, a.Ref) || !proto.Equal(seal.OperationId, a.OperationId) {
		t.Fatalf("cancel erased possible original send: %v %v", seal, err)
	}
	sealed, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || sealed.Dispatch != "SEALED" || sealed.Effect.Outcome != "UNKNOWN" || sealed.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(sealed.Execution, original.Execution) {
		t.Fatalf("sealed original responsibility: %v %v", sealed, err)
	}
	showCancellation := func() *v1.CancellationView {
		t.Helper()
		cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Budget: f.h.Budget, Caller: f.caller, Domain: "d"}
		var out bytes.Buffer
		if e := cli.Run(f.ctx, []string{"cancellation", a.TaskId.LocalId}, &out); e != nil {
			t.Fatal(e)
		}
		view := new(v1.CancellationView)
		if e := protojson.Unmarshal(out.Bytes(), view); e != nil {
			t.Fatal(e)
		}
		return view
	}
	view := showCancellation()
	if view.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || view.Task.ResultRef != nil || len(view.PendingClosureRefs) != 0 || len(view.UnresolvedEffectOperationRefs) != 1 || !proto.Equal(view.UnresolvedEffectOperationRefs[0].Name, a.OperationId) || len(view.Reconciliations) != 1 || !proto.Equal(view.Reconciliations[0], originalPlan) {
		t.Fatalf("CLI hid saved plan/unknown after ACK: %v", view)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 30 || budget.Settled != 0 {
		t.Fatalf("cancel released unknown fee: %v %v", budget, err)
	}
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
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, relation.AdmissionReceipt.ResultRef)
	if err != nil || admission.WorkCategory != "CLOSURE" || admission.ControlGeneration != scope.ControlGeneration || admission.ControlGeneration != a.ControlGeneration+1 || !proto.Equal(admission.TaskId, a.TaskId) || admission.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("query not independently admitted in current control: %v %v", admission, err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
	if err != nil || query.Lifecycle != "SETTLED" || proto.Equal(query.Ref.Name, a.OperationId) || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, original.Execution.Attempt.Ref.Name) || query.QuerySubject.ExternalKey != original.Execution.Attempt.ExternalKey || !proto.Equal(query.Execution.CallDescriptor.ApiDescriptor, original.Execution.CallDescriptor.ApiDescriptor) {
		t.Fatalf("independent query changed original binding: %v %v", query, err)
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, relation.ObservationRef)
	if err != nil || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.Ref.Name) || raw.ExternalKey != query.Execution.Attempt.ExternalKey || !proto.Equal(raw.QuerySubject, query.QuerySubject) || proto.Equal(raw.Ref, originalRaw.Ref) {
		t.Fatalf("query raw relabeled as original: %v %v", raw, err)
	}
	history, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	if err != nil || len(history.AdmissionRefs) != 2 || !proto.Equal(history.AdmissionRefs[0], a.Ref) || !proto.Equal(history.AdmissionRefs[1], admission.Ref) {
		t.Fatalf("query missing from full task history: %v %v", history, err)
	}
	savedScope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
	if err != nil || !proto.Equal(savedScope, scope) {
		t.Fatalf("late admission rewrote cancellation scope: %v %v", savedScope, err)
	}
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Dispatch != "SEALED" || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || !proto.Equal(after.Execution, original.Execution) {
		t.Fatalf("original query proof: %v %v", after, err)
	}
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 30 || budget.Settled != 2 {
		t.Fatalf("query proof erased original bill responsibility: %v %v", budget, err)
	}
	// 目标在真实POST期间生成的账单字节晚交接；不从核心记录合成账单。
	mu.Lock()
	billBytes := append([]byte(nil), bills[original.Execution.Send.Ref.Name.LocalId]...)
	mu.Unlock()
	evidence, err := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("api-cancel-late-bill").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: string(billBytes)})
	if err != nil {
		t.Fatal(err)
	}
	imported := &v1.ImportBillCommand{Header: header("api-cancel-import-original-bill"), SendRef: original.Execution.Send.Ref, EvidenceRef: evidence}
	r, err = f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	accepted(t, r, err)
	sources := map[string]bool{}
	for i, send := range []*v1.PhysicalSend{original.Execution.Send, query.Execution.Send} {
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		want := int64(7)
		if i == 1 {
			want = 2
		}
		if e != nil || source == nil || source.Amount == nil || *source.Amount != want || source.Status != "SETTLED" || source.Identity.NativeInstance != send.Ref.Name.LocalId || sources[source.Ref.Name.LocalId] {
			t.Fatalf("separate late original/query fees: %v %v", source, e)
		}
		sources[source.Ref.Name.LocalId] = true
	}
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 0 || budget.Settled != 9 {
		t.Fatalf("late original bill not settled exactly once: %v %v", budget, err)
	}
	r, err = f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	accepted(t, r, err)
	r, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
	accepted(t, r, err)
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, err)
	r, err = f.h.Egress.Invoke(f.ctx, actor, start)
	accepted(t, r, err)
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if err != nil {
		t.Fatal(err)
	}
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
	view = showCancellation()
	if !proto.Equal(view.Cancellation, scope) || view.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || view.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || view.Task.ResultRef != nil || len(view.UnresolvedEffectOperationRefs) != 0 || len(view.PendingClosureRefs) != 0 || len(view.Reconciliations) != 1 || !proto.Equal(view.Reconciliations[0], plan) {
		t.Fatalf("cleanup invented Result or new responsibility: %v", view)
	}
	result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, a.TaskId)
	if err != nil || result != nil {
		t.Fatalf("unrequested Result: %v %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0].method != "POST" || requests[1].method != "GET" || len(effects) != 1 || len(bills) != 2 {
		t.Fatalf("actual request/effect/bill count: %v %d %d", requests, len(effects), len(bills))
	}
	write, read := requests[0], requests[1]
	if write.operation != a.OperationId.LocalId || write.attempt != original.Execution.Attempt.Ref.Name.LocalId || write.key != original.Execution.Attempt.ExternalKey || write.send != original.Execution.Send.Ref.Name.LocalId || write.account != d.Binding.Account || read.operation != query.Ref.Name.LocalId || read.attempt != query.Execution.Attempt.Ref.Name.LocalId || read.key != query.Execution.Attempt.ExternalKey || read.send != query.Execution.Send.Ref.Name.LocalId || read.account != write.account || read.subjectOperation != write.operation || read.subjectAttempt != write.attempt || read.subjectKey != write.key {
		t.Fatalf("actual original/query identity drift: %v", requests)
	}
}
