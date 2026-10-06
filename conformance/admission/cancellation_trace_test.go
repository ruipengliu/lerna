package admission_test

import (
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G8、G10、G11、R7
func TestCancellationSourcesKeepOriginalControlClosureAndUnknownIdentity(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	cancel := cancelTask(t, f, "cancel-trace-original")
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
	if err != nil || intent.RecipientReceipt == nil {
		t.Fatal("missing original endpoint receipt", err)
	}
	seal, err := f.h.Ledger.QueryCancellationSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]*v1.Ref{"CANCELLATION_SAVED": scope.Ref, "CANCELLATION_CLOSURE_REQUESTED": intent.Ref, "CANCELLATION_SEALED": seal.Ref, "CANCELLATION_CLOSURE_ACKNOWLEDGED": intent.Ref}
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string]*v1.TraceSourceRecord{}
	for _, source := range sources {
		ev := source.Command.Event
		want := expected[ev.EventType]
		if want == nil {
			continue
		}
		if originals[ev.EventType] != nil || !proto.Equal(ev.SourceRecordRef, want) || !proto.Equal(ev.TaskId, a.TaskId) || source.Receipt != nil {
			t.Fatalf("changed or duplicated cancellation source %v", source)
		}
		originals[ev.EventType] = proto.Clone(source).(*v1.TraceSourceRecord)
		if ev.EventType == "CANCELLATION_SAVED" {
			if !proto.Equal(ev.OriginCommand, cancel.Identity) {
				t.Fatal("saved control lost its original identity")
			}
			requireModelTraceRef(t, ev, a.Ref)
			requireModelTraceRef(t, ev, intent.Ref)
		} else {
			if !proto.Equal(ev.OperationId, a.OperationId) || !proto.Equal(ev.OriginCommand, intent.Command.Header.Identity) {
				t.Fatal("closure lost its original operation/command")
			}
			requireModelTraceRef(t, ev, scope.Ref)
			requireModelTraceRef(t, ev, a.Ref)
		}
		if ev.EventType == "CANCELLATION_SEALED" {
			if !proto.Equal(ev.AttemptId, op.Execution.Attempt.Ref.Name) || !proto.Equal(ev.SendRef, op.Execution.Send.Ref) || ev.EffectOutcome != "UNKNOWN" || ev.LateEffect != "MAY_OCCUR" {
				t.Fatal("seal source erased original P5/unknown identity")
			}
			requireModelTraceRef(t, ev, intent.Ref)
			requireModelTraceRef(t, ev, seal.OperationRef)
		}
		if ev.EventType == "CANCELLATION_CLOSURE_ACKNOWLEDGED" {
			requireModelTraceRef(t, ev, intent.JobRef)
			requireModelTraceRef(t, ev, intent.RecipientReceipt.DecisionRef)
			requireModelTraceRef(t, ev, seal.Ref)
		}
		actor := &v1.Caller{UserId: f.caller.UserId, IssuerId: source.Command.Header.Identity.IssuerId}
		q, e := f.h.Trace.QueryReceipt(f.ctx, actor, source.Command.Header.Identity)
		if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
			t.Fatal("business source entered receiver transaction", e)
		}
	}
	if len(originals) != len(expected) {
		t.Fatalf("missing cancellation source facts: got %v", originals)
	}
	if err = f.h.Trace.Collect(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	collected, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range collected {
		original := originals[source.Command.Event.EventType]
		if original == nil {
			continue
		}
		if !proto.Equal(source.Command, original.Command) || source.Receipt == nil || !proto.Equal(source.Receipt.Identity, original.Command.Header.Identity) {
			t.Fatal("source ACK changed original command")
		}
		ev, e := f.h.Trace.QueryEvent(f.ctx, f.caller, original.Command.Event.Ref)
		if e != nil || !proto.Equal(ev, original.Command.Event) {
			t.Fatal("receiver changed original source", e)
		}
	}
	lagging, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: a.TaskId})
	if err != nil || lagging.Complete || lagging.IndexBacklog == 0 || lagging.PendingReceipts != 0 {
		t.Fatal("index lag or ACK hidden", err)
	}
	if err = f.h.Trace.Index(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: a.TaskId})
	if err != nil || !view.Complete {
		t.Fatal("cancellation coverage", err)
	}
	byOperation, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if err != nil {
		t.Fatal(err)
	}
	for kind, original := range originals {
		if kind == "CANCELLATION_SAVED" {
			continue
		}
		found := false
		for _, ev := range byOperation.Events {
			found = found || proto.Equal(ev, original.Command.Event)
		}
		if !found {
			t.Fatalf("original operation lost %s", kind)
		}
	}
	capability, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"create a record", f.path, capability.Resource, capability.ExecutorEndpointId} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("cancellation trace leaked body/path/target/endpoint %q", private)
		}
	}
	if err = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: a.TaskId})
	if err != nil || len(replayed.Events) != len(view.Events) {
		t.Fatal("recovery duplicated cancellation events", err)
	}
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(op, after) {
		t.Fatal("trace changed original effect", err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	requests, effects := target.Target.Snapshot()
	if err != nil || budget.Reserved != 30 || len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatal("trace changed original fee or external counts", err)
	}
}
