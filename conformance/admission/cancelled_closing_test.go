package admission_test

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/hosting"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G4、G11、R7、完成-4
func TestCLICancelledClosingRequiresBothOriginalCancellationAndClosingAcknowledgements(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, oldStart := prepareStart(t, f)
	cancelTask(t, f, "cancel-before-close")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || scope == nil {
		t.Fatal("missing independent cancellation", e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	cmd := &v1.BeginTaskCloseCommand{Header: header("cancelled-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
	body, e := protojson.Marshal(cmd)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "cancelled-close.json")
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Tasks: f.h.Tasks, Sessions: f.h.Sessions, Durable: f.h.Durable, Ledger: f.h.Ledger, Budget: f.h.Budget, Caller: f.caller, Domain: "d"}
	cli.Progress = newManualProgress(t, hosting.ManualDependencies{Sessions: f.h.Sessions, Reports: f.h.Ledger, Completions: f.h.Tasks, Cancellations: f.h.Tasks, Reconciliations: f.h.Ledger, TaskClosings: f.h.Tasks, BudgetClosures: f.h.Budget, ExecutionFollowups: f.h.Ledger, SettlementFollowups: f.h.Budget})
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"close-task", "--json", path}, &out); e != nil {
		t.Fatal(e)
	}
	receipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	accepted(t, receipt, nil)
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || !proto.Equal(view.Closing.CancellationRef, scope.Ref) || len(view.Closing.AdmissionRefs) != 1 || !proto.Equal(view.Closing.AdmissionRefs[0], a.Ref) || view.Result != nil {
		t.Fatalf("current independent closing missing %v %v", view, e)
	}
	// 新关闭的端点回执不能冒充原取消交接已由源负责方确认。
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result != nil {
		t.Fatalf("closing skipped original cancellation ACK %v %v", result, e)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, &out); e != nil {
		t.Fatal(e)
	}
	result, e = f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "CANCELLED" || result.VerificationRef != nil || len(result.OperationRefs) != 1 || len(result.UnknownOperationRefs) != 0 {
		t.Fatalf("cancelled Result missing %v %v", result, e)
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
	serialized, e := proto.Marshal(again)
	if e != nil || !bytes.Equal(fixed, serialized) {
		t.Fatal("cancelled Result changed on restart", e)
	}
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, oldStart)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || f.calls.Load() != 0 {
		t.Fatalf("cancelled old worker sent %v %v calls=%d", r, e, f.calls.Load())
	}
}

// 规则：G1、G2、G4、G5、G10、G11、R7、完成-4、完成-6
func TestCancelledClosingCoversTargetModelAndOriginalClosureAdmissions(t *testing.T) {
	target := simulator.NewBillingTarget(3)
	target.Target = simulator.New("queryable")
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	queryCapability, queryGrant := configureReconciliation(t, f)
	writeCapability, writeGrant := f.capability, f.grant
	provider := simulator.NewModelProvider()
	server := httptest.NewServer(provider)
	defer server.Close()
	modelCapability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	modelCapability.Ref, modelCapability.ApprovedBy = nil, nil
	modelCapability.Action, modelCapability.Resource, modelCapability.AdapterRef.Name.LocalId = "MODEL_INFER", server.URL, "model-reference-v1"
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("full-scope-model-cap"), Capability: modelCapability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref, grant.Issuer, grant.Status = nil, nil, ""
	grant.Permissions[0].Action, grant.Permissions[0].Resource, grant.UsePoolId = "MODEL_INFER", server.URL, "full-scope-model-pool"
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("full-scope-model-grant"), Grant: grant})
	accepted(t, r, e)
	f.grant, f.suffix = r.ResultRef, "-full-scope-model"
	run := modelRunCommand(t, f, 30000)
	model, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if e != nil || model.Status != "COMPLETED" || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("actual model missing %v %v", model, e)
	}
	f.capability, f.grant, f.suffix = writeCapability, writeGrant, ""
	a, start := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" {
		t.Fatal("original receipt loss did not retain unknown", e)
	}
	target.DropReceipt(false)
	target.Target.SetQueryBehavior("weak")
	request := reconciliationCommand(f, a, queryCapability, queryGrant)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.CheckCount != 1 || plan.State != "WAITING" {
		t.Fatalf("weak original plan missing %v %v", plan, e)
	}
	cancelTask(t, f, "full-scope-cancel")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || len(scope.AdmissionRefs) != 3 {
		t.Fatalf("original cancel omitted history %v %v", scope, e)
	}
	fixedScope := proto.Clone(scope)
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	// 原核对在当前取消控制下继续；新只读准入不能改写旧取消范围。
	target.Target.SetQueryBehavior("")
	time.Sleep(25 * time.Millisecond)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 2 || len(plan.QueryRefs) != 2 {
		t.Fatalf("current-control original query stranded %v %v", plan, e)
	}
	closingRef := beginCancelledClose(t, f, "full-scope-close")
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || len(view.Closing.AdmissionRefs) != 4 || len(view.ClosureSeals) != 4 || len(view.SettlementFollowups) != 4 || len(view.Operations) != 4 || !proto.Equal(view.Closing.Ref, closingRef) {
		t.Fatalf("current full historical scope omitted %v %v", view, e)
	}
	modelCount, closureCount, targetCount := 0, 0, 0
	for _, ref := range view.Closing.AdmissionRefs {
		admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, ref)
		if e != nil {
			t.Fatal(e)
		}
		switch {
		case admission.ModelDescriptorDigest != "":
			modelCount++
		case admission.WorkCategory == "CLOSURE":
			closureCount++
		default:
			targetCount++
		}
	}
	if modelCount != 1 || closureCount != 2 || targetCount != 1 {
		t.Fatalf("scope categories %d/%d/%d", modelCount, closureCount, targetCount)
	}
	now, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(now.Execution, original.Execution) || now.Effect.Outcome != "APPLIED" || now.Dispatch != "SEALED" {
		t.Fatal("original query changed original execution", e)
	}
	fixed, e := proto.Marshal(view.Result)
	if e != nil {
		t.Fatal(e)
	}
	evidence := stageBill(t, f, target.Bills()[0], "full-scope-original-late-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("full-scope-import"), SendRef: original.Execution.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
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
	for _, job := range view.FollowupJobs {
		if job.State != "COMPLETED" {
			t.Fatalf("original full-scope fee responsibility stranded %v", job)
		}
	}
	again, e := proto.Marshal(view.Result)
	if e != nil || !bytes.Equal(fixed, again) {
		t.Fatal("full-scope late bill rewrote Result", e)
	}
	currentScope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(currentScope, fixedScope) {
		t.Fatal("current close rewrote original cancellation", e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Settled != 16 || budget.Reserved != 0 {
		t.Fatalf("actual four send fees missing %v %v", budget, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 3 || requests[0].Method != "POST" || requests[1].Method != "GET" || requests[2].Method != "GET" || len(effects) != 1 || len(target.Bills()) != 3 || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatal("full historical closing duplicated independent target/model effects")
	}
	assertClosingTraceSources(t, f, view)
	t.Log("original_cancel_admissions=3 current_closing_admissions=4 TARGET=1 MODEL=1 CLOSURE=2 POST=1 GET=2 effects=1 model_calls=1 model_bills=1 total_fee=16")
}

func beginCancelledClose(t *testing.T, f *fixture, id string) *v1.Ref {
	t.Helper()
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || scope == nil {
		t.Fatal("missing original cancellation", e)
	}
	r, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header(id), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref})
	accepted(t, r, e)
	return r.ResultRef
}

func beginNonSuccessClose(t *testing.T, f *fixture, id, outcome, reason string) (*v1.CommandReceipt, error) {
	t.Helper()
	var cancellationRef *v1.Ref
	if outcome == "CANCELLED" {
		cancelTask(t, f, "cancel:"+id)
		scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
		if e != nil || scope == nil || len(scope.AdmissionRefs) != 1 || len(scope.ClosureIntentRefs) != 1 {
			t.Fatal("original cancellation scope missing", e)
		}
		cancellationRef = scope.Ref
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		return nil, e
	}
	return f.h.Tasks.BeginTaskClose(f.ctx, f.caller, &v1.BeginTaskCloseCommand{Header: header(id), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: outcome, CloseReason: reason, CancellationRef: cancellationRef})
}

func processNonSuccessClosings(f *fixture, outcome string) error {
	if outcome == "CANCELLED" {
		if e := f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
			return e
		}
	}
	return f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller)
}

// 规则：G2、G3、G6、G11、R7、完成-4
func TestCancelledClosingRejectsForgedBasisAndFencesLaterInput(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	prepareStart(t, f)
	cancelTask(t, f, "guard-current-cancel")
	mutateCompletionBasis(t, f, "MODIFY")
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	valid := &v1.BeginTaskCloseCommand{Header: header("guard-current-close"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
	for _, kind := range []string{"missing", "foreign", "schema", "revision", "generation", "untrusted"} {
		c := proto.Clone(valid).(*v1.BeginTaskCloseCommand)
		c.Header = header("invalid-close-" + kind)
		caller := f.caller
		switch kind {
		case "missing":
			c.CancellationRef = nil
		case "foreign":
			c.CancellationRef.Name.LocalId = "other-original-cancellation"
		case "schema":
			c.CancellationRef.SchemaId = "lerna.v1.Verification"
		case "revision":
			c.CancellationRef.Revision++
		case "generation":
			c.ExpectedControlGeneration--
		case "untrusted":
			caller = &v1.Caller{UserId: "u", IssuerId: "reasoner"}
			c.Header.Identity.IssuerId = caller.IssuerId
		}
		r, e := f.h.Tasks.BeginTaskClose(f.ctx, caller, c)
		if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
			t.Fatalf("forged %s closing accepted %v %v", kind, r, e)
		}
	}
	r, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, valid)
	accepted(t, r, e)
	closing := r.ResultRef
	current, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	q, e := f.h.Sessions.PublishQuestion(f.ctx, f.caller, &v1.PublishQuestionCommand{Header: header("closing-question"), SessionId: goal.Receipt.SessionRef.Name, TaskRef: &v1.Ref{Name: current.TaskId, Revision: current.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: f.parameters, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
	accepted(t, q, e)
	for _, kind := range []string{"MODIFY", "ANSWER", "CONTROL"} {
		c := &v1.SubmitInputCommand{Header: header("input-after-close-" + kind), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: kind, ContentRef: f.parameters, ExpectedInputVersion: current.InputVersion, ExpectedRequirementsVersion: current.RequirementsVersion}
		if kind == "ANSWER" {
			c.RequestRef = q.ResultRef
		}
		if kind == "CONTROL" {
			c.Control, c.ExpectedControlGeneration = "CANCEL", current.ControlGeneration
			c.ContentRef = nil
			c.ExpectedInputVersion, c.ExpectedRequirementsVersion = 0, 0
		}
		rejected, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, c)
		if e != nil || rejected.GetError().GetCode() != "TASK_CLOSING" {
			t.Fatalf("%s changed saved close basis %v %v", kind, rejected, e)
		}
	}
	rejected, e := f.h.Tasks.ProcessInput(f.ctx, f.caller, &v1.ProcessInputCommand{Header: header("process-after-cancelled-close"), TaskRef: &v1.Ref{Name: current.TaskId, Revision: current.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: current.BoundInputVersion + 1, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"})
	if e != nil || rejected.GetError().GetCode() != "TASK_CLOSING" {
		t.Fatal("trusted processing changed current close basis", e)
	}
	rejected, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements-after-close"), TaskRef: &v1.Ref{Name: current.TaskId, Revision: current.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: current.InputVersion, Source: "TRUSTED_TEMPLATE"})
	if e != nil || rejected.GetError().GetCode() != "TASK_CLOSING" {
		t.Fatal("trusted requirements changed current close basis", e)
	}
	again, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, valid)
	accepted(t, again, e)
	if !proto.Equal(r, again) {
		t.Fatal("accepted original Begin reinterpreted")
	}
	changed := proto.Clone(valid).(*v1.BeginTaskCloseCommand)
	changed.ExpectedControlGeneration = current.ControlGeneration
	if _, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, changed); e == nil || e.Error() != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("same identity upgraded its original semantic decision")
	}
	if e = processNonSuccessClosings(f, "CANCELLED"); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || !proto.Equal(view.Result.TaskClosingRef, closing) || view.Result.Conditions[0].Conclusion != "UNKNOWN" || view.Result.Conditions[0].Gap != "REQUIREMENTS_STALE_INPUT" || f.calls.Load() != 0 {
		t.Fatalf("current unbound input was falsely completed %v %v", view, e)
	}
}

// 规则：G2、G3、G4、G11、R7、完成-4
func TestCancelledClosingViewQualifiesTombstoneAndWaitsForOriginalAdmission(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("closing-awaiting-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	cancelTask(t, f, "closing-awaiting-cancel")
	beginCancelledClose(t, f, "closing-awaiting-begin")
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || len(view.AwaitingOperationAdmissionRefs) != 1 || !proto.Equal(view.AwaitingOperationAdmissionRefs[0], a.Ref) || len(view.ClosureIntents) != 1 || len(view.PendingClosureRefs) != 1 || len(view.Operations) != 0 || len(view.ClosureSeals) != 0 {
		t.Fatalf("pending original admission hidden %v %v", view, e)
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result != nil || len(view.PendingClosureRefs) != 0 || len(view.AwaitingOperationAdmissionRefs) != 1 || len(view.ClosureSeals) != 1 || len(view.Operations) != 0 {
		t.Fatalf("tombstone fabricated completed operation %v %v", view, e)
	}
	seal := view.ClosureSeals[0]
	if !seal.NoSendProven || seal.PhysicalSendWasPossible || seal.OperationRef != nil || !proto.Equal(seal.AdmissionRef, a.Ref) || !proto.Equal(seal.Ref, view.ClosureIntents[0].RecipientReceipt.ResultRef) {
		t.Fatal("missing actual original no-send tombstone")
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || view.Result.Outcome != "CANCELLED" || len(view.AwaitingOperationAdmissionRefs) != 0 || len(view.Operations) != 1 || view.Operations[0].Dispatch != "SEALED" || view.Operations[0].Effect.Outcome != "NOT_APPLIED" || view.Operations[0].Execution != nil {
		t.Fatalf("late original handoff escaped closing %v %v", view, e)
	}
	oldLedger := f.h.Ledger
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	// 源事实仍可读；真正关闭的原执行管理连接不能被解释为空清单。
	f.h.Tasks.WithCompletion(oldLedger, f.h.Budget)
	cli := interaction.CLI{Tasks: f.h.Tasks, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"task-closing", f.task.Name.LocalId}, &out); e == nil || out.Len() != 0 {
		t.Fatal("actual original owner read failure looked healthy")
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatal("tombstone or public view sent target")
	}
}

// 规则：G2、G3、G6、G11、完成-2、完成-4
func TestCancelledClosingRejectsOldBeginButAllowsFreshTrustedBasisAfterInput(t *testing.T) {
	for _, kind := range []string{"NONBASIS", "MODIFY", "REQUIREMENTS"} {
		for _, acknowledgeBefore := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/ack-after", true: "/ack-before"}[acknowledgeBefore], func(t *testing.T) {
				f := newFixture(t, 100, 80, false)
				prepareStart(t, f)
				cancelTask(t, f, "cancel-changing-basis")
				if acknowledgeBefore {
					if e := f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
						t.Fatal(e)
					}
				}
				scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				originalScope := proto.Clone(scope)
				old, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				prepared := &v1.BeginTaskCloseCommand{Header: header("prepared-old-close"), TaskRef: &v1.Ref{Name: old.TaskId, Revision: old.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: old.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
				mutateCompletionBasis(t, f, kind)
				current, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil || current.Revision <= old.Revision || current.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || kind == "NONBASIS" && (current.InputVersion != old.InputVersion+1 || current.ControlGeneration != old.ControlGeneration) {
					t.Fatalf("public input did not create actual new basis %v %v", current, e)
				}
				if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
				if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
				result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
				if e != nil || result != nil {
					t.Fatal("old ACK created a new closing basis", e)
				}
				r, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, prepared)
				if e != nil || r.GetError().GetCode() != "STALE_GENERATION" {
					t.Fatalf("old prepared Begin decided new basis %v %v", r, e)
				}
				current, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				fresh := proto.Clone(prepared).(*v1.BeginTaskCloseCommand)
				fresh.Header = header("explicit-current-close")
				fresh.TaskRef.Revision, fresh.ExpectedControlGeneration = current.Revision, current.ControlGeneration
				r, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, fresh)
				accepted(t, r, e)
				closing, e := f.h.Tasks.QueryTaskClosing(f.ctx, f.caller, r.ResultRef)
				if e != nil || closing.InputVersion != current.InputVersion || closing.RequirementsVersion != current.RequirementsVersion || !proto.Equal(closing.RequirementsRef, planning.Requirements.Ref) || closing.ControlGeneration != current.ControlGeneration+1 || !proto.Equal(closing.CancellationRef, scope.Ref) {
					t.Fatalf("fresh explicit basis not fixed %v %v", closing, e)
				}
				if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
				result, e = f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
				if e != nil || result == nil || result.Outcome != "CANCELLED" || !proto.Equal(result.TaskClosingRef, closing.Ref) {
					t.Fatal("fresh current closing stranded", e)
				}
				again, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
				if e != nil || !proto.Equal(again, originalScope) || f.calls.Load() != 0 {
					t.Fatal("new close upgraded original cancellation or sent target", e)
				}
				t.Logf("kind=%s original_cancel_gen=%d current_basis_gen=%d input=%d/%d current_requirement=%v calls=0", kind, scope.ControlGeneration, closing.ControlGeneration, old.InputVersion, closing.InputVersion, closing.RequirementsRef)
			})
		}
	}
}

// 规则：G2、G3、G4、G11、R7、完成-4
func TestCancelledClosingCannotFinalizeWhenOriginalCancellationSealReadFails(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	prepareStart(t, f)
	cancelTask(t, f, "read-failure-cancel")
	if e := f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	// 从实际同库原执行管理取得端口，再关闭它；当前源负责方保持可读。
	shadow, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	deadLedger := shadow.Ledger
	if e = shadow.Close(); e != nil {
		t.Fatal(e)
	}
	beginCancelledClose(t, f, "read-failure-begin")
	f.h.Tasks.WithTaskClosingFacts(deadLedger, f.h.Budget)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e == nil {
		t.Fatal("actual cancellation seal read failure permitted finalization")
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result != nil {
		t.Fatalf("missing owner facts appeared final %v %v", result, e)
	}
	f.h.Tasks.WithTaskClosingFacts(f.h.Ledger, f.h.Budget)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e = f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result.GetOutcome() != "CANCELLED" || f.calls.Load() != 0 {
		t.Fatalf("restored original owner failed closing %v %v", result, e)
	}
}
