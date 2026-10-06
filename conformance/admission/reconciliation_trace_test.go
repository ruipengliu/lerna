package admission_test

import (
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func assertReconciliationTrace(t *testing.T, f *fixture, p *v1.Reconciliation, wantOutcome string, findingRequired bool) *v1.TraceView {
	t.Helper()
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: p.OperationId})
	if err != nil || !view.Complete {
		t.Fatalf("trace coverage %v %v", view, err)
	}
	kinds := map[string]bool{}
	var last *v1.TraceEvent
	planFound := false
	findingFound := false
	for _, event := range view.Events {
		kinds[event.EventType] = true
		if event.EventType == "OPERATION_CHANGED" {
			last = event
		}
		if event.EventType == "RECONCILIATION_"+p.State && proto.Equal(event.SourceRecordRef, p.Ref) {
			planFound = true
		}
		if event.EventType == "RECONCILIATION_FINDING" {
			original, err := f.h.Ledger.QueryReconciliationFinding(f.ctx, f.caller, event.SourceRecordRef)
			if err != nil || original == nil || !proto.Equal(original.ObservationRef, event.ObservationRef) || original.Outcome != event.EffectOutcome || original.LateEffect != event.LateEffect {
				t.Fatalf("finding provenance %v %v %v", event, original, err)
			}
			relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, original.QueryRef)
			if err != nil || relation == nil {
				t.Fatal("query source missing")
			}
			linked := false
			for _, ref := range event.RelatedRefs {
				if proto.Equal(ref, relation.QueryOperationRef) {
					linked = true
				}
			}
			if !linked {
				t.Fatal("original/query actions collapsed")
			}
			findingFound = true
		}
	}
	if !planFound || last == nil || last.EffectOutcome != wantOutcome {
		t.Fatalf("lost current source or changed original effect plan=%v latest=%v", planFound, last)
	}
	for _, kind := range []string{"PROGRESS_HANDOFF", "PROGRESS_ACCEPTED", "PROGRESS_ACKNOWLEDGED"} {
		if !kinds[kind] {
			t.Errorf("missing %s", kind)
		}
	}
	if findingRequired && !findingFound {
		t.Fatal("query finding missing")
	}
	return view
}
