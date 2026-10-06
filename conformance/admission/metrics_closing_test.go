package admission_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G10、G11、R3、R7、V4
func TestClosedUnknownMetricsAndStoppedBackupRetainOriginalResponsibilities(t *testing.T) {
	for _, outcome := range []string{"FAILED", "CANCELLED"} {
		t.Run(outcome, func(t *testing.T) {
			target := simulator.NewBillingTarget(3)
			target.Target = simulator.New("queryable")
			target.WithholdBill(true)
			f := newFixtureWithTarget(t, 200, 200, false, target)
			capability, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			late := performWithoutObservation(t, f, a, start)
			r, err := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, capability, grant))
			accepted(t, r, err)
			closeReceipt, err := beginNonSuccessClose(t, f, "metric-original-close", outcome, "USER_STOPPED")
			accepted(t, closeReceipt, err)
			if err = processNonSuccessClosings(f, outcome); err != nil {
				t.Fatal(err)
			}
			view, err := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
			if err != nil || view.Task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || view.Result.GetOutcome() != outcome || len(view.Result.UnknownOperationRefs) != 1 || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 1 || len(view.FollowupJobs) != 2 {
				t.Fatalf("real closed UNKNOWN population: %v %v", view, err)
			}
			fixed, err := proto.Marshal(view.Result)
			if err != nil {
				t.Fatal(err)
			}
			original, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			admissionReceipt, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("admit").Identity)
			if err != nil {
				t.Fatal(err)
			}
			body, err := f.h.Content.Read(f.ctx, f.caller, f.parameters)
			if err != nil {
				t.Fatal(err)
			}
			egressCaller := &v1.Caller{UserId: "u", IssuerId: "egress"}
			dispatchIdentity := &v1.CommandIdentity{UserId: "u", IssuerId: "egress", TargetDomainId: "d/ledger", CommandId: "dispatch:" + original.Send.Ref.Name.LocalId}
			dispatchReceipt, err := f.h.Egress.QueryReceipt(f.ctx, egressCaller, dispatchIdentity)
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range view.FollowupJobs {
				if job.State != "WAITING" {
					t.Fatalf("UNKNOWN responsibility missing: %v", job)
				}
			}
			assertClosedMetricsReadOnly(t, f, a, 1, &v1.ReconciliationMetrics{Total: 1, Active: 1, Ready: 1, Unfinished: 1, UnknownCovered: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			originalPlan := proto.Clone(plan).(*v1.Reconciliation)
			r, err = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metric-closed-pause"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
			accepted(t, r, err)
			assertClosedMetricsReadOnly(t, f, a, 1, &v1.ReconciliationMetrics{Total: 1, Paused: 1, Unfinished: 1, UnknownCovered: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			paused, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			sourcePath := f.path
			backupDirectory := t.TempDir()
			manifest, err := f.h.CloseAndBackup(f.ctx, backupDirectory)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("actual stopped storage=%+v files=%+v", manifest.Environment, manifest.Files)
			stopped := stoppedFileSet(t, sourcePath)
			destination := t.TempDir()
			restored, err := assembly.RestoreBackup(f.ctx, backupDirectory, destination, "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			f.h, f.path = restored, filepath.Join(destination, "state.db")
			assertStoppedFileSet(t, sourcePath, stopped)
			restoredView, err := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
			if err != nil || !proto.Equal(restoredView.Closing, view.Closing) || !proto.Equal(restoredView.ClosureIntents[0], view.ClosureIntents[0]) || !proto.Equal(restoredView.ClosureSeals[0], view.ClosureSeals[0]) || !proto.Equal(restoredView.ExecutionFollowups[0], view.ExecutionFollowups[0]) || !proto.Equal(restoredView.SettlementFollowups[0], view.SettlementFollowups[0]) {
				t.Fatalf("restore changed original fixed responsibility: %v %v", restoredView, err)
			}
			plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil || !proto.Equal(plan, paused) || !proto.Equal(plan.Ref.Name, originalPlan.Ref.Name) || !proto.Equal(plan.RequestedBy, originalPlan.RequestedBy) {
				t.Fatalf("restore replaced original paused plan: %v %v", plan, err)
			}
			actual, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil || !proto.Equal(actual, original) {
				t.Fatalf("restore replaced original execution: %v %v", actual, err)
			}
			closeAfter, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, closeReceipt.Identity)
			if err != nil || !proto.Equal(closeAfter.GetReceipt(), closeReceipt) {
				t.Fatalf("restore replaced original closing receipt: %v %v", closeAfter, err)
			}
			admissionAfter, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, admissionReceipt.Receipt.Identity)
			if err != nil || !proto.Equal(admissionAfter.GetReceipt(), admissionReceipt.Receipt) {
				t.Fatalf("restore replaced original admission receipt: %v %v", admissionAfter, err)
			}
			bodyAfter, err := f.h.Content.Read(f.ctx, f.caller, f.parameters)
			if err != nil || !proto.Equal(bodyAfter, body) {
				t.Fatalf("restore replaced original content: %v %v", bodyAfter, err)
			}
			r, err = f.h.Egress.Invoke(f.ctx, egressCaller, start)
			accepted(t, r, err)
			if !proto.Equal(r, dispatchReceipt.Receipt) {
				t.Fatal("restored original command changed its dispatch receipt")
			}
			requests, effects := target.Target.Snapshot()
			if len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 1 || len(target.Bills()) != 1 {
				t.Fatal("restore/replay added a business send or rolled back the original target")
			}
			var output bytes.Buffer
			if err = (interaction.CLI{Metrics: f.h, Caller: f.caller, Domain: "d"}).Run(f.ctx, []string{"metrics"}, &output); err != nil {
				t.Fatal(err)
			}
			local := new(v1.LocalMetrics)
			if err = protojson.Unmarshal(output.Bytes(), local); err != nil || local.CrossDomainAtomic || local.Ledger.Unknown.Total != 1 || local.Ledger.Reconciliation.Paused != 1 || local.Trace.Diagnostics.Source.Availability != "DISABLED" || local.Trace.Diagnostics.Dropped != nil || local.Trace.Diagnostics.StalenessMs != nil {
				t.Fatalf("closed UNKNOWN CLI metric/missing boundary: %v %v", local, err)
			}
			assertClosedMetricsReadOnly(t, f, a, 1, &v1.ReconciliationMetrics{Total: 1, Paused: 1, Unfinished: 1, UnknownCovered: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			r, err = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metric-closed-resume"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME"})
			accepted(t, r, err)
			target.WithholdBill(false)
			target.Target.SetQueryBehavior("weak")
			if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			assertClosedMetricsReadOnly(t, f, a, 1, &v1.ReconciliationMetrics{Total: 1, Waiting: 1, Unfinished: 1, UnknownCovered: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil || len(plan.QueryRefs) != 1 {
				t.Fatalf("real weak query missing: %v %v", plan, err)
			}
			query, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if err != nil || query.Work == nil || !proto.Equal(query.Work.OwnerRef.Name, originalPlan.Ref.Name) || !proto.Equal(query.Work.QuerySubject.AttemptId, original.Attempt.Ref.Name) || query.Work.QuerySubject.ExternalKey != original.Attempt.ExternalKey || query.AdmissionCommand == nil || query.AdmissionReceipt == nil || query.ObservationRef == nil {
				t.Fatalf("query replaced original plan or skipped real admission: %v %v", query, err)
			}
			evidence := stageBill(t, f, target.Bills()[0], "metric-original-late-bill")
			bill := &v1.ImportBillCommand{Header: header("metric-original-bill-import"), SendRef: original.Send.Ref, EvidenceRef: evidence}
			r, err = f.h.Budget.ImportBill(f.ctx, f.caller, bill)
			accepted(t, r, err)
			assertClosedMetricsReadOnly(t, f, a, 1, &v1.ReconciliationMetrics{Total: 1, Waiting: 1, Unfinished: 1, UnknownCovered: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			// 原费用到齐仍不证明原效果；原实际回报由自己的负责方处理。
			savePhysicalObservation(t, f, late)
			assertClosedMetricsReadOnly(t, f, a, 0, &v1.ReconciliationMetrics{Total: 1, Completed: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			if err = f.h.Ledger.ProcessExecutionFollowups(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			if err = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			assertClosedMetricsReadOnly(t, f, a, 0, &v1.ReconciliationMetrics{Total: 1, Completed: 1, DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}, fixed)
			finalView, err := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range finalView.FollowupJobs {
				if job.State != "COMPLETED" {
					t.Fatalf("original owner followup not complete: %v", job)
				}
			}
			r, err = f.h.Budget.ImportBill(f.ctx, f.caller, bill)
			accepted(t, r, err)
			duplicate := proto.Clone(bill).(*v1.ImportBillCommand)
			duplicate.Header = header("metric-original-bill-duplicate")
			r, err = f.h.Budget.ImportBill(f.ctx, f.caller, duplicate)
			accepted(t, r, err)
			budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
			if err != nil || budget.Settled != 6 || budget.Reserved != 0 {
				t.Fatalf("original and separate query fee/dedup: %v %v", budget, err)
			}
			current, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil || current.Attempt.Phase != "OBSERVED" || current.Send.Phase != "OBSERVED" || current.Attempt.Ref.Revision != original.Attempt.Ref.Revision+1 || current.Send.Ref.Revision != original.Send.Ref.Revision+1 || !proto.Equal(current.Send.ObservationRef.Name, late.Observation.Ref.Name) {
				t.Fatalf("original late observation was not accepted: %v %v", current, err)
			}
			// 原回报只推进原尝试、发送的阶段和版本；完整身份、开始回执、描述符与费用依据不变。
			unchanged := proto.Clone(current).(*v1.Execution)
			unchanged.Attempt.Phase, unchanged.Attempt.Ref.Revision = original.Attempt.Phase, original.Attempt.Ref.Revision
			unchanged.Send.Phase, unchanged.Send.Ref.Revision = original.Send.Phase, original.Send.Ref.Revision
			if !proto.Equal(unchanged, original) {
				t.Fatalf("late facts replaced original execution identity or binding: %v", current)
			}
			requests, effects = target.Target.Snapshot()
			if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 || len(target.Bills()) != 2 {
				t.Fatal("metrics/late facts introduced another physical send or query effect")
			}
			assertStoppedFileSet(t, sourcePath, stopped)
			t.Logf("closed=%s originalUNKNOWN=1 currentUNKNOWN=0 originalPlan=1 completed=1 unfinished=0 independentPOST=1 GET=1 effects=1 bills=2 settled=6 reserved=0 fixedResult=true originalExecutionIdentity=true originalObservationAccepted=true sourceNeverResumed=true duration=MISSING", outcome)
		})
	}
}

// assertClosedMetricsReadOnly 对比实际原负责方快照，查询不能代替业务推进。
func assertClosedMetricsReadOnly(t *testing.T, f *fixture, a *v1.Admission, unknown uint64, expected *v1.ReconciliationMetrics, fixed []byte) {
	t.Helper()
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	metric, err := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if err != nil || metric.Source.Scope != "ALL_OPERATIONS_INCLUDING_CLOSED_TASKS" || metric.Source.OwnerDomainId != "d/ledger" || metric.Source.DefinitionVersion != "lerna.m1.ledger.v1" || metric.Source.CapturedAtUnixMs <= 0 || metric.Source.SourceRevision == nil || metric.Unknown.Total != unknown || metric.UnknownBusinessOperations != unknown || metric.UnknownQueryOperations != 0 || !proto.Equal(metric.Reconciliation, expected) {
		t.Fatalf("actual closed owner metric: %v %v expected=%v", metric, err, expected)
	}
	if unknown == 1 && (metric.Unknown.AgeSamples != 1 || metric.Unknown.OldestAgeMs == nil || metric.Unknown.AgeMissing != 0 || metric.Unknown.AgeClockInvalid != 0) {
		t.Fatalf("closed original age became missing or healthy zero: %v", metric.Unknown)
	}
	again, err := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if err != nil || metric.Source.GetSourceRevision() != again.Source.GetSourceRevision() {
		t.Fatalf("read-only metrics advanced source revision: %v %v", again, err)
	}
	afterOp, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(afterOp, op) {
		t.Fatalf("metrics interpreted original effect: %v %v", afterOp, err)
	}
	afterPlan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(afterPlan, plan) {
		t.Fatalf("metrics advanced original plan: %v %v", afterPlan, err)
	}
	afterBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if err != nil || !proto.Equal(afterBudget, budget) {
		t.Fatalf("metrics settled original bill: %v %v", afterBudget, err)
	}
	result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	bytesAfter, marshalErr := proto.Marshal(result)
	if err != nil || marshalErr != nil || !bytes.Equal(fixed, bytesAfter) {
		t.Fatalf("metrics rewrote fixed Result: %v %v", err, marshalErr)
	}
}
