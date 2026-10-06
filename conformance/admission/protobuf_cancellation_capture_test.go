package admission_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、G12、R7
func TestCaptureCancellationPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureCancellation(t, &samples)
	required := map[string]bool{"lerna.v1.Cancellation": false, "lerna.v1.CloseCancellationCommand": false, "lerna.v1.CancellationClosureIntent": false, "lerna.v1.CancellationSeal": false, "lerna.v1.CancellationView": false}
	named := map[string]proto.Message{}
	for _, sample := range samples {
		if named[sample.Name] != nil {
			t.Fatalf("duplicate sample %s", sample.Name)
		}
		named[sample.Name] = sample.Message
		typ := string(sample.Message.ProtoReflect().Descriptor().FullName())
		if _, ok := required[typ]; ok {
			required[typ] = true
		}
		wire, err := proto.Marshal(sample.Message)
		if err != nil {
			t.Fatal(err)
		}
		decoded := sample.Message.ProtoReflect().Type().New().Interface()
		if err = proto.Unmarshal(wire, decoded); err != nil || !proto.Equal(decoded, sample.Message) {
			t.Fatalf("%s binary roundtrip: %v", sample.Name, err)
		}
		body, err := protojson.Marshal(sample.Message)
		if err != nil {
			t.Fatal(err)
		}
		jsonDecoded := sample.Message.ProtoReflect().Type().New().Interface()
		if err = protojson.Unmarshal(body, jsonDecoded); err != nil || !proto.Equal(jsonDecoded, sample.Message) {
			t.Fatalf("%s JSON roundtrip: %v", sample.Name, err)
		}
	}
	for typ, seen := range required {
		if !seen {
			t.Errorf("missing actual cancellation sample: %s", typ)
		}
	}
	if t.Failed() {
		return
	}
	for _, name := range []string{"cancellation-scope", "cancellation-control-command", "cancellation-control-receipt", "cancellation-original-admission", "cancellation-close-command", "cancellation-intent-acknowledged", "cancellation-endpoint-receipt", "cancellation-no-send-seal", "cancellation-view-pending", "cancellation-view-awaiting-original", "cancellation-view-original-arrived", "cancellation-original-operation"} {
		if named[name] == nil {
			t.Fatalf("missing actual stage %s", name)
		}
	}
	scope := named["cancellation-scope"].(*v1.Cancellation)
	cancel := named["cancellation-control-command"].(*v1.SubmitInputCommand)
	control := named["cancellation-control-receipt"].(*v1.CommandReceipt)
	admission := named["cancellation-original-admission"].(*v1.Admission)
	closeCommand := named["cancellation-close-command"].(*v1.CloseCancellationCommand)
	intent := named["cancellation-intent-acknowledged"].(*v1.CancellationClosureIntent)
	receipt := named["cancellation-endpoint-receipt"].(*v1.CommandReceipt)
	seal := named["cancellation-no-send-seal"].(*v1.CancellationSeal)
	pending := named["cancellation-view-pending"].(*v1.CancellationView)
	awaiting := named["cancellation-view-awaiting-original"].(*v1.CancellationView)
	arrived := named["cancellation-view-original-arrived"].(*v1.CancellationView)
	operation := named["cancellation-original-operation"].(*v1.Operation)
	if !proto.Equal(scope.ControlIdentity, cancel.Header.Identity) || !proto.Equal(control.Identity, cancel.Header.Identity) || scope.ControlGeneration != cancel.ExpectedControlGeneration+1 || len(scope.AdmissionRefs) != 1 || !proto.Equal(scope.AdmissionRefs[0], admission.Ref) || len(scope.ClosureIntentRefs) != 1 || !proto.Equal(scope.ClosureIntentRefs[0], intent.Ref) {
		t.Fatal("captured cancellation lost original control/scope")
	}
	if !proto.Equal(closeCommand, intent.Command) || !proto.Equal(closeCommand.Header.Identity, receipt.Identity) || !proto.Equal(closeCommand.IntentRef, intent.Ref) || !proto.Equal(closeCommand.CancellationRef, scope.Ref) || !proto.Equal(closeCommand.AdmissionRef, admission.Ref) || !proto.Equal(closeCommand.OperationId, admission.OperationId) || closeCommand.ExecutorEndpointId != admission.ExecutorEndpointId || !proto.Equal(intent.RecipientReceipt, receipt) || receipt.Decision != v1.Decision_DECISION_ACCEPTED || !proto.Equal(receipt.ResultRef, seal.Ref) {
		t.Fatal("captured closure command and receipt drifted")
	}
	if !seal.NoSendProven || seal.PhysicalSendWasPossible || seal.OperationRef != nil || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.CancellationRef, scope.Ref) || !proto.Equal(seal.AdmissionRef, admission.Ref) || !proto.Equal(seal.OperationId, admission.OperationId) {
		t.Fatal("captured no-send tombstone invented operation/effect")
	}
	if len(pending.PendingClosureRefs) != 1 || len(pending.AwaitingOperationAdmissionRefs) != 1 || len(pending.Operations) != 0 || len(pending.ClosureSeals) != 0 || len(pending.UnresolvedEffectOperationRefs) != 0 {
		t.Fatal("pending view lost qualification")
	}
	if len(awaiting.PendingClosureRefs) != 0 || len(awaiting.AwaitingOperationAdmissionRefs) != 1 || !proto.Equal(awaiting.AwaitingOperationAdmissionRefs[0], admission.Ref) || len(awaiting.Operations) != 0 || len(awaiting.UnresolvedEffectOperationRefs) != 0 || len(awaiting.ClosureSeals) != 1 || !proto.Equal(awaiting.ClosureSeals[0], seal) {
		t.Fatal("endpoint ACK fabricated operation")
	}
	if len(arrived.AwaitingOperationAdmissionRefs) != 0 || len(arrived.Operations) != 1 || !proto.Equal(arrived.Operations[0], operation) || !proto.Equal(arrived.Cancellation, scope) || !proto.Equal(operation.Ref.Name, admission.OperationId) || operation.Dispatch != "SEALED" || operation.Effect.Outcome != "NOT_APPLIED" || operation.Effect.LateEffect != "RULED_OUT" || operation.Execution != nil || arrived.Task.ResultRef != nil {
		t.Fatal("late original handoff did not preserve no-send identity")
	}
	path := filepath.Join(t.TempDir(), "cancellation.json")
	if err := protobuf.Save(path, samples); err != nil {
		t.Fatal(err)
	}
	restored, err := protobuf.Load(path)
	if err != nil || len(restored) != len(samples) {
		t.Fatalf("isolated sample persistence: %v", err)
	}
	for i := range samples {
		if restored[i].Name != samples[i].Name || !proto.Equal(restored[i].Message, samples[i].Message) {
			t.Fatalf("saved sample changed: %s", samples[i].Name)
		}
	}
	t.Logf("captured %d actual cancellation objects; all five new types round-tripped", len(samples))
}

// 规则：G3、G4、G10、G11、R7
func captureCancellation(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	proposal := f.propose(t, nil)
	admit := &v1.AdmitCommand{Header: header("capture-cancel-original-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant}
	admitted, err := f.h.Tasks.Admit(f.ctx, f.caller, admit)
	accepted(t, admitted, err)
	captureObject(t, samples, "cancellation-original-admit-command", admit, nil)
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, admitted.ResultRef)
	captureObject(t, samples, "cancellation-original-admission", admission, err)
	missing, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if err != nil || missing != nil {
		t.Fatalf("original P2 already arrived: %v %v", missing, err)
	}
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if err != nil {
		t.Fatal(err)
	}
	cancel := &v1.SubmitInputCommand{Header: header("capture-cancel-before-original-P2"), SessionId: goal.Receipt.SessionRef.Name, TaskId: f.task.Name, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration}
	control, err := f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
	accepted(t, control, err)
	captureObject(t, samples, "cancellation-control-command", cancel, nil)
	captureObject(t, samples, "cancellation-control-receipt", control, nil)
	scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	captureObject(t, samples, "cancellation-scope", scope, err)
	if len(scope.ClosureIntentRefs) != 1 {
		t.Fatalf("missing exact cancellation responsibility: %v", scope)
	}
	intent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	captureObject(t, samples, "cancellation-intent-pending", intent, err)
	captureObject(t, samples, "cancellation-close-command", intent.Command, nil)
	if intent.RecipientReceipt != nil {
		t.Fatal("endpoint ACK preceded actual closure")
	}
	readView := func() *v1.CancellationView {
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
	pending := readView()
	captureObject(t, samples, "cancellation-view-pending", pending, nil)
	if len(pending.PendingClosureRefs) != 1 || len(pending.AwaitingOperationAdmissionRefs) != 1 || len(pending.Operations) != 0 || len(pending.UnresolvedEffectOperationRefs) != 0 {
		t.Fatalf("pending view invented original facts: %v", pending)
	}
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	intent, err = f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	captureObject(t, samples, "cancellation-intent-acknowledged", intent, err)
	captureObject(t, samples, "cancellation-endpoint-receipt", intent.RecipientReceipt, nil)
	seal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
	captureObject(t, samples, "cancellation-no-send-seal", seal, err)
	if !seal.NoSendProven || seal.OperationRef != nil || seal.PhysicalSendWasPossible || !proto.Equal(seal.AdmissionRef, admission.Ref) {
		t.Fatalf("missing exact original no-send tombstone: %v", seal)
	}
	awaiting := readView()
	captureObject(t, samples, "cancellation-view-awaiting-original", awaiting, nil)
	if len(awaiting.PendingClosureRefs) != 0 || len(awaiting.AwaitingOperationAdmissionRefs) != 1 || len(awaiting.Operations) != 0 || len(awaiting.ClosureSeals) != 1 || len(awaiting.UnresolvedEffectOperationRefs) != 0 {
		t.Fatalf("ACK fabricated absent operation: %v", awaiting)
	}
	missing, err = f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if err != nil || missing != nil {
		t.Fatalf("tombstone created fabricated operation: %v %v", missing, err)
	}
	if err = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	captureObject(t, samples, "cancellation-original-operation", operation, err)
	if !proto.Equal(operation.Ref.Name, admission.OperationId) || operation.Dispatch != "SEALED" || operation.Lifecycle != "SETTLED" || operation.Effect.Outcome != "NOT_APPLIED" || operation.Effect.LateEffect != "RULED_OUT" || operation.Execution != nil {
		t.Fatalf("late original admission escaped tombstone: %v", operation)
	}
	arrived := readView()
	captureObject(t, samples, "cancellation-view-original-arrived", arrived, nil)
	planning, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	captureObject(t, samples, "cancellation-original-planning", planning, err)
	if len(planning.AdmissionRefs) != 1 || !proto.Equal(planning.AdmissionRefs[0], admission.Ref) {
		t.Fatal("cancellation replaced original admission")
	}
	replay, err := f.h.Tasks.Admit(f.ctx, f.caller, admit)
	if err != nil || !proto.Equal(replay, admitted) {
		t.Fatalf("original admit replay: %v %v", replay, err)
	}
	replay, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, cancel)
	if err != nil || !proto.Equal(replay, control) {
		t.Fatalf("original control replay: %v %v", replay, err)
	}
	replay, err = f.h.Egress.CloseForCancellation(f.ctx, &v1.Caller{UserId: "u", IssuerId: intent.Command.Header.Identity.IssuerId}, intent.Command)
	if err != nil || !proto.Equal(replay, intent.RecipientReceipt) {
		t.Fatalf("original endpoint replay: %v %v", replay, err)
	}
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if err != nil || !proto.Equal(restored, operation) {
		t.Fatalf("recovery changed original operation: %v %v", restored, err)
	}
	restoredScope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if err != nil || !proto.Equal(restoredScope, scope) {
		t.Fatalf("recovery rewrote cancellation scope: %v %v", restoredScope, err)
	}
	restoredIntent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, intent.Ref)
	if err != nil || !proto.Equal(restoredIntent, intent) {
		t.Fatalf("recovery rewrote original ACK: %v %v", restoredIntent, err)
	}
	restoredSeal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, seal.Ref)
	if err != nil || !proto.Equal(restoredSeal, seal) {
		t.Fatalf("recovery rewrote original seal: %v %v", restoredSeal, err)
	}
	if recovered := readView(); !proto.Equal(recovered, arrived) {
		t.Fatalf("recovery changed qualified view: %v", recovered)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatalf("capture/replay/recovery reached target: requests=%d effects=%d bills=%d", len(requests), len(effects), len(target.Bills()))
	}
}
