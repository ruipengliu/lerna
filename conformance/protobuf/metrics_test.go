package protobuf_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：H1、G1、G3、G10、G11、G12、R3
func TestCapturedLocalMetricsKeepOwnerScopeAndMissingMeasurements(t *testing.T) {
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := protobuf.Inspect(samples)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range report.UnsampledTypes {
		switch missing {
		case "lerna.v1.MetricSource", "lerna.v1.LatencyBucket", "lerna.v1.AdmissionMetrics", "lerna.v1.AgeBucket", "lerna.v1.UnknownMetrics", "lerna.v1.ReconciliationMetrics", "lerna.v1.LedgerMetrics", "lerna.v1.DiagnosticMetrics", "lerna.v1.TraceStreamMetrics", "lerna.v1.TraceMetrics", "lerna.v1.LocalMetrics":
			t.Errorf("missing actual public metric type: %s", missing)
		}
	}
	if t.Failed() {
		return
	}
	local := map[string]*v1.LocalMetrics{}
	for _, sample := range samples {
		if m, ok := sample.Message.(*v1.LocalMetrics); ok {
			local[sample.Name] = m
		}
	}
	for _, name := range []string{"metric-local-empty", "metric-local-unknown", "metric-local-ready", "metric-local-paused", "metric-local-completed", "metric-local-indexed"} {
		m := local[name]
		if m == nil || m.CrossDomainAtomic || m.DefinitionVersion != "lerna.m1.local-metrics.v1" || m.CapturedAtUnixMs <= 0 || len(m.UnavailableSources) != 0 || m.Admission == nil || m.Ledger == nil || m.Trace == nil {
			t.Fatalf("missing original public composite: %s %v", name, m)
		}
		if len(m.InvestigationGuidance) == 0 || len(m.InvestigationGuidance) > 16 {
			t.Fatalf("captured current public metric lacks bounded investigation guidance: %s", name)
		}
		if m.Ledger.Source.OwnerDomainId != "d/ledger" || m.Ledger.Source.SourceRevision == nil || m.Ledger.Source.Scope != "ALL_OPERATIONS_INCLUDING_CLOSED_TASKS" || m.Trace.Source.OwnerDomainId != "d/trace" || m.Admission.Source.Scope != "PROCESS_INSTANCE:TARGET_MODEL_CLOSURE" || m.Trace.Diagnostics.GetSource().GetAvailability() != "DISABLED" || m.Trace.Diagnostics.Dropped != nil || m.Trace.Diagnostics.StalenessMs != nil || m.Ledger.Reconciliation.DurationAvailability != "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS" {
			t.Fatalf("owner scope or missing measurement became healthy zero: %s %v", name, m)
		}
	}
	empty, unknown, ready, paused, completed, indexed := local["metric-local-empty"], local["metric-local-unknown"], local["metric-local-ready"], local["metric-local-paused"], local["metric-local-completed"], local["metric-local-indexed"]
	if empty.Admission.Source.Availability != "NO_SAMPLES" || empty.Admission.DurationSumNs != nil || len(empty.Admission.Buckets) != 0 || unknown.Admission.Samples != 1 || unknown.Admission.Accepted != 1 || unknown.Admission.DurationSumNs == nil || len(unknown.Admission.Buckets) == 0 || unknown.Ledger.Unknown.Total != 1 || unknown.Ledger.Unknown.Queryable != 1 || unknown.Ledger.Unknown.AgeSamples != 1 || unknown.Ledger.Unknown.OldestAgeMs == nil || ready.Ledger.Reconciliation.Total != 1 || ready.Ledger.Reconciliation.Ready != 1 || ready.Ledger.Reconciliation.Unfinished != 1 || paused.Ledger.Reconciliation.Paused != 1 || paused.Ledger.Reconciliation.Unfinished != 1 || completed.Ledger.Unknown.Total != 0 || completed.Ledger.Reconciliation.Total != 1 || completed.Ledger.Reconciliation.Completed != 1 || completed.Ledger.Reconciliation.Unfinished != 0 || !indexed.Trace.CoverageComplete || indexed.Trace.PendingAcknowledgements != 0 || indexed.Trace.ReceiverBacklog != 0 || indexed.Trace.IndexBacklog != 0 {
		t.Fatal("captured public population/availability lost its original state")
	}
}
