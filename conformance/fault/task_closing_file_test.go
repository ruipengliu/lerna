//go:build fault && darwin

package fault_test

import (
	"bytes"
	"context"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5、G10、G11、R7、完成-4、完成-6
func TestFailedClosingKeepsOriginalFileHistoryQueryAndFixedResult(t *testing.T) {
	testNonSuccessFileHistory(t, "FAILED")
}

// 规则：G1、G3、G5、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingKeepsOriginalFileHistoryQueryAndFixedResult(t *testing.T) {
	testNonSuccessFileHistory(t, "CANCELLED")
}

func testNonSuccessFileHistory(t *testing.T, closingOutcome string) {
	t.Helper()
	n := newNativeScenario(t)
	publication := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "readback.object", Error: syscall.EIO, Recorder: publication})
	a, start := n.prepare(t, "closing-original", "CREATE", "", []byte("original A"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	originalReceipt := proto.Clone(r).(*v1.CommandReceipt)
	first := readNativePointer(t, n.root)
	before, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || before.Lifecycle != "SETTLED" || before.Dispatch != "SEALED" || before.Effect.Outcome != "UNKNOWN" || before.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("real readback failure lost original unknown: %v %v", before, e)
	}
	originalExecution := proto.Clone(before.Execution).(*v1.Execution)
	originalObservation := n.observation(t, a)
	resources, e := n.h.Content.QueryFileResources(n.ctx, n.caller, originalObservation.FileEvidence.ResourcesRef)
	if e != nil || resources == nil || resources.CleanupStatus != "RETAINED" {
		t.Fatalf("original resources missing: %v %v", resources, e)
	}

	// 后继来自自己的实际任务和准入，之后原查询不得把 A 恢复到当前指针。
	successor := n.additionalTask(t, "closing-successor")
	_, next := successor.prepare(t, "closing-B", "REPLACE", first.Version, []byte("successor B"))
	r, e = successor.h.Egress.Invoke(successor.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, next)
	requireAccepted(t, r, e)
	second := readNativePointer(t, n.root)
	if second.Version == first.Version || second.PreviousVersion != first.Version {
		t.Fatal("real successor did not replace original file")
	}
	manifest := nativeManifest(t, n.root)
	published := 0
	for _, event := range publication.Events() {
		if event.Kind == "rename" && event.Stage == "publish.rename" {
			published++
		}
	}
	if published != 2 {
		t.Fatalf("independent real publications: %d", published)
	}

	// 查询授权和原核对身份在关闭前真实保存，不在恢复时补造新 authority。
	cap, grant := n.capGrant(t, "closing-history-query", "QUERY", a.ParametersRef)
	request := &v1.RequestReconciliationCommand{Header: executionHeader("closing-history-query"), OperationId: a.OperationId, QueryCapabilityRef: cap, ParametersRef: a.ParametersRef, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 0, InitialDelayMs: 10, MaxDelayMs: 100}}
	r, e = n.h.Ledger.RequestReconciliation(n.ctx, n.caller, request)
	requireAccepted(t, r, e)
	requestReceipt := proto.Clone(r).(*v1.CommandReceipt)
	plan, e := n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil || plan == nil || plan.State != "READY" || !proto.Equal(plan.RequestedBy, request.Header.Identity) {
		t.Fatalf("original query responsibility missing: %v %v", plan, e)
	}
	originalPlan := proto.Clone(plan).(*v1.Reconciliation)
	var cancelRef *v1.Ref
	if closingOutcome == "CANCELLED" {
		cancelRef = prepareClosingCancellation(t, n.h, n.ctx, n.caller, n.task.Name)
	}
	task, e := n.h.Tasks.QueryTask(n.ctx, n.caller, n.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = n.h.Tasks.BeginTaskClose(n.ctx, n.caller, &v1.BeginTaskCloseCommand{Header: admissionHeader("failed-file-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: closingOutcome, CloseReason: "UNABLE_TO_COMPLETE", CancellationRef: cancelRef})
	requireAccepted(t, r, e)
	if e = n.h.Tasks.ProcessTaskClosings(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	processFileClosingFollowups(t, n)
	view, e := n.h.Tasks.QueryTaskClosingView(n.ctx, n.caller, n.task.Name)
	if e != nil || view.Result == nil || view.Result.Outcome != closingOutcome || view.Result.VerificationRef != nil || len(view.Result.UnknownOperationRefs) != 1 || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 1 || len(view.Closing.AdmissionRefs) != 1 || !proto.Equal(view.Closing.AdmissionRefs[0], a.Ref) {
		t.Fatalf("FAILED discarded original file responsibility: %v %v", view, e)
	}
	if view.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || len(view.PendingClosureRefs) != 0 {
		t.Fatalf("FAILED did not actually close task: %v", view)
	}
	followup := proto.Clone(view.ExecutionFollowups[0]).(*v1.ExecutionFollowup)
	settlement := proto.Clone(view.SettlementFollowups[0]).(*v1.SettlementFollowup)
	if !proto.Equal(followup.OperationId, a.OperationId) || !proto.Equal(followup.AttemptRef, originalExecution.Attempt.Ref) || len(followup.SendRefs) != 1 || !proto.Equal(followup.SendRefs[0], originalExecution.Send.Ref) || followup.ExecutorEndpointId != "local-file" || len(settlement.BillingSourceRefs) != 1 || len(settlement.ReservationRefs) != 1 {
		t.Fatalf("original send or fee scope replaced: %v %v", followup, settlement)
	}
	assertFileClosingJobs(t, view, "WAITING", "COMPLETED")
	fixed, e := proto.Marshal(view.Result)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := n.h.Tasks.QueryTaskClosureIntent(n.ctx, n.caller, view.Closing.ClosureIntentRefs[0])
	if e != nil || intent.RecipientReceipt == nil {
		t.Fatalf("actual closing ACK missing: %v %v", intent, e)
	}
	seal, e := n.h.Ledger.QueryTaskClosureSeal(n.ctx, n.caller, intent.RecipientReceipt.ResultRef)
	if e != nil || seal.NoSendProven || !seal.PhysicalSendWasPossible || !proto.Equal(seal.ExecutionFollowupRef, followup.Ref) {
		t.Fatalf("P5 file publication called unsent: %v %v", seal, e)
	}
	assertFileClosingZeroSource(t, n, before)

	// 弱读取实际触及目标；零费用已结清不能把原 UNKNOWN 执行责任完成。
	weakReads := egressio.NewNativeFileRecorder(nil, nil)
	weak := egressio.WithNativeFileFault(context.Background(), &egressio.NativeFileFault{FailBefore: "query.history.object", Error: syscall.EIO, Recorder: weakReads})
	if e = n.h.Ledger.ProcessReconciliations(weak, n.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil || plan.State != "WAITING" || len(plan.QueryRefs) != 1 || !proto.Equal(plan.Ref.Name, originalPlan.Ref.Name) || !proto.Equal(plan.RequestedBy, originalPlan.RequestedBy) {
		t.Fatalf("weak query replaced/completed original plan: %v %v", plan, e)
	}
	processFileClosingFollowups(t, n)
	view, e = n.h.Tasks.QueryTaskClosingView(n.ctx, n.caller, n.task.Name)
	if e != nil || !proto.Equal(view.Operations[0].Execution, originalExecution) || view.Operations[0].Effect.Outcome != "UNKNOWN" {
		t.Fatalf("weak query changed original execution: %v %v", view, e)
	}
	assertFileClosingJobs(t, view, "WAITING", "COMPLETED")
	weakQuery := assertFileClosingQuery(t, n, plan.QueryRefs[0], a, originalExecution)
	weakRelation, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	weakRaw, e := n.h.Ledger.QueryObservation(n.ctx, n.caller, weakRelation.ObservationRef)
	if e != nil || weakRaw.FileEvidence.Stage != "ACCEPTED_HISTORY" || weakRaw.FileEvidence.ReadbackVerified || weakRaw.FileEvidence.ErrorCode != "FILE_NATIVE_IO_FAILED" {
		t.Fatalf("actual original history read fault not exercised: %v %v", weakRaw, e)
	}
	assertFileClosingZeroSource(t, n, weakQuery)
	weakReadCount := assertFileClosingReadsOnly(t, weakReads)

	// 原公开暂停使重启不抢先调度新查询；随后只恢复同一原计划。
	r, e = n.h.Ledger.ControlReconciliation(n.ctx, n.caller, &v1.ControlReconciliationCommand{Header: executionHeader("pause-closed-file-query"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
	requireAccepted(t, r, e)
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	n.ctx = context.Background()
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
	plan, e = n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil || plan.State != "PAUSED" || !proto.Equal(plan.Ref.Name, originalPlan.Ref.Name) || len(plan.QueryRefs) != 1 {
		t.Fatalf("restart replaced original plan: %v %v", plan, e)
	}
	r, e = n.h.Ledger.ControlReconciliation(n.ctx, n.caller, &v1.ControlReconciliationCommand{Header: executionHeader("resume-closed-file-query"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME"})
	requireAccepted(t, r, e)
	strongReads := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: strongReads})
	if e = n.h.Ledger.ProcessReconciliations(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || len(plan.QueryRefs) != 2 || !proto.Equal(plan.Ref.Name, originalPlan.Ref.Name) || !proto.Equal(plan.RequestedBy, originalPlan.RequestedBy) || !proto.Equal(plan.QueryCapabilityRef, cap) || !proto.Equal(plan.GrantRef, grant) {
		t.Fatalf("known historical query lost original plan: %v %v", plan, e)
	}
	strongQuery := assertFileClosingQuery(t, n, plan.QueryRefs[1], a, originalExecution)
	if proto.Equal(strongQuery.Ref.Name, weakQuery.Ref.Name) || proto.Equal(strongQuery.Execution.Attempt.Ref.Name, weakQuery.Execution.Attempt.Ref.Name) || proto.Equal(strongQuery.Execution.Send.Ref.Name, weakQuery.Execution.Send.Ref.Name) {
		t.Fatal("independent query send identities collapsed")
	}
	queryRelation, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, plan.QueryRefs[1])
	if e != nil {
		t.Fatal(e)
	}
	raw, e := n.h.Ledger.QueryObservation(n.ctx, n.caller, queryRelation.ObservationRef)
	if e != nil || raw.FileEvidence.Stage != "ACCEPTED_HISTORY" || !raw.FileEvidence.ReadbackVerified || !proto.Equal(raw.FileEvidence.Commit, first) || !proto.Equal(raw.OperationId, strongQuery.Ref.Name) {
		t.Fatalf("original history relabelled or not read: %v %v", raw, e)
	}
	assertFileClosingZeroSource(t, n, strongQuery)
	processFileClosingFollowups(t, n)
	view, e = n.h.Tasks.QueryTaskClosingView(n.ctx, n.caller, n.task.Name)
	if e != nil || view.Operations[0].Dispatch != "SEALED" || view.Operations[0].Effect.Outcome != "APPLIED" || !proto.Equal(view.Operations[0].Execution, originalExecution) || !proto.Equal(view.ExecutionFollowups[0], followup) || !proto.Equal(view.SettlementFollowups[0], settlement) {
		t.Fatalf("historical facts replaced original responsibility: %v %v", view, e)
	}
	assertFileClosingJobs(t, view, "COMPLETED", "COMPLETED")
	r, e = n.h.Ledger.RequestReconciliation(n.ctx, n.caller, request)
	requireAccepted(t, r, e)
	if !proto.Equal(r, requestReceipt) {
		t.Fatal("original reconciliation command receipt changed")
	}
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	if !proto.Equal(r, originalReceipt) {
		t.Fatal("original file send receipt changed")
	}
	strongReadCount := assertFileClosingReadsOnly(t, strongReads)
	currentResources, e := n.h.Content.QueryFileResources(n.ctx, n.caller, resources.Ref)
	if e != nil || !proto.Equal(currentResources, resources) {
		t.Fatalf("query removed/reassigned original file resources: %v %v", currentResources, e)
	}
	later, e := proto.Marshal(view.Result)
	if e != nil || !bytes.Equal(fixed, later) || !proto.Equal(second, readNativePointer(t, n.root)) || !reflect.DeepEqual(manifest, nativeManifest(t, n.root)) {
		t.Fatal("late query rewrote Result or actual target namespace/bytes/inode/mtime")
	}
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
	final, e := n.h.Tasks.QueryResult(n.ctx, n.caller, n.task.Name)
	if e != nil || !proto.Equal(final, view.Result) || !reflect.DeepEqual(manifest, nativeManifest(t, n.root)) {
		t.Fatalf("second restart changed fixed Result/target: %v %v", final, e)
	}
	assertTaskClosingFaultSources(t, n.h, n.ctx, n.caller, view)
	if closingOutcome == "CANCELLED" {
		assertClosingCancellation(t, n.h, n.ctx, n.caller, view.Closing, true)
	}
	t.Logf("actual publications=%d weak_reads=%d historical_reads=%d original_execution_unchanged=true result=%s/fixed original_unknown=1 final_original=APPLIED original_and_query_fees=0 separate_zero_sources=3 retained_original_resources=true plan=%s original_operation=%s attempt=%s send=%s queries=%d", published, weakReadCount, strongReadCount, closingOutcome, originalPlan.Ref.Name.LocalId, a.OperationId.LocalId, originalExecution.Attempt.Ref.Name.LocalId, originalExecution.Send.Ref.Name.LocalId, len(plan.QueryRefs))
}

func processFileClosingFollowups(t *testing.T, n *nativeScenario) {
	t.Helper()
	if e := n.h.Ledger.ProcessExecutionFollowups(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	if e := n.h.Budget.ProcessSettlementFollowups(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
}

func assertFileClosingJobs(t *testing.T, view *v1.TaskClosingView, execution, settlement string) {
	t.Helper()
	if len(view.FollowupJobs) != 2 {
		t.Fatalf("original followup owners missing: %v", view.FollowupJobs)
	}
	for _, job := range view.FollowupJobs {
		want := settlement
		if job.Module == "ledger" {
			want = execution
		}
		if job.State != want || (want == "WAITING" && job.WaitingReason == "") {
			t.Fatalf("original owner job state: %v want %s", job, want)
		}
	}
}

func assertFileClosingReadsOnly(t *testing.T, recorder *egressio.NativeFileRecorder) int {
	t.Helper()
	reads := 0
	for _, event := range recorder.Events() {
		if event.Kind != "read" && event.Kind != "error" {
			t.Fatalf("closed history query changed actual target: %v", event)
		}
		if event.Kind == "read" {
			reads++
		}
	}
	if reads == 0 {
		t.Fatal("original query never read actual file")
	}
	return reads
}

func assertFileClosingQuery(t *testing.T, n *nativeScenario, ref *v1.Ref, original *v1.Admission, execution *v1.Execution) *v1.Operation {
	t.Helper()
	q, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, ref)
	if e != nil || q.AdmissionReceipt == nil {
		t.Fatalf("actual query admission missing: %v %v", q, e)
	}
	a, e := n.h.Tasks.QueryAdmission(n.ctx, n.caller, q.AdmissionReceipt.ResultRef)
	if e != nil || a.WorkCategory != "CLOSURE" || !proto.Equal(a.TaskId, original.TaskId) || !proto.Equal(a.Origin, q.Work.Ref) {
		t.Fatalf("query lost independent original closure origin: %v %v", a, e)
	}
	op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, q.QueryOperationRef.Name)
	if e != nil || op.Execution == nil || proto.Equal(op.Ref.Name, original.OperationId) || !proto.Equal(op.QuerySubject.OperationId, original.OperationId) || !proto.Equal(op.QuerySubject.AttemptId, execution.Attempt.Ref.Name) || op.QuerySubject.ExternalKey != execution.Attempt.ExternalKey || op.QuerySubject.ExecutorEndpointId != original.ExecutorEndpointId || !proto.Equal(op.AdmissionRef, a.Ref) {
		t.Fatalf("query relabelled original operation/attempt/key/endpoint: %v %v", op, e)
	}
	return op
}

func assertFileClosingZeroSource(t *testing.T, n *nativeScenario, op *v1.Operation) {
	t.Helper()
	source, e := n.h.Budget.QueryBillingSource(n.ctx, n.caller, op.Execution.Send.Ref)
	if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 0 || !proto.Equal(source.OperationId, op.Ref.Name) || !proto.Equal(source.SendRef.Name, op.Execution.Send.Ref.Name) || !proto.Equal(source.AdmissionRef, op.AdmissionRef) {
		t.Fatalf("original zero fee source absent or relabelled: %v %v", source, e)
	}
	entry, e := n.h.Budget.QueryBillingEntry(n.ctx, n.caller, source.EntryRef)
	if e != nil || entry.MeasurementRule != "managed-file-zero-v1" || entry.PriceVersion != "managed-file-price-v1" || entry.Amount != 0 || !proto.Equal(entry.SourceRef.Name, source.Ref.Name) {
		t.Fatalf("zero fee inferred without actual protocol evidence: %v %v", entry, e)
	}
}
