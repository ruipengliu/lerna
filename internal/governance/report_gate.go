package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 发布批准和实际启动同库核验 report 原结论与当前资格。永久封存不
// 代表迟到的真实事实或曝光已经失效的报告仍可用。
func (s *Service) reportEligibleTx(ctx context.Context, tx runtime.Tx, ref api.ObjectRef) error {
	if err := ownerRef(tx.Scope(), ref); err != nil {
		return api.E("unsupported", "cross_owner_release_evidence_not_supported")
	}
	var report EvaluationReport
	if _, err := tx.Get(ctx, ns("reports"), ref.ObjectID, &report); err != nil {
		return err
	}
	if report.Revision != ref.Revision || report.Purpose != "formal" || !report.FormalEligible || report.TargetAttainment != "pass" || report.StatisticalGate != "pass" || report.ImprovementGate != "pass" {
		return api.E("forbidden", "formal_improvement_evidence_required")
	}
	var qualification ReportQualification
	if _, err := tx.Get(ctx, ns("report_qualification"), report.ReportID, &qualification); err != nil {
		return err
	}
	if !qualification.Eligible {
		return api.E("forbidden", "evaluation_evidence_invalidated")
	}
	var plan EvaluationPlan
	if _, err := tx.Get(ctx, ns("plans"), report.PlanID, &plan); err != nil {
		return err
	}
	var gate ExposureGate
	if _, err := tx.Get(ctx, ns("exposure_gates"), digestID("exposure_gate", plan.SourceGroup), &gate); err != nil {
		return err
	}
	if gate.Revision > qualification.GateRevision {
		return api.E("forbidden", "exposure_gate_requires_revalidation")
	}
	return nil
}
