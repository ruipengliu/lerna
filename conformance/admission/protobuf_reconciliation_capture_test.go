package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func captureReconciliation(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	g, err := f.h.Grants.QueryGrant(f.ctx, f.caller, grant)
	if err != nil {
		t.Fatal(err)
	}
	g.Ref, g.Issuer, g.Status, g.SemanticVersion = nil, nil, "", 0
	g.ConfirmationRequired = true
	r, err := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("capture-confirmed-query-grant"), Grant: g})
	accepted(t, r, err)
	grant = r.ResultRef
	a, start := prepareStart(t, f)
	r, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-original-unknown", before, err)
	if before.Effect.Outcome != "UNKNOWN" {
		t.Fatal("lost write not unknown")
	}
	request := reconciliationCommand(f, a, cap, grant)
	requested, err := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, requested, err)
	captureObject(t, samples, "reconciliation-request-command", request, nil)
	if err = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	waiting, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	captureObject(t, samples, "reconciliation-waiting-task", waiting, err)
	if waiting.Progress != v1.TaskProgress_TASK_PROGRESS_WAITING {
		t.Fatal("reconciliation did not wake task into waiting")
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-confirmation-required", plan, err)
	if plan.State != "PAUSED" || plan.PauseReason != "CONFIRMATION_REQUIRED" {
		t.Fatal(plan)
	}
	q, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.ActiveQueryRef)
	captureObject(t, samples, "reconciliation-query-before-approval", q, err)
	if q.AdmissionReceipt != nil || q.AdmissionCommand != nil {
		t.Fatal("query admitted without confirmation")
	}
	work, err := f.h.Ledger.QueryClosureWork(f.ctx, f.caller, q.Work.Ref)
	captureObject(t, samples, "closure-work", work, err)
	goal, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if err != nil {
		t.Fatal(err)
	}
	confirm := &v1.RequestClosureConfirmationCommand{Header: header("capture-closure-confirmation"), WorkRef: work.Ref, SessionId: goal.Receipt.SessionRef.Name}
	confirmationReceipt, err := f.h.Tasks.RequestClosureConfirmation(f.ctx, f.caller, confirm)
	accepted(t, confirmationReceipt, err)
	captureObject(t, samples, "closure-confirmation-command", confirm, nil)
	pending, err := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, confirmationReceipt.ResultRef)
	captureObject(t, samples, "closure-confirmation-pending", pending, err)
	if !proto.Equal(pending.GetOperationAdmission().QuerySubject, work.QuerySubject) {
		t.Fatal("confirmation not bound to original query subject")
	}
	approve := &v1.RespondConfirmationCommand{Header: header("capture-approve-closure"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
	approved, err := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, approve)
	accepted(t, approved, err)
	resume := &v1.ControlReconciliationCommand{Header: ledgerHeader("capture-resume-closure"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME", ConfirmationRef: approved.ResultRef}
	resumed, err := f.h.Ledger.ControlReconciliation(f.ctx, f.caller, resume)
	accepted(t, resumed, err)
	captureObject(t, samples, "reconciliation-control-command", resume, nil)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-completed", plan, err)
	if plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatal(plan)
	}
	q, err = f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	captureObject(t, samples, "reconciliation-query-completed", q, err)
	finding, err := f.h.Ledger.QueryReconciliationFinding(f.ctx, f.caller, q.InterpretationRef)
	captureObject(t, samples, "reconciliation-finding", finding, err)
	if finding.Outcome != "APPLIED" || finding.LateEffect != "RULED_OUT" {
		t.Fatal(finding)
	}
	qa, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, q.AdmissionReceipt.ResultRef)
	captureObject(t, samples, "closure-admission", qa, err)
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, qa.OperationId)
	captureObject(t, samples, "reconciliation-query-operation", query, err)
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-original-settled", original, err)
	if qa.WorkCategory != "CLOSURE" || query.Lifecycle != "SETTLED" || query.Execution.CallDescriptor.Method != "GET" || query.Execution.Attempt.ExternalKey == before.Execution.Attempt.ExternalKey || original.Effect.Outcome != "APPLIED" || !proto.Equal(original.Execution, before.Execution) {
		t.Fatal("query changed original execution identity or failed to settle independent responsibility")
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, q.ObservationRef)
	captureObject(t, samples, "reconciliation-query-observation", raw, err)
	if !proto.Equal(raw.OperationId, qa.OperationId) || !proto.Equal(raw.QuerySubject, work.QuerySubject) {
		t.Fatal("query observation relabeled as original")
	}
	reports, err := f.h.Ledger.QueryReports(f.ctx, f.caller, q.ObservationRef)
	captureObject(t, samples, "reconciliation-query-reports", reports, err)
	if reports.UsageReceipt == nil {
		t.Fatal("query has no independent billing receipt")
	}
	consumed, err := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, approved.ResultRef.Name)
	captureObject(t, samples, "closure-confirmation-consumed", consumed, err)
	if consumed.State != "CONSUMED" || !proto.Equal(consumed.GetConsumedAdmissionRef(), qa.Ref) {
		t.Fatal(consumed)
	}
	replay, err := f.h.Tasks.AdmitClosure(f.ctx, &v1.Caller{UserId: "u", IssuerId: "ledger-reconciliation"}, q.AdmissionCommand)
	if err != nil || !proto.Equal(replay, q.AdmissionReceipt) {
		t.Fatalf("closure replay: %v %v", replay, err)
	}
	captureObject(t, samples, "closure-admission-command", q.AdmissionCommand, nil)
	replay, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	if err != nil || !proto.Equal(replay, requested) {
		t.Fatalf("reconciliation replay: %v %v", replay, err)
	}
	replay, err = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, resume)
	if err != nil || !proto.Equal(replay, resumed) {
		t.Fatalf("control replay: %v %v", replay, err)
	}
	if err = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	notices, err := f.h.Ledger.QueryOperationProgress(f.ctx, f.caller, a.OperationId)
	if err != nil || len(notices) < 2 {
		t.Fatalf("progress: %v %v", notices, err)
	}
	for i, n := range notices {
		actual, err := f.h.Tasks.QueryOperationProgress(f.ctx, f.caller, n.Notice.Ref)
		if err != nil || n.RecipientReceipt == nil || !proto.Equal(actual, n.Notice) {
			t.Fatalf("progress receipt: %v %v", n, err)
		}
		if i == len(notices)-1 {
			captureObject(t, samples, "reconciliation-progress-handoff", n, nil)
		}
	}
	awake, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	captureObject(t, samples, "reconciliation-awake-task", awake, err)
	if awake.Progress != v1.TaskProgress_TASK_PROGRESS_RUNNING || len(awake.WaitingOn) != 0 {
		t.Fatal(awake)
	}
	old := notices[0]
	replay, err = f.h.Tasks.AcceptOperationProgress(f.ctx, &v1.Caller{UserId: "u", IssuerId: "ledger-progress"}, old.Command)
	if err != nil || !proto.Equal(replay, old.RecipientReceipt) {
		t.Fatalf("progress replay: %v %v", replay, err)
	}
	after, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if err != nil || !proto.Equal(after, awake) {
		t.Fatal("stale notice changed current task")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
		t.Fatalf("query or replay duplicated write: %v %v", requests, effects)
	}
	captureWeakReconciliation(t, samples)
}

func captureWeakReconciliation(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	request := reconciliationCommand(f, a, cap, grant)
	request.Limits.MaxChecks = 1
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-check-limit", plan, err)
	if plan.State != "PAUSED" || plan.PauseReason != "CHECK_LIMIT" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatal(plan)
	}
	q, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	captureObject(t, samples, "reconciliation-weak-query", q, err)
	finding, err := f.h.Ledger.QueryReconciliationFinding(f.ctx, f.caller, q.InterpretationRef)
	captureObject(t, samples, "reconciliation-weak-finding", finding, err)
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "reconciliation-weak-original", original, err)
	if original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatal("weak absence settled delayed write")
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 0 {
		t.Fatalf("paused query sent again: %v %v", requests, effects)
	}
}
