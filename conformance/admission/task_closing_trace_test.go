package admission_test

import (
	"strings"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G6、G11、R7、完成-6
func assertClosingTraceSources(t *testing.T, f *fixture, view *v1.TaskClosingView) {
	t.Helper()
	assertTaskOwnerSource(t, f, "TASK_CLOSE_REQUESTED", view.Closing.Ref, view.Closing.AdmissionRefs...)
	assertTaskOwnerSource(t, f, "RESULT_FIXED", view.Result.Ref, view.Closing.Ref)
	for _, ref := range view.Closing.ClosureIntentRefs {
		intent, e := f.h.Tasks.QueryTaskClosureIntent(f.ctx, f.caller, ref)
		if e != nil {
			t.Fatal(e)
		}
		assertTaskOwnerSource(t, f, "TASK_CLOSURE_ACKNOWLEDGED", ref, intent.RecipientReceipt.DecisionRef)
		seal, e := f.h.Ledger.QueryTaskClosureSeal(f.ctx, f.caller, intent.RecipientReceipt.ResultRef)
		if e != nil {
			t.Fatal(e)
		}
		assertTaskOwnerSource(t, f, "TASK_CLOSURE_SEALED", seal.Ref, append([]*v1.Ref{view.Closing.Ref, seal.OperationRef}, seal.ClosedSendRefs...)...)
	}
	for _, followup := range view.ExecutionFollowups {
		assertTaskOwnerSource(t, f, "EXECUTION_FOLLOWUP_RETAINED", followup.Ref, append([]*v1.Ref{view.Closing.Ref, followup.OperationRef, followup.AttemptRef, followup.JobRef}, followup.SendRefs...)...)
	}
	for _, followup := range view.SettlementFollowups {
		assertTaskOwnerSource(t, f, "SETTLEMENT_FOLLOWUP_RETAINED", followup.Ref, append(append([]*v1.Ref{view.Closing.Ref, followup.JobRef}, followup.ReservationRefs...), followup.BillingSourceRefs...)...)
	}
	for _, job := range view.FollowupJobs {
		kind := "SETTLEMENT_FOLLOWUP_COMPLETED"
		if job.Module == "ledger" {
			kind = "EXECUTION_FOLLOWUP_COMPLETED"
		}
		assertTaskOwnerSource(t, f, kind, job.Ref, job.SpecificationRef)
	}
}

// assertTaskOwnerSource 读取原负责方 outbox，再核验独立接收方的原接纳事实。
func assertTaskOwnerSource(t *testing.T, f *fixture, kind string, ref *v1.Ref, related ...*v1.Ref) {
	t.Helper()
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	var original *v1.TraceSourceRecord
	for _, source := range sources {
		event := source.Command.Event
		if event.EventType == kind && proto.Equal(event.SourceRecordRef, ref) {
			if original != nil {
				t.Fatalf("duplicate original source %s %v", kind, ref)
			}
			original = source
		}
	}
	if original == nil {
		t.Fatalf("missing original source %s %v", kind, ref)
	}
	event := original.Command.Event
	if !proto.Equal(event.TaskId, f.task.Name) {
		t.Fatalf("wrong task identity %v", event)
	}
	for _, r := range related {
		if r == nil {
			continue
		}
		found := false
		for _, actual := range event.RelatedRefs {
			if proto.Equal(actual, r) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s lost original responsibility %v", kind, r)
		}
	}
	encoded, e := proto.Marshal(event)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(encoded), "http://") || strings.Contains(string(encoded), "an independent appendix") {
		t.Fatalf("source exposed transport or body %v", event)
	}
	if e = f.h.Trace.Recover(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	accepted, e := f.h.Trace.QueryEvent(f.ctx, f.caller, event.Ref)
	if e != nil || !proto.Equal(event, accepted) {
		t.Fatalf("independent receiver changed original %v %v", accepted, e)
	}
}
