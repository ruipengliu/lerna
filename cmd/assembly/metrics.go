package assembly

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// QueryMetrics 只组合各负责方的只读结果，单一来源失败保留其他来源及其版本。
func (h *Harness) QueryMetrics(ctx context.Context, caller *v1.Caller) (*v1.LocalMetrics, error) {
	if e := command.CheckCaller(caller, h.user); e != nil {
		return nil, e
	}
	m := &v1.LocalMetrics{DefinitionVersion: "lerna.m1.local-metrics.v1"}
	unavailable := func(domain, definition string, e error) {
		code := "DEPENDENCY_UNAVAILABLE"
		var failure *command.Failure
		if errors.As(e, &failure) {
			code = failure.Detail.Code
		}
		m.UnavailableSources = append(m.UnavailableSources, &v1.MetricSource{OwnerDomainId: domain, DefinitionVersion: definition, CapturedAtUnixMs: time.Now().UnixMilli(), Availability: "MISSING", Scope: "COLLECTION_FAILED", ErrorCode: code})
	}
	var e error
	if m.Admission, e = h.Tasks.QueryAdmissionMetrics(ctx, caller); e != nil {
		unavailable(h.domain, "lerna.m1.admission.v1", e)
	}
	if m.Ledger, e = h.Ledger.QueryMetrics(ctx, caller); e != nil {
		m.Ledger = nil
		unavailable(h.domain+"/ledger", "lerna.m1.ledger.v1", e)
	}
	if m.Trace, e = h.Trace.QueryMetrics(ctx, caller); e != nil {
		m.Trace = nil
		unavailable(h.domain+"/trace", "lerna.m1.trace.v1", e)
	}
	m.InvestigationGuidance = localMetricGuidance(m)
	m.CapturedAtUnixMs = time.Now().UnixMilli()
	return m, nil
}

// localMetricGuidance 只选择固定排查文字，不读取正文、对象标识或任意错误详情。
func localMetricGuidance(m *v1.LocalMetrics) []string {
	var guidance []string
	if len(m.UnavailableSources) > 0 {
		guidance = append(guidance, "Metric owners are unavailable. Inspect unavailableSources for the responsible owner and availability, and check the local database before interpreting its missing values. Missing values are not healthy zeros.")
	}
	if m.Admission.GetSource().GetAvailability() == "NO_SAMPLES" {
		guidance = append(guidance, "Admission latency is NO_SAMPLES for this process. Check the current process instance and sample availability; recovered receipts cannot reconstruct prior admission latency.")
	}
	if m.Admission.GetDecisionsWithoutEndpoint() > 0 || m.Admission.GetCallsWithoutDecision() > 0 {
		guidance = append(guidance, "Some calls lack an original admission endpoint or durable decision. Use task TASK_ID to inspect the original admitted work and check process sample availability; replay and recovery cannot reconstruct the missing latency.")
	}
	if m.Ledger != nil {
		if m.Ledger.KnownEffectLateMayOccur > 0 {
			guidance = append(guidance, "Known effects may still occur late. Use operation OPERATION_ID to inspect the original evidence and late-effect state; a known outcome does not prove that later effects are ruled out.")
		}
		u := m.Ledger.GetUnknown()
		if u.GetQueryabilityMissing() > 0 || m.Ledger.UninterpretableOperations > 0 || m.Ledger.GetReconciliation().GetOther() > 0 {
			guidance = append(guidance, "Inspect missing or uninterpretable operation metadata and reconciliation state with operation OPERATION_ID and reconciliation OPERATION_ID. Check the original declaration and implementation version; unsupported fields do not justify strong guarantees or healthy zeros.")
		}
		if u.GetAgeMissing() > 0 || u.GetAgeClockInvalid() > 0 {
			guidance = append(guidance, "UNKNOWN effects have missing or invalid age. Inspect the original attempt's first possible-send timestamp and the local clock; absent or future timestamps do not mean zero age.")
		}
		if u.GetTotal() > 0 {
			guidance = append(guidance, "UNKNOWN effects remain. Use operation OPERATION_ID to inspect the original attempt and effect, and reconciliation OPERATION_ID to inspect its independently authorized query. Do not resend an UNKNOWN action to test its outcome.")
		}
		r := m.Ledger.GetReconciliation()
		if r.GetWaiting() > 0 || r.GetPaused() > 0 {
			guidance = append(guidance, "WAITING or PAUSED reconciliation remains unfinished. Use reconciliation OPERATION_ID to inspect the saved retry time, pause reason and limits before an explicitly authorized scan or control command.")
		}
		if r.GetDurationAvailability() != "AVAILABLE" {
			guidance = append(guidance, "Original UNKNOWN and completion-duration endpoints are missing. Inspect progress counts including unfinished plans; do not infer a completion duration or SLO from completed samples.")
		}
	}
	if m.Trace != nil {
		if !m.Trace.CoverageComplete {
			guidance = append(guidance, "Trace coverage is incomplete. Use trace-task TASK_ID or trace-operation OPERATION_ID to inspect original source, receiver acceptance, index and source ACK positions. Trace completeness does not prove business completion.")
		}
		if m.Trace.GetDiagnostics().GetSource().GetAvailability() == "DISABLED" {
			guidance = append(guidance, "Optional diagnostics are DISABLED. Inspect the durable task and operation queries; missing diagnostic drop and staleness values are not healthy zeros.")
		}
	}
	return guidance
}
