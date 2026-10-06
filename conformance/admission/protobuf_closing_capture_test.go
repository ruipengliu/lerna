package admission_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G2、G3、G10、G11、R7、完成-4、完成-6
func TestCaptureTaskClosingPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureTaskClosing(t, &samples)
	named := roundTripClosingSamples(t, samples)
	required := map[string]bool{"lerna.v1.BeginTaskCloseCommand": false, "lerna.v1.TaskClosing": false, "lerna.v1.CloseTaskEndpointCommand": false, "lerna.v1.TaskClosureIntent": false, "lerna.v1.TaskClosureSeal": false, "lerna.v1.ExecutionFollowup": false, "lerna.v1.SettlementFollowup": false, "lerna.v1.TaskClosingView": false}
	for _, s := range samples {
		typ := string(s.Message.ProtoReflect().Descriptor().FullName())
		if _, ok := required[typ]; ok {
			required[typ] = true
		}
	}
	for typ, seen := range required {
		if !seen {
			t.Errorf("missing actual closing family %s", typ)
		}
	}
	for _, prefix := range []string{"closing-failed", "closing-cancelled"} {
		for _, stage := range []string{"begin", "scope", "endpoint-command", "intent-ack", "seal", "execution-followup", "settlement-followup", "view-fixed", "view-late", "result-fixed", "result-late"} {
			if named[prefix+"-"+stage] == nil {
				t.Errorf("missing actual stage %s-%s", prefix, stage)
			}
		}
	}
	for _, stage := range []string{"closing-pre-p2-admission", "closing-pre-p2-pending", "closing-pre-p2-awaiting", "closing-pre-p2-arrived"} {
		if named[stage] == nil {
			t.Errorf("missing actual qualified stage %s", stage)
		}
	}
	if t.Failed() {
		return
	}
	for _, prefix := range []string{"closing-failed", "closing-cancelled"} {
		begin := named[prefix+"-begin"].(*v1.BeginTaskCloseCommand)
		scope := named[prefix+"-scope"].(*v1.TaskClosing)
		command := named[prefix+"-endpoint-command"].(*v1.CloseTaskEndpointCommand)
		intent := named[prefix+"-intent-ack"].(*v1.TaskClosureIntent)
		seal := named[prefix+"-seal"].(*v1.TaskClosureSeal)
		execution := named[prefix+"-execution-followup"].(*v1.ExecutionFollowup)
		settlement := named[prefix+"-settlement-followup"].(*v1.SettlementFollowup)
		fixed := named[prefix+"-view-fixed"].(*v1.TaskClosingView)
		late := named[prefix+"-view-late"].(*v1.TaskClosingView)
		if !proto.Equal(scope.RequestedBy, begin.Header.Identity) || scope.ControlGeneration != begin.ExpectedControlGeneration+1 || !proto.Equal(scope.TaskId, begin.TaskRef.Name) || !proto.Equal(command, intent.Command) || !proto.Equal(command.TaskClosingRef, scope.Ref) || !proto.Equal(intent.RecipientReceipt.ResultRef, seal.Ref) || !proto.Equal(seal.IntentRef, intent.Ref) || seal.NoSendProven || !seal.PhysicalSendWasPossible {
			t.Fatal("captured closing command/ACK/seal drifted")
		}
		if !proto.Equal(execution.TaskClosingRef, scope.Ref) || !proto.Equal(settlement.TaskClosingRef, scope.Ref) || execution.AttemptRef == nil || len(execution.SendRefs) != 1 || len(settlement.ReservationRefs) != 1 || len(settlement.BillingSourceRefs) != 1 || len(fixed.FollowupJobs) != 2 || len(late.FollowupJobs) != 2 {
			t.Fatal("actual owner responsibility absent")
		}
		if fixed.Result.Outcome != begin.Outcome || len(fixed.Result.UnknownOperationRefs) != 1 || fixed.Result.UsageSnapshot.Reserved != 30 || fixed.Operations[0].Effect.Outcome != "UNKNOWN" || late.Operations[0].Effect.Outcome != "APPLIED" || !proto.Equal(fixed.Result, late.Result) || !proto.Equal(named[prefix+"-result-fixed"], named[prefix+"-result-late"]) {
			t.Fatal("late facts altered fixed Result")
		}
		for _, j := range late.FollowupJobs {
			if j.State != "COMPLETED" || j.Ref.Revision < 2 {
				t.Fatal("owner completion not captured", j)
			}
		}
	}
	a := named["closing-pre-p2-admission"].(*v1.Admission)
	pending := named["closing-pre-p2-pending"].(*v1.TaskClosingView)
	awaiting := named["closing-pre-p2-awaiting"].(*v1.TaskClosingView)
	arrived := named["closing-pre-p2-arrived"].(*v1.TaskClosingView)
	if pending.Result != nil || len(pending.ClosureIntents) != 1 || len(pending.PendingClosureRefs) != 1 || len(pending.Operations) != 0 || len(awaiting.AwaitingOperationAdmissionRefs) != 1 || !proto.Equal(awaiting.AwaitingOperationAdmissionRefs[0], a.Ref) || awaiting.Result != nil || len(awaiting.Operations) != 0 || len(awaiting.ClosureSeals) != 1 || !awaiting.ClosureSeals[0].NoSendProven || awaiting.ClosureSeals[0].OperationRef != nil {
		t.Fatal("pre-P2 sample fabricated operation or unknown")
	}
	if len(arrived.AwaitingOperationAdmissionRefs) != 0 || len(arrived.Operations) != 1 || arrived.Operations[0].Execution != nil || arrived.Operations[0].Effect.Outcome != "NOT_APPLIED" || arrived.Result.Outcome != "CANCELLED" || len(arrived.Result.UnknownOperationRefs) != 0 {
		t.Fatal("original handoff escaped tombstone")
	}
	t.Logf("captured %d actual closing objects; all eight families and pre-P2 qualified views round-tripped", len(samples))
}

func roundTripClosingSamples(t *testing.T, samples []protobuf.Sample) map[string]proto.Message {
	t.Helper()
	named := map[string]proto.Message{}
	for _, sample := range samples {
		if named[sample.Name] != nil {
			t.Fatalf("duplicate sample %s", sample.Name)
		}
		named[sample.Name] = sample.Message
		wire, e := proto.Marshal(sample.Message)
		if e != nil {
			t.Fatal(e)
		}
		decoded := sample.Message.ProtoReflect().Type().New().Interface()
		if e = proto.Unmarshal(wire, decoded); e != nil || !proto.Equal(decoded, sample.Message) {
			t.Fatalf("%s binary: %v", sample.Name, e)
		}
		body, e := protojson.Marshal(sample.Message)
		if e != nil {
			t.Fatal(e)
		}
		if e = protojson.Unmarshal(body, decoded); e != nil || !proto.Equal(decoded, sample.Message) {
			t.Fatalf("%s JSON: %v", sample.Name, e)
		}
	}
	path := filepath.Join(t.TempDir(), "actual-closing.json")
	if e := protobuf.Save(path, samples); e != nil {
		t.Fatal(e)
	}
	restored, e := protobuf.Load(path)
	if e != nil || len(restored) != len(samples) {
		t.Fatalf("sample persistence %v", e)
	}
	for i, s := range samples {
		if restored[i].Name != s.Name || !proto.Equal(restored[i].Message, s.Message) {
			t.Fatalf("saved sample changed %s", s.Name)
		}
	}
	return named
}

// 规则：G1、G2、G3、G10、G11、R7、完成-4、完成-6
func captureTaskClosing(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	for _, outcome := range []string{"FAILED", "CANCELLED"} {
		captureUnknownTaskClosing(t, samples, outcome)
	}
	captureClosingBeforeHandoff(t, samples)
}

func captureUnknownTaskClosing(t *testing.T, samples *[]protobuf.Sample, outcome string) {
	t.Helper()
	prefix := "closing-" + strings.ToLower(outcome)
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, prefix+"-"+name, m, e)
	}
	target := simulator.NewBillingTarget(120)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	add("admission", a, nil)
	add("start-command", start, nil)
	// 与正式关闭验收相同的真实受信 HTTP 调用；只推迟已经取得的原回报交接。
	late := performWithoutObservation(t, f, a, start)
	add("actual-physical-result-held", late, nil)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	add("operation-before-close", original, e)
	if original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatal("actual send was not unresolved")
	}
	var cancelScope *v1.Cancellation
	if outcome == "CANCELLED" {
		cancelTask(t, f, prefix+"-cancel")
		cancelScope, e = f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
		add("original-cancellation", cancelScope, e)
		if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	begin := &v1.BeginTaskCloseCommand{Header: header(prefix + "-begin"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: outcome, CloseReason: "UNABLE_TO_COMPLETE", CancellationRef: cancelScope.GetRef()}
	receipt, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, begin)
	accepted(t, receipt, e)
	add("begin", begin, nil)
	add("begin-receipt", receipt, nil)
	pending, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("view-pending", pending, e)
	if len(pending.ClosureIntents) != 1 || pending.ClosureIntents[0].RecipientReceipt != nil {
		t.Fatal("new closure had no exact pending intent")
	}
	add("endpoint-command", pending.ClosureIntents[0].Command, nil)
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	fixed, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("view-fixed", fixed, e)
	if fixed.Result == nil || fixed.Result.Outcome != outcome || len(fixed.Result.UnknownOperationRefs) != 1 || fixed.Result.UsageSnapshot.Reserved != 30 || len(fixed.ClosureIntents) != 1 || len(fixed.ClosureSeals) != 1 || len(fixed.ExecutionFollowups) != 1 || len(fixed.SettlementFollowups) != 1 {
		t.Fatal("real closed unknown responsibility missing", fixed)
	}
	add("scope", fixed.Closing, nil)
	add("intent-ack", fixed.ClosureIntents[0], nil)
	add("endpoint-receipt", fixed.ClosureIntents[0].RecipientReceipt, nil)
	add("seal", fixed.ClosureSeals[0], nil)
	add("execution-followup", fixed.ExecutionFollowups[0], nil)
	add("settlement-followup", fixed.SettlementFollowups[0], nil)
	add("result-fixed", fixed.Result, nil)
	for _, j := range fixed.FollowupJobs {
		if j.State != "WAITING" {
			t.Fatal("unresolved owner not waiting", j)
		}
		add("job-waiting-"+j.Module, j, nil)
	}
	fixedBytes, e := proto.Marshal(fixed.Result)
	if e != nil {
		t.Fatal(e)
	}
	bill := stageBill(t, f, target.Bills()[0], prefix+"-late-bill")
	imported := &v1.ImportBillCommand{Header: header(prefix + "-import"), SendRef: original.Execution.Send.Ref, EvidenceRef: bill}
	billReceipt, e := f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	accepted(t, billReceipt, e)
	add("import-original-bill", imported, nil)
	add("import-original-bill-receipt", billReceipt, nil)
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	billView, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("view-after-bill", billView, e)
	if billView.Operations[0].Effect.Outcome != "UNKNOWN" || !proto.Equal(billView.Result, fixed.Result) {
		t.Fatal("bill decided original effect or altered result")
	}
	for _, j := range billView.FollowupJobs {
		want := "WAITING"
		if j.Module == "budget" {
			want = "COMPLETED"
		}
		if j.State != want {
			t.Fatal("bill completed wrong responsibility", j)
		}
	}
	savePhysicalObservation(t, f, late)
	if e = f.h.Ledger.ProcessExecutionFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	final, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("view-late", final, e)
	add("result-late", final.Result, nil)
	finalBytes, e := proto.Marshal(final.Result)
	if e != nil || !bytes.Equal(fixedBytes, finalBytes) || !proto.Equal(final.Closing, fixed.Closing) || !proto.Equal(final.ExecutionFollowups[0], fixed.ExecutionFollowups[0]) || !proto.Equal(final.SettlementFollowups[0], fixed.SettlementFollowups[0]) || final.Operations[0].Effect.Outcome != "APPLIED" || final.Operations[0].Effect.LateEffect != "RULED_OUT" || final.Operations[0].Dispatch != "SEALED" || !proto.Equal(final.Operations[0].Execution.Attempt.Ref.Name, original.Execution.Attempt.Ref.Name) || !proto.Equal(final.Operations[0].Execution.CallDescriptor, original.Execution.CallDescriptor) {
		t.Fatal("late original facts changed fixed scope or original execution")
	}
	for _, j := range final.FollowupJobs {
		if j.State != "COMPLETED" {
			t.Fatal("actual owner stranded", j)
		}
		add("job-completed-"+j.Module, j, nil)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	add("budget-late", budget, e)
	if budget.Settled != 120 || budget.Reserved != 0 || budget.Deficit != 40 {
		t.Fatal("late supplier cost missing", budget)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	add("source-late", source, e)
	if source.Amount == nil || *source.Amount != 120 || source.Status != "SETTLED" || !proto.Equal(source.SendRef.Name, original.Execution.Send.Ref.Name) {
		t.Fatal("bill lost original send")
	}
	observation, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, final.Operations[0].Execution.Send.ObservationRef)
	add("observation-late", observation, e)
	replay, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, begin)
	if e != nil || !proto.Equal(replay, receipt) {
		t.Fatal("close replay drifted", e)
	}
	endpoint := fixed.ClosureIntents[0].Command
	replay, e = f.h.Egress.CloseForTaskClose(f.ctx, &v1.Caller{UserId: "u", IssuerId: endpoint.Header.Identity.IssuerId}, endpoint)
	if e != nil || !proto.Equal(replay, fixed.ClosureIntents[0].RecipientReceipt) {
		t.Fatal("endpoint replay drifted", e)
	}
	replay, e = f.h.Budget.ImportBill(f.ctx, f.caller, imported)
	if e != nil || !proto.Equal(replay, billReceipt) {
		t.Fatal("bill replay drifted", e)
	}
	savePhysicalObservation(t, f, late)
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	f.h = h
	recovered, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if e != nil || !proto.Equal(recovered.Result, fixed.Result) || !proto.Equal(recovered.Closing, fixed.Closing) || !proto.Equal(recovered.Operations[0], final.Operations[0]) {
		t.Fatal("restart replaced original close", e)
	}
	if cancelScope != nil {
		scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
		if e != nil || !proto.Equal(scope, cancelScope) {
			t.Fatal("close replaced original cancellation", e)
		}
	}
	assertClosingTraceSources(t, f, recovered)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("closing capture resent: calls=%d effects=%d bills=%d", len(requests), len(effects), len(target.Bills()))
	}
	t.Logf("%s actual calls=%d effects=%d bills=%d fixed_reserved=%d current_settled=%d current_deficit=%d", prefix, len(requests), len(effects), len(target.Bills()), fixed.Result.UsageSnapshot.Reserved, budget.Settled, budget.Deficit)
}

func captureClosingBeforeHandoff(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "closing-pre-p2-"+name, m, e)
	}
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	command := &v1.AdmitCommand{Header: header("capture-closing-delayed-admit"), TaskId: f.task.Name, ProposalRef: f.propose(t, nil), GrantRef: f.grant}
	receipt, e := f.h.Tasks.Admit(f.ctx, f.caller, command)
	accepted(t, receipt, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, receipt.ResultRef)
	add("admission", a, e)
	missing, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || missing != nil {
		t.Fatal("P2 already delivered", e)
	}
	cancelTask(t, f, "capture-closing-delayed-cancel")
	scope, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	begin := &v1.BeginTaskCloseCommand{Header: header("capture-closing-delayed-begin"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: task.ControlGeneration, Outcome: "CANCELLED", CloseReason: "USER_STOPPED", CancellationRef: scope.Ref}
	closeReceipt, e := f.h.Tasks.BeginTaskClose(f.ctx, f.caller, begin)
	accepted(t, closeReceipt, e)
	add("begin", begin, nil)
	pending, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("pending", pending, e)
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	awaiting, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("awaiting", awaiting, e)
	if awaiting.Result != nil || len(awaiting.Operations) != 0 || len(awaiting.AwaitingOperationAdmissionRefs) != 1 || len(awaiting.ClosureSeals) != 1 || !awaiting.ClosureSeals[0].NoSendProven || awaiting.ClosureSeals[0].OperationRef != nil {
		t.Fatal("tombstone fabricated operation", awaiting)
	}
	add("tombstone", awaiting.ClosureSeals[0], nil)
	missing, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || missing != nil {
		t.Fatal("tombstone created operation", e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	arrived, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	add("arrived", arrived, e)
	if arrived.Result == nil || len(arrived.Operations) != 1 || arrived.Operations[0].Execution != nil || arrived.Operations[0].Effect.Outcome != "NOT_APPLIED" {
		t.Fatal("original handoff escaped tombstone")
	}
	for i, j := range arrived.FollowupJobs {
		add(fmt.Sprintf("job-%d", i), j, nil)
	}
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, command)
	if e != nil || !proto.Equal(r, receipt) {
		t.Fatal("admission replay replaced original", e)
	}
	r, e = f.h.Tasks.BeginTaskClose(f.ctx, f.caller, begin)
	if e != nil || !proto.Equal(r, closeReceipt) {
		t.Fatal("closing replay replaced original", e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	f.h = h
	recovered, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if e != nil || !proto.Equal(recovered.Result, arrived.Result) || !proto.Equal(recovered.Closing, arrived.Closing) || !proto.Equal(recovered.Operations[0], arrived.Operations[0]) {
		t.Fatal("restart replaced delayed original", e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatal("pre-P2 capture reached target")
	}
	t.Logf("closing-pre-p2 actual calls=%d effects=%d bills=%d awaiting_original_without_operation=true", len(requests), len(effects), len(target.Bills()))
}
