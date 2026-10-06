package admission_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、R7
func TestCancellationWithoutAdmissionsDoesNotLeavePhantomClosureWait(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	cancelTask(t, f, "cancel-empty")
	if e := f.h.Close(); e != nil {
		t.Fatal(e)
	}
	var e error
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || scope == nil || len(scope.AdmissionRefs) != 0 {
		t.Fatalf("empty cancellation lost %v %v", scope, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || slices.Contains(task.WaitingOn, "CANCELLATION_CLOSURE") {
		t.Fatalf("phantom closure wait %v %v", task, e)
	}
}

// 规则：G1、G3、G10、G11、R7
func TestCancellationCLISeparatesSavedControlEndpointClosureAndUnknownEffect(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	cancelTask(t, f, "cancel-cli")
	cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Budget: f.h.Budget, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &out); e != nil {
		t.Fatalf("missing cancellation view: %v", e)
	}
	var view struct {
		Task struct {
			Control string `json:"control"`
		} `json:"task"`
		Pending []json.RawMessage `json:"pendingClosureRefs"`
		Unknown []json.RawMessage `json:"unresolvedEffectOperationRefs"`
	}
	if e = json.Unmarshal(out.Bytes(), &view); e != nil || view.Task.Control != "TASK_CONTROL_CANCELLING" || len(view.Pending) != 1 || len(view.Unknown) != 1 {
		t.Fatalf("misleading pending view %s %v", out.String(), e)
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(out.Bytes(), &view); e != nil || len(view.Pending) != 0 || len(view.Unknown) != 1 {
		t.Fatalf("endpoint seal claimed known effect %s %v", out.String(), e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("lost possible send %v %v", op, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 30 || b.Settled != 0 {
		t.Fatalf("cancel invented zero fee %v %v", b, e)
	}
	evidence := stageBill(t, f, target.Bills()[0], "cancel-cli-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("cancel-cli-import"), SendRef: op.Execution.Send.Ref, EvidenceRef: evidence})
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Settled != 25 || b.Reserved != 0 || len(target.Bills()) != 1 {
		t.Fatalf("late fee %v %v", b, e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"budget", f.task.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	shown := new(v1.Budget)
	if e = protojson.Unmarshal(out.Bytes(), shown); e != nil || shown.Settled != 25 || shown.Reserved != 0 {
		t.Fatalf("late fee hidden in CLI %s %v", out.String(), e)
	}
	sendJSON, e := protojson.Marshal(op.Execution.Send.Ref)
	if e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"billing-source", string(sendJSON)}, &out); e != nil {
		t.Fatal(e)
	}
	shownSource := new(v1.BillingSource)
	if e = protojson.Unmarshal(out.Bytes(), shownSource); e != nil || shownSource.Amount == nil || *shownSource.Amount != 25 || !proto.Equal(shownSource.SendRef.Name, op.Execution.Send.Ref.Name) {
		t.Fatalf("late original source hidden in CLI %s %v", out.String(), e)
	}
}

// 规则：G1、G3、G4、G10、G11、R7、开始-2
func TestCancellationEndpointAcknowledgementClosesOldWorkerBeforeP5(t *testing.T) {
	for _, afterP4 := range []bool{false, true} {
		t.Run(fmt.Sprint(afterP4), func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			a, start := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			if afterP4 {
				r, e := f.h.Tasks.StartExecution(f.ctx, actor, start)
				accepted(t, r, e)
			}
			cancelTask(t, f, "cancel-before-send")
			if e := f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			intent, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
			if e != nil || intent.RecipientReceipt == nil {
				t.Fatalf("missing endpoint acknowledgement %v %v", intent, e)
			}
			seal, e := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
			if e != nil || !seal.NoSendProven || seal.PhysicalSendWasPossible || len(seal.ClosedSendRefs) != 1 || !proto.Equal(seal.CancellationRef, scope.Ref) {
				t.Fatalf("no-send proof %v %v", seal, e)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Execution.Send.Phase != "CLOSED" {
				t.Fatalf("unsealed old operation %v %v", op, e)
			}
			if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
				t.Fatal(e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || budget.Reserved != 0 || budget.Settled != 0 {
				t.Fatalf("proved unused reservation %v %v", budget, e)
			}
			r, e := f.h.Egress.Invoke(f.ctx, actor, start)
			if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || f.calls.Load() != 0 {
				t.Fatalf("paused old worker resumed %v %v calls=%d", r, e, f.calls.Load())
			}
		})
	}
}

// 规则：G3、G4、G11、R7、开始-2
func TestCancellationSavesExactClosureResponsibilityWithControl(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, start := prepareStart(t, f)
	control := cancelTask(t, f, "cancel-scope")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || scope == nil || !proto.Equal(scope.ControlIdentity, control.Identity) || scope.ControlGeneration != a.ControlGeneration+1 || len(scope.AdmissionRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], a.Ref) || len(scope.ClosureIntentRefs) != 1 {
		t.Fatalf("cancel scope %v %v", scope, e)
	}
	intent, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if e != nil || intent == nil || intent.RecipientReceipt != nil || !proto.Equal(intent.Command.AdmissionRef, a.Ref) || intent.Command.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("closure intent %v %v", intent, e)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, intent.JobRef.Name)
	if e != nil || job.JobType != "DELIVER_CANCELLATION_CLOSURE" || job.State != "READY" || !proto.Equal(job.Responsibility, intent.Command.Header.Identity) {
		t.Fatalf("closure job %v %v", job, e)
	}
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || f.calls.Load() != 0 {
		t.Fatalf("old start after cancel %v %v calls=%d", r, e, f.calls.Load())
	}
	state, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || state.VerificationRound != 0 {
		t.Fatalf("cancel invented verification %v %v", state, e)
	}
}

func cancelTask(t *testing.T, f *fixture, id string) *v1.CommandReceipt {
	t.Helper()
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header(id), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration})
	accepted(t, r, e)
	return r
}

// 规则：G3、G4、G11、R7
func TestCancellationTombstoneCoversOriginalAdmissionDeliveredLate(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("late-cancel-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	control := cancelTask(t, f, "cancel-before-delivery")
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	seal, e := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
	if e != nil || !seal.NoSendProven || seal.OperationRef != nil {
		t.Fatalf("missing late-admission tombstone %v %v", seal, e)
	}
	r, e = f.h.Budget.ReleaseUnused(f.ctx, f.caller, &v1.ReleaseReservationCommand{Header: header("release-late-cancel"), ReservationRef: a.BudgetBasis.ReservationRef, ClosureRef: seal.Ref})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Execution != nil {
		t.Fatalf("late intent escaped %v %v", op, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("cancelled-new-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("cancelled admission %v %v", r, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, control.Identity)
	if e != nil || !proto.Equal(q.Receipt, control) {
		t.Fatalf("control receipt rewritten %v %v", q, e)
	}
	jobs, e := f.h.Ledger.QueryJobs(f.ctx, f.caller, a.OperationId)
	if e != nil || len(jobs) != 1 || jobs[0].State != "COMPLETED" || f.calls.Load() != 0 {
		t.Fatalf("late admission scheduled IO %v %v calls=%d", jobs, e, f.calls.Load())
	}
}

// 规则：G3、G4、G10、G11、R7
func TestCancellationCLIShowsSealedAdmissionAwaitingOriginalOperation(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("view-original-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	cancelTask(t, f, "view-before-original-delivery")
	cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Caller: f.caller, Domain: "d"}
	read := func() struct {
		Pending, Operations, Unknown, Intents, Seals, Awaiting []json.RawMessage
	} {
		t.Helper()
		var out bytes.Buffer
		if e := cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &out); e != nil {
			t.Fatal(e)
		}
		var fields map[string]json.RawMessage
		if e := json.Unmarshal(out.Bytes(), &fields); e != nil {
			t.Fatal(e)
		}
		var view struct {
			Pending, Operations, Unknown, Intents, Seals, Awaiting []json.RawMessage
		}
		for field, dest := range map[string]*[]json.RawMessage{"pendingClosureRefs": &view.Pending, "operations": &view.Operations, "unresolvedEffectOperationRefs": &view.Unknown, "closureIntents": &view.Intents, "closureSeals": &view.Seals, "awaitingOperationAdmissionRefs": &view.Awaiting} {
			if body := fields[field]; body != nil {
				if e := json.Unmarshal(body, dest); e != nil {
					t.Fatal(e)
				}
			}
		}
		return view
	}
	pending := read()
	if len(pending.Pending) != 1 || len(pending.Awaiting) != 1 || len(pending.Intents) != 1 || len(pending.Seals) != 0 || len(pending.Operations) != 0 || len(pending.Unknown) != 0 {
		t.Fatal("missing pending original admission qualification")
	}
	awaiting := new(v1.Ref)
	if e = protojson.Unmarshal(pending.Awaiting[0], awaiting); e != nil || !proto.Equal(awaiting, a.Ref) {
		t.Fatal("awaiting record invented operation identity", e)
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	sealed := read()
	if len(sealed.Pending) != 0 || len(sealed.Awaiting) != 1 || len(sealed.Intents) != 1 || len(sealed.Seals) != 1 || len(sealed.Operations) != 0 || len(sealed.Unknown) != 0 {
		t.Fatal("tombstone ACK fabricated operation effect or hid pending original handoff")
	}
	intent, proof := new(v1.CancellationClosureIntent), new(v1.CancellationSeal)
	if e = protojson.Unmarshal(sealed.Intents[0], intent); e != nil || intent.RecipientReceipt == nil {
		t.Fatal("missing original source ACK", e)
	}
	if e = protojson.Unmarshal(sealed.Seals[0], proof); e != nil || !proof.NoSendProven || proof.PhysicalSendWasPossible || proof.OperationRef != nil || !proto.Equal(proof.Ref, intent.RecipientReceipt.ResultRef) || !proto.Equal(proof.IntentRef, intent.Ref) || !proto.Equal(proof.AdmissionRef, a.Ref) || !proto.Equal(proof.OperationId, a.OperationId) {
		t.Fatal("view lost exact endpoint tombstone proof", e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	delivered := read()
	if len(delivered.Pending) != 0 || len(delivered.Awaiting) != 0 || len(delivered.Operations) != 1 || len(delivered.Unknown) != 0 || len(delivered.Seals) != 1 {
		t.Fatal("original operation not visible after late admission")
	}
	op := new(v1.Operation)
	if e = protojson.Unmarshal(delivered.Operations[0], op); e != nil || !proto.Equal(op.Ref.Name, a.OperationId) || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Execution != nil {
		t.Fatal("late operation lost original no-send facts", e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	// 源方可读、执行管理的原连接已关闭，真实读取失败不能呈现为查无记录。
	cli.Tasks = f.h.Tasks
	var unavailable bytes.Buffer
	if e = cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &unavailable); e == nil || unavailable.Len() != 0 {
		t.Fatal("owner read failure appeared as empty or healthy facts")
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatal("view or late delivery touched target")
	}
}

// 规则：G3、G4、G9、G10、G11、R7
func TestCancellationScopeClosesOriginalModelAdmissionWithoutProviderSend(t *testing.T) {
	provider := simulator.NewModelProvider()
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 30000)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, run.Preparation)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, &v1.AdmitModelCallCommand{Header: header("cancel-model-admit"), RequestRef: run.Preparation.RequestRef, Claim: run.Preparation.Claim, DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil || a.ModelDescriptorDigest != call.DescriptorDigest || !proto.Equal(a.Origin, call.Ref) {
		t.Fatal("missing model admission", e)
	}
	cancelTask(t, f, "cancel-original-model")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil || len(scope.AdmissionRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], a.Ref) || len(scope.ClosureIntentRefs) != 1 {
		t.Fatal("model responsibility omitted from cancellation", e)
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	intent, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if e != nil || intent.RecipientReceipt == nil || !proto.Equal(intent.Command.AdmissionRef, a.Ref) || !proto.Equal(intent.Command.OperationId, a.OperationId) {
		t.Fatal("model closure lost original responsibility", e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Lifecycle != "SETTLED" || op.Execution != nil || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatal("model handoff escaped original tombstone", e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || budget.Reserved != 0 || budget.Settled != 0 || provider.Calls() != 0 || len(provider.Bills()) != 0 {
		t.Fatal("cancelled model sent or retained proved unused fee", e)
	}
}

// 规则：G1、G3、G10、G11、R7、开始-5
func TestCancellationOfNewestSendKeepsHistoricalEffectAndLateFee(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	late := performWithoutObservation(t, f, a, first)
	original, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("cancel-newest-resend"), OperationId: a.OperationId, PreviousSendRef: original.Send.Ref, Claim: first.Claim})
	accepted(t, r, e)
	current, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	second := resendStart(t, f, a, first, current, first.Claim)
	r, e = f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	accepted(t, r, e)
	cancelTask(t, f, "cancel-newest")
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || op.Execution.Send.Phase != "CLOSED" || op.Execution.PreviousSends[0].Phase != "DISPATCH_POSSIBLE" {
		t.Fatalf("new send closure erased old risk %v %v", op, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 30 || b.Settled != 0 {
		t.Fatalf("new closure released old fee %v %v", b, e)
	}
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	var sealSource *v1.TraceEvent
	for _, source := range sources {
		ev := source.Command.Event
		if ev.EventType == "CANCELLATION_SEALED" && proto.Equal(ev.OperationId, a.OperationId) {
			sealSource = ev
		}
	}
	if sealSource == nil || !proto.Equal(sealSource.AttemptId, original.Attempt.Ref.Name) || !proto.Equal(sealSource.SendRef, op.Execution.Send.Ref) || sealSource.EffectOutcome != "UNKNOWN" || sealSource.LateEffect != "MAY_OCCUR" {
		t.Fatal("seal source lost current send or historical risk")
	}
	requireModelTraceRef(t, sealSource, original.Send.Ref)
	requireModelTraceRef(t, sealSource, op.Execution.Send.Ref)
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Send.Ref)
	if e != nil || source.Status != "PENDING" || source.Amount != nil {
		t.Fatalf("lost original source %v %v", source, e)
	}
	savePhysicalObservation(t, f, late)
	op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "APPLIED" || op.Execution.Send.Phase != "CLOSED" || op.Execution.PreviousSends[0].Phase != "OBSERVED" {
		t.Fatalf("late original proof %v %v", op, e)
	}
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Reserved != 0 || b.Settled != 25 {
		t.Fatalf("late original fee %v %v", b, e)
	}
	source, e = f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Send.Ref)
	if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 25 {
		t.Fatalf("original settlement %v %v", source, e)
	}
	assertLateSendTrace(t, f, op, original.Send.Ref)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("cancel repeated IO %v %v", requests, effects)
	}
}

// 规则：G3、G11、R7
func TestCancellationClosureCannotBeAbandonedOrCompletedByStaleWorker(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, _ = prepareStart(t, f)
	cancelTask(t, f, "cancel-claim")
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("old-cancel-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{"DELIVER_CANCELLATION_CLOSURE"}, Limit: 1, LeaseMs: 500, ProcessInstance: "old-cancel-worker"})
	accepted(t, claim, e)
	old := claim.Jobs[0]
	for _, action := range []string{"CONTROL", "PROGRESS"} {
		next := "CLOSED"
		if action == "PROGRESS" {
			next = "COMPLETED"
		}
		r, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("abandon-cancel-" + action).Identity, ContractVersion: 1, Action: action, Module: "tasks", JobRef: old.Ref, ClaimEpoch: old.ClaimEpoch, ProcessInstance: old.ProcessInstance, NextState: next})
		if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
			t.Fatalf("abandoned cancellation %v %v", r, e)
		}
	}
	replacement := proto.Clone(old.SpecificationRef).(*v1.Ref)
	replacement.Revision++
	r, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("replace-cancel-basis").Identity, ContractVersion: 1, Action: "CONTROL", Module: "tasks", JobRef: old.Ref, SpecificationRef: replacement, NextState: "READY"})
	if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
		t.Fatalf("replaced cancellation basis %v %v", r, e)
	}
	stale := proto.Clone(old.Ref).(*v1.Ref)
	stale.Revision--
	r, e = f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("stale-cancel-control").Identity, ContractVersion: 1, Action: "CONTROL", Module: "tasks", JobRef: stale, NextState: "READY"})
	if e != nil || r.GetError().GetCode() != "REVISION_CONFLICT" {
		t.Fatalf("stale control changed cancellation %v %v", r, e)
	}
	time.Sleep(time.Until(time.UnixMilli(old.LeaseUntilUnixMs)) + 20*time.Millisecond)
	if e = f.h.Tasks.ProcessCancellationClosureClaim(f.ctx, old); e == nil {
		t.Fatal("stale worker completed closure")
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	current, e := f.h.Durable.QueryJob(f.ctx, f.caller, old.Ref.Name)
	if e != nil || current.State != "COMPLETED" || current.ClaimEpoch != old.ClaimEpoch+1 {
		t.Fatalf("lost original responsibility %v %v", current, e)
	}
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if e != nil || intent.RecipientReceipt == nil || f.calls.Load() != 0 {
		t.Fatalf("new worker closure %v %v", intent, e)
	}
}

// 规则：G1、G3、G4、G10、G11、R7
func TestCancellationAcknowledgementWaitsForOriginalPhysicalUseAndKeepsLateResult(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; target.ServeHTTP(w, r) }))
	a, start := prepareStart(t, f)
	invoked := make(chan error, 1)
	go func() {
		_, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		invoked <- e
	}()
	select {
	case <-entered:
	case e := <-invoked:
		t.Fatalf("original use did not enter: %v", e)
	case <-time.After(time.Second):
		close(release)
		<-invoked
		t.Fatal("original use did not enter")
	}
	cancelTask(t, f, "cancel-in-use")
	closing := make(chan error, 1)
	go func() { closing <- f.h.Tasks.ProcessCancellations(f.ctx, f.caller) }()
	select {
	case e := <-closing:
		close(release)
		<-invoked
		t.Fatalf("endpoint acknowledged while use still active: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if e := <-invoked; e != nil {
		t.Fatal(e)
	}
	if e := <-closing; e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("late result erased by cancel %v %v", op, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || budget.Settled != 25 || budget.Reserved != 0 {
		t.Fatalf("late fee erased by cancel %v %v", budget, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("original target counts %v %v", requests, effects)
	}
}

// 规则：G3、G4、G11、R7
func TestCancellationAcknowledgementPreservesNewerInputResponsibility(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	_, _ = prepareStart(t, f)
	cancelTask(t, f, "cancel-before-input")
	mutateCompletionBasis(t, f, "MODIFY")
	before, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	after, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil || after.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || after.ControlGeneration != before.ControlGeneration || !slices.Contains(after.WaitingOn, "INPUT_PROCESSING") || slices.Contains(after.WaitingOn, "CANCELLATION_CLOSURE") {
		t.Fatalf("old closure changed newer basis or left orphan wait %v %v", after, e)
	}
}

// 规则：G1、G3、G4、G5、G10、G11、R7、准入-7、开始-2
func TestCancellationKeepsOriginalReconciliationUnderCurrentControl(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	cancelTask(t, f, "cancel-queryable")
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	target.ReleasePending()
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatalf("cancel abandoned original reconciliation %v %v", plan, e)
	}
	query, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, query.AdmissionReceipt.ResultRef)
	if e != nil || admission.WorkCategory != "CLOSURE" || admission.ControlGeneration != a.ControlGeneration+1 {
		t.Fatalf("cleanup used old control basis %v %v", admission, e)
	}
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, query.ObservationRef)
	if e != nil || !proto.Equal(raw.OperationId, admission.OperationId) || proto.Equal(raw.OperationId, a.OperationId) {
		t.Fatalf("query relabeled as original send %v %v", raw, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "SEALED" || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("cancelled late effect missing %v %v", op, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || budget.Reserved != 35 || budget.Settled != 0 {
		t.Fatalf("effect proof fabricated original fee %v %v", budget, e)
	}
	cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Caller: f.caller, Domain: "d"}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"cancellation", f.task.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	var view struct{ Pending, Unknown, Reconciliations []json.RawMessage }
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(out.Bytes(), &fields); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(fields["pendingClosureRefs"], &view.Pending); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(fields["unresolvedEffectOperationRefs"], &view.Unknown); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(fields["reconciliations"], &view.Reconciliations); e != nil {
		t.Fatal(e)
	}
	if len(view.Pending) != 0 || len(view.Unknown) != 0 || len(view.Reconciliations) != 1 {
		t.Fatalf("missing original cleanup projection %s", out.String())
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[1].Method != "GET" || len(effects) != 1 {
		t.Fatalf("cancel caused another write %v %v", requests, effects)
	}
}
