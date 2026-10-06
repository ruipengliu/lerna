package admission_test

import (
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G9、G11、R7
func TestModelTraceKeepsOriginalRequestCallAndOutcomeWithoutBodies(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"private":"MODEL-BODY-SECRET-20"}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 60000)
	run.Preparation.Settings.ParametersJson = []byte(`{"private":"MODEL-PARAMETER-SECRET-20"}`)
	result, err := f.h.Tasks.RunModelCall(f.ctx, f.caller, run)
	if err != nil {
		t.Fatal(err)
	}
	call, err := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	report := &v1.SubmitProposalOutcomeCommand{Header: header("trace-model-outcome"), RequestRef: run.Preparation.RequestRef, Claim: run.Preparation.Claim, ErrorCode: "INVALID_OUTPUT", ModelCallRef: result.CallRef, OutputRef: result.OutputRef, UsageRef: result.UsageRef}
	receipt, err := f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, report)
	accepted(t, receipt, err)
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !view.Complete {
		t.Fatalf("trace %v %v", view, err)
	}
	expected := map[string]*v1.Ref{"MODEL_CALL_PREPARING": call.Ref, "MODEL_CALL_SEALED": call.Ref, "MODEL_CALL_ADMITTED": call.Ref, "MODEL_RESULT_COMPLETED": call.Ref, "PROPOSAL_OUTCOME_ACCEPTED": receipt.ResultRef, "PROPOSAL_REQUEST_REPORTED": run.Preparation.RequestRef}
	for _, ev := range view.Events {
		if ref := expected[ev.EventType]; ref != nil && proto.Equal(ref, ev.SourceRecordRef) {
			if strings.HasPrefix(ev.EventType, "MODEL_") {
				requireModelTraceRef(t, ev, run.Preparation.RequestRef)
			}
			if ev.EventType == "MODEL_RESULT_COMPLETED" {
				requireModelTraceRef(t, ev, result.OutputRef)
				requireModelTraceRef(t, ev, result.UsageRef)
				if !proto.Equal(ev.OperationId, result.OperationId) || !proto.Equal(ev.ObservationRef, result.ObservationRef) {
					t.Fatal("result lost original operation/observation")
				}
			}
			if ev.EventType == "PROPOSAL_OUTCOME_ACCEPTED" {
				requireModelTraceRef(t, ev, result.CallRef)
				requireModelTraceRef(t, ev, result.OutputRef)
				requireModelTraceRef(t, ev, result.UsageRef)
			}
			delete(expected, ev.EventType)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing source events %v", expected)
	}
	encoded, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"MODEL-BODY-SECRET-20", "MODEL-PARAMETER-SECRET-20"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("model body entered trace")
		}
	}
	before := len(view.Events)
	replay, err := f.h.Tasks.SubmitProposalOutcome(f.ctx, f.caller, report)
	if err != nil || !proto.Equal(replay, receipt) {
		t.Fatal("outcome replay changed")
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	after, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || len(after.Events) != before || provider.Calls() != 1 {
		t.Fatal("replay duplicated model/source events")
	}
	changed := proto.Clone(run.Preparation).(*v1.PrepareModelCallCommand)
	changed.Header = header("trace-model-position-conflict")
	changed.Settings.ParametersJson = []byte(`{"changed":true}`)
	if _, err = f.h.Tasks.PrepareModelCall(f.ctx, f.caller, changed); err == nil {
		t.Fatal("changed position accepted")
	}
	denied, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, changed.Header.Identity)
	if err != nil || denied.GetReceipt().GetError().GetCode() != "MODEL_POSITION_CONFLICT" {
		t.Fatal("missing rejection", err)
	}
	assertModelSourceEvent(t, f, "DECISION_REJECTED", denied.Receipt.DecisionRef, "MODEL_POSITION_CONFLICT")

}
func requireModelTraceRef(t *testing.T, ev *v1.TraceEvent, want *v1.Ref) {
	t.Helper()
	for _, r := range ev.RelatedRefs {
		if proto.Equal(r, want) {
			return
		}
	}
	t.Fatalf("%s missing original ref %v", ev.EventType, want)
}

func assertModelSourceEvent(t *testing.T, f *fixture, kind string, ref *v1.Ref, reason string) {
	t.Helper()
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !view.Complete {
		t.Fatal("model source coverage", err)
	}
	for _, ev := range view.Events {
		if ev.EventType == kind && proto.Equal(ev.SourceRecordRef, ref) && ev.ReasonCode == reason {
			return
		}
	}
	t.Fatalf("missing model original state %s %v %s", kind, ref, reason)
}
