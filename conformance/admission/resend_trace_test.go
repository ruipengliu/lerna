package admission_test

import (
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func assertResendTrace(t *testing.T, f *fixture, request *v1.PrepareResendCommand, receipt *v1.CommandReceipt) {
	t.Helper()
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: request.OperationId})
	if err != nil || !view.Complete {
		t.Fatal("resend coverage", err)
	}
	var event *v1.TraceEvent
	for _, ev := range view.Events {
		if ev.EventType == "RESEND_PREPARED" && proto.Equal(ev.OriginCommand, request.Header.Identity) {
			if event != nil {
				t.Fatal("replay duplicated resend source")
			}
			event = ev
		}
	}
	if event == nil || !proto.Equal(event.SourceRecordRef, receipt.ResultRef) || !proto.Equal(event.SendRef, receipt.ResultRef) {
		t.Fatal("missing original resend decision/send source")
	}
	requireModelTraceRef(t, event, request.PreviousSendRef)
	send, err := f.h.Ledger.QuerySend(f.ctx, f.caller, receipt.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	if send.ResendQueryObservationRef != nil {
		requireModelTraceRef(t, event, send.ResendQueryObservationRef)
	}
	previous, err := f.h.Ledger.QuerySend(f.ctx, f.caller, request.PreviousSendRef)
	if err != nil || previous == nil || !proto.Equal(previous.AttemptId, event.AttemptId) {
		t.Fatal("previous send identity lost", err)
	}
}

func assertLateSendTrace(t *testing.T, f *fixture, op *v1.Operation, original *v1.Ref) {
	t.Helper()
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: op.Ref.Name})
	if err != nil {
		t.Fatal(err)
	}
	observed, interpreted := false, false
	for _, ev := range view.Events {
		if ev.EventType == "OPERATION_CHANGED" && proto.Equal(ev.SourceRecordRef, op.Ref) {
			requireModelTraceRef(t, ev, op.Execution.PreviousSends[0].Ref)
			requireModelTraceRef(t, ev, op.Execution.PreviousSends[0].ObservationRef)
		}
		if proto.Equal(ev.SendRef, original) && proto.Equal(ev.ObservationRef, op.Execution.PreviousSends[0].ObservationRef) {
			if ev.EventType == "PHYSICAL_OBSERVATION" {
				observed = true
			}
			if ev.EventType == "EFFECT_INTERPRETED" {
				interpreted = true
			}
		}
	}
	if !observed || !interpreted {
		t.Fatalf("late evidence lost original send association observed=%v interpreted=%v", observed, interpreted)
	}
}
