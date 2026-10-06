package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G3、G4、G10、G11、R3、R7
func TestCaptureLocalMetricPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureLocalMetrics(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.MetricSource", "lerna.v1.LatencyBucket", "lerna.v1.AdmissionMetrics", "lerna.v1.AgeBucket", "lerna.v1.UnknownMetrics", "lerna.v1.ReconciliationMetrics", "lerna.v1.LedgerMetrics", "lerna.v1.DiagnosticMetrics", "lerna.v1.TraceStreamMetrics", "lerna.v1.TraceMetrics", "lerna.v1.LocalMetrics"})
	requireCapturedFields(t, samples, []string{"lerna.v1.MetricSource.error_code", "lerna.v1.MetricSource.source_revision", "lerna.v1.AdmissionMetrics.duration_sum_ns", "lerna.v1.AdmissionMetrics.buckets", "lerna.v1.UnknownMetrics.oldest_age_ms", "lerna.v1.UnknownMetrics.age_buckets", "lerna.v1.ReconciliationMetrics.paused", "lerna.v1.ReconciliationMetrics.completed", "lerna.v1.ReconciliationMetrics.unfinished", "lerna.v1.TraceMetrics.receiver_acceptance_position", "lerna.v1.TraceStreamMetrics.acknowledged", "lerna.v1.LocalMetrics.unavailable_sources", "lerna.v1.LocalMetrics.investigation_guidance"})
	roundTripClosingSamples(t, samples)
}

func captureLocalMetrics(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	first := len(*samples)
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	capability, grant := configureReconciliation(t, f)
	var admission *v1.Admission
	capture := func(name string) *v1.LocalMetrics {
		t.Helper()
		beforeSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
		if e != nil {
			t.Fatal(e)
		}
		budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
		if e != nil {
			t.Fatal(e)
		}
		var before *v1.Operation
		if admission != nil {
			before, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if e != nil {
				t.Fatal(e)
			}
		}
		requests, effects := target.Snapshot()
		m, e := f.h.QueryMetrics(f.ctx, f.caller)
		captureObject(t, samples, name, m, e)
		if m.CrossDomainAtomic || len(m.UnavailableSources) != 0 || m.Trace.Diagnostics.Source.Availability != "DISABLED" || m.Trace.Diagnostics.Dropped != nil || m.Trace.Diagnostics.StalenessMs != nil || m.Ledger.Reconciliation.DurationAvailability != "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS" {
			t.Fatal("actual metric query invented atomicity or measurements")
		}
		afterSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
		if e != nil || len(beforeSources) != len(afterSources) {
			t.Fatalf("metric query changed original source inventory: %v", e)
		}
		for i := range beforeSources {
			if !proto.Equal(beforeSources[i], afterSources[i]) {
				t.Fatal("metric query advanced original source acknowledgement")
			}
		}
		afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
		if e != nil || !proto.Equal(budget, afterBudget) {
			t.Fatalf("metric query changed original budget: %v", e)
		}
		if before != nil {
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
			if e != nil || !proto.Equal(before, after) {
				t.Fatalf("metric query changed original execution/effect: %v", e)
			}
		}
		afterRequests, afterEffects := target.Snapshot()
		if len(requests) != len(afterRequests) || len(effects) != len(afterEffects) {
			t.Fatal("metric query caused physical target work")
		}
		return m
	}
	empty := capture("metric-local-empty")
	if empty.Admission.Source.Availability != "NO_SAMPLES" || empty.Admission.DurationSumNs != nil || len(empty.Admission.Buckets) != 0 {
		t.Fatal("empty process has no latency sample")
	}
	a, start := prepareStart(t, f)
	admission = a
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	unknown := capture("metric-local-unknown")
	if unknown.Admission.Samples != 1 || unknown.Ledger.Unknown.Total != 1 || unknown.Ledger.Unknown.Queryable != 1 || unknown.Ledger.Unknown.AgeSamples != 1 || unknown.Ledger.Reconciliation.UnknownUncovered != 1 {
		t.Fatal("original admitted P5 UNKNOWN is missing from public metrics")
	}
	// 嵌套对象直接采自原查询；不手工填充直方图、来源或诊断对象。
	captureObject(t, samples, "metric-admission", unknown.Admission, nil)
	captureObject(t, samples, "metric-admission-source", unknown.Admission.Source, nil)
	for _, bucket := range unknown.Admission.Buckets {
		if bucket.Count > 0 {
			captureObject(t, samples, "metric-latency-bucket", bucket, nil)
			break
		}
	}
	captureObject(t, samples, "metric-ledger", unknown.Ledger, nil)
	captureObject(t, samples, "metric-ledger-source", unknown.Ledger.Source, nil)
	captureObject(t, samples, "metric-unknown", unknown.Ledger.Unknown, nil)
	for _, bucket := range unknown.Ledger.Unknown.AgeBuckets {
		if bucket.Count > 0 {
			captureObject(t, samples, "metric-age-bucket", bucket, nil)
			break
		}
	}
	captureObject(t, samples, "metric-trace", unknown.Trace, nil)
	captureObject(t, samples, "metric-trace-source", unknown.Trace.Source, nil)
	captureObject(t, samples, "metric-diagnostics-disabled", unknown.Trace.Diagnostics, nil)
	for _, stream := range unknown.Trace.Streams {
		if stream.Progress.SourceHighWater > 0 {
			captureObject(t, samples, "metric-trace-stream", stream, nil)
			break
		}
	}
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, capability, grant))
	accepted(t, r, e)
	ready := capture("metric-local-ready")
	captureObject(t, samples, "metric-reconciliation-ready", ready.Ledger.Reconciliation, nil)
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metric-capture-pause"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	paused := capture("metric-local-paused")
	if ready.Ledger.Reconciliation.Ready != 1 || ready.Ledger.Reconciliation.Unfinished != 1 || paused.Ledger.Reconciliation.Paused != 1 || paused.Ledger.Reconciliation.Unfinished != 1 {
		t.Fatal("actual paused plan disappeared from unfinished denominator")
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metric-capture-resume"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME"})
	accepted(t, r, e)
	target.SetBehavior("")
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	completed := capture("metric-local-completed")
	if completed.Ledger.Unknown.Total != 0 || completed.Ledger.Reconciliation.Completed != 1 || completed.Ledger.Reconciliation.Unfinished != 0 {
		t.Fatal("separately admitted actual query did not determine original effect")
	}
	if e = f.h.Trace.Collect(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Trace.Index(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	indexed := capture("metric-local-indexed")
	if !indexed.Trace.CoverageComplete || indexed.Trace.PendingAcknowledgements != 0 || indexed.Trace.IndexBacklog != 0 || indexed.Trace.ReceiverBacklog != 0 {
		t.Fatal("actual indexed source coverage remains incomplete")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 || f.calls.Load() != 2 {
		t.Fatal("public metric population did not use exactly one original POST and one independent GET")
	}
	// 真正关闭存储造成读取失败；进程准入指标仍可用，不能填假空持久快照。
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	partial, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil || partial.Admission == nil || partial.Admission.Samples != indexed.Admission.Samples || partial.Ledger != nil || partial.Trace != nil || len(partial.UnavailableSources) != 2 || partial.CrossDomainAtomic {
		t.Fatalf("closed actual store erased available process source: %v %v", partial, e)
	}
	captureObject(t, samples, "metric-local-source-unavailable", partial, nil)
	for _, source := range partial.UnavailableSources {
		if source.Availability != "MISSING" || source.Scope != "COLLECTION_FAILED" || source.ErrorCode != "DEPENDENCY_UNAVAILABLE" {
			t.Fatal("unavailable original owner became healthy zero")
		}
		captureObject(t, samples, "metric-unavailable-"+source.OwnerDomainId, source, nil)
	}
	restored, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	f.h = restored
	restarted := capture("metric-local-restarted")
	if restarted.Admission.Source.Availability != "NO_SAMPLES" || restarted.Admission.Samples != 0 || restarted.Admission.DurationSumNs != nil || restarted.Ledger.Reconciliation.Completed != 1 {
		t.Fatal("restart reconstructed old admission latency or lost original plan")
	}
	requests, effects = target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 || f.calls.Load() != 2 {
		t.Fatal("recovery added a business send")
	}
	t.Logf("actual metric public objects=%d; POST1 GET1 effects1; originalUNKNOWN1 completed1 unfinished0; source/index/ACK checked; storeUnavailable retains process; restartNO_SAMPLES; diagnosticsDISABLED; durationMISSING", len(*samples)-first)
}
