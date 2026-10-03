package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type ExposureImpact struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	SourceGroup string `json:"source_group"`
	Cursor      string `json:"cursor,omitempty"`
}
type LateSource struct {
	ID             string `json:"id"`
	Revision       uint64 `json:"revision"`
	SourceRevision uint64 `json:"source_revision"`
	Digest         string `json:"digest"`
}
type EvaluationNotice struct {
	ID         string        `json:"id"`
	ReportRef  api.ObjectRef `json:"report_ref"`
	SourceRef  api.ObjectRef `json:"source_ref"`
	Reason     string        `json:"reason"`
	RecordedAt string        `json:"recorded_at"`
}

// 报告和逐样本结果查询也是实际反馈出口，必须先持久登记曝光再披露。
// 稳定主体/会话代次/资源键保留第一次出口事实，重查不会伪造较晚曝光。
func (s *Service) exposeRead(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, plan EvaluationPlan, resource, kind string, report *api.ObjectRef) (ExposureGate, error) {
	var gate ExposureGate
	id := digestID("read_exposure", []any{a.SubjectID, a.CredentialGeneration, resource, kind})
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		var e error
		gate, e = s.exposureGate(ctx, tx, plan.SourceGroup)
		if e != nil {
			return e
		}
		var old Exposure
		if _, e = tx.Get(ctx, ns("exposures"), id, &old); e == nil {
			return nil
		} else if !errMissing(e) {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		if _, e = s.saveExposure(ctx, tx, a, ExposureRegister{ExposureID: id, SourceGroup: plan.SourceGroup, Kind: kind, OccurredAt: api.Time(now), ReportRef: report}); e != nil {
			return e
		}
		gate, e = s.exposureGate(ctx, tx, plan.SourceGroup)
		return e
	})
	if status == runtime.CommitUnknown {
		return gate, runtime.ErrCommitUnknown
	}
	return gate, err
}

func (s *Service) registerExposure(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ExposureRegister) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Kind != "debug" && in.Kind != "leak" {
		return runtime.Outcome{}, api.E("invalid_request", "feedback_requires_sealed_report")
	}
	ref, err := s.saveExposure(ctx, tx, a, in)
	return runtime.Applied(StateOutput{Ref: ref, State: "recorded"}), err
}
func (s *Service) saveExposure(ctx context.Context, tx runtime.Tx, a runtime.Auth, in ExposureRegister) (api.ObjectRef, error) {
	gate, err := s.exposureGate(ctx, tx, in.SourceGroup)
	if err != nil {
		return api.ObjectRef{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return api.ObjectRef{}, err
	}
	if !api.ValidID(in.ExposureID) || in.SourceGroup == "" {
		return api.ObjectRef{}, api.E("invalid_request", "exposure_identity_invalid")
	}
	if in.OccurredAt != "" {
		occurred, err := api.ParseTime(in.OccurredAt)
		if err != nil || occurred.After(now) {
			return api.ObjectRef{}, api.E("invalid_request", "exposure_time_invalid")
		}
	}
	exposure := Exposure{ExposureID: in.ExposureID, SourceGroup: in.SourceGroup, ActorRef: a.Ref(tx.Scope().OwnerID), Kind: in.Kind, OccurredAt: in.OccurredAt, RecordedAt: api.Time(now), ReportRef: in.ReportRef, Cursor: gate.Cursor + 1}
	if err = tx.Create(ctx, ns("exposures"), in.ExposureID, in.SourceGroup, exposure); err != nil {
		return api.ObjectRef{}, err
	}
	gate.Cursor++
	old := gate.Revision
	gate.Revision++
	gate.KnownExposure = true
	if in.OccurredAt == "" {
		gate.UnknownTime = true
	} else {
		gate.EarliestOccurredAt = minTime(gate.EarliestOccurredAt, in.OccurredAt)
	}
	if err = tx.Put(ctx, ns("exposure_gates"), gate.ID, old, gate); err != nil {
		return api.ObjectRef{}, err
	}
	id := digestID("exposure_impact", in.ExposureID)
	impact := ExposureImpact{ID: id, Revision: 1, SourceGroup: in.SourceGroup}
	if err = tx.Create(ctx, ns("exposure_impacts"), id, in.SourceGroup, impact); err != nil {
		return api.ObjectRef{}, err
	}
	if _, err = tx.Raise(ctx, "governance.exposure", id, tx.Scope().Ref(in.ExposureID, 1), now); err != nil {
		return api.ObjectRef{}, err
	}
	return tx.Scope().Ref(in.ExposureID, 1), nil
}
func (s *Service) feedbackOpen(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in FeedbackOpen) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.ReportRef); err != nil {
		return runtime.Outcome{}, err
	}
	var report EvaluationReport
	if _, err := tx.Get(ctx, ns("reports"), in.ReportRef.ObjectID, &report); err != nil {
		return runtime.Outcome{}, err
	}
	if report.Revision != in.ReportRef.Revision {
		return runtime.Outcome{}, api.E("revision_conflict", "revision_changed")
	}
	var plan EvaluationPlan
	if _, err := tx.Get(ctx, ns("plans"), report.PlanID, &plan); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = s.saveExposure(ctx, tx, a, ExposureRegister{ExposureID: in.ExposureID, SourceGroup: plan.SourceGroup, Kind: "feedback", OccurredAt: api.Time(now), ReportRef: &in.ReportRef}); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(report), nil
}
func gateInvalidatesReport(gate ExposureGate, report EvaluationReport) bool {
	if gate.UnknownTime {
		return true
	}
	if gate.EarliestOccurredAt == "" {
		return false
	}
	occurred, err := api.ParseTime(gate.EarliestOccurredAt)
	if err != nil {
		return true
	}
	sealed, err := api.ParseTime(report.SealedAt)
	return err != nil || !occurred.After(sealed)
}
func (s *Service) continueExposure(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var impact ExposureImpact
		rev, err := tx.Get(ctx, ns("exposure_impacts"), work.Job.ResponsibilityKey, &impact)
		if err != nil {
			return err
		}
		gate, err := s.exposureGate(ctx, tx, impact.SourceGroup)
		if err != nil {
			return err
		}
		reports, err := tx.List(ctx, ns("reports"), impact.SourceGroup, impact.Cursor, 100)
		if err != nil {
			return err
		}
		for _, row := range reports {
			var report EvaluationReport
			if err = row.Decode(&report); err != nil {
				return err
			}
			impact.Cursor = row.ID
			qualification := ReportQualification{}
			qrev, err := tx.Get(ctx, ns("report_qualification"), report.ReportID, &qualification)
			if err != nil {
				return err
			}
			qualification.Revision = qrev + 1
			qualification.GateRevision = gate.Revision
			invalid := gateInvalidatesReport(gate, report)
			if invalid {
				qualification.Eligible = false
				qualification.Reason = "exposure_invalidated"
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			qualification.UpdatedAt = api.Time(now)
			if err = tx.Put(ctx, ns("report_qualification"), report.ReportID, qrev, qualification); err != nil {
				return err
			}
			if invalid {
				if err = s.invalidateReportApprovalsTx(ctx, tx, report.ReportID); err != nil {
					return err
				}
			}
		}
		impact.Revision = rev + 1
		if err = tx.Put(ctx, ns("exposure_impacts"), impact.ID, rev, impact); err != nil {
			return err
		}
		if len(reports) == 100 {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		}
		return nil
	})
}
func (s *Service) invalidateReportApprovalsTx(ctx context.Context, tx runtime.Tx, reportID string) error {
	id := digestID("report_invalidation", reportID)
	var impact ApprovalStopImpact
	if _, err := tx.Get(ctx, ns("approval_stop_impacts"), id, &impact); err == nil {
		return nil
	} else if !errMissing(err) {
		return err
	}
	impact = ApprovalStopImpact{ID: id, Revision: 1, ReportID: reportID}
	if err := tx.Create(ctx, ns("approval_stop_impacts"), id, reportID, impact); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Raise(ctx, "governance.approval_stop", id, tx.Scope().Ref(reportID, 1), now)
	return err
}

func (s *Service) mergeLateObservation(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in LateObservation) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_reporter"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.SampleRunRef); err != nil {
		return runtime.Outcome{}, err
	}
	if err := validateObservation(tx.Scope(), in.Observation); err != nil {
		return runtime.Outcome{}, err
	}
	if in.SourceRevision == 0 || in.SourceDigest == "" {
		return runtime.Outcome{}, api.E("invalid_request", "late_source_invalid")
	}
	var source LateSource
	id := in.SampleRunRef.ObjectID
	srcRev, err := tx.Get(ctx, ns("late_sources"), id, &source)
	if err == nil {
		if in.SourceRevision < source.SourceRevision {
			return runtime.Applied(StateOutput{Ref: in.SampleRunRef, State: "stale_ignored"}), nil
		}
		if in.SourceRevision == source.SourceRevision {
			if in.SourceDigest != source.Digest {
				return runtime.Outcome{}, api.E("idempotency_conflict", "late_source_revision_changed")
			}
			return runtime.Applied(StateOutput{Ref: in.SampleRunRef, State: "duplicate"}), nil
		}
	} else if !errMissing(err) {
		return runtime.Outcome{}, err
	}
	source = LateSource{ID: id, Revision: srcRev + 1, SourceRevision: in.SourceRevision, Digest: in.SourceDigest}
	if srcRev == 0 {
		err = tx.Create(ctx, ns("late_sources"), id, "", source)
	} else {
		err = tx.Put(ctx, ns("late_sources"), id, srcRev, source)
	}
	if err != nil {
		return runtime.Outcome{}, err
	}
	var sample SampleRun
	rev, err := tx.Get(ctx, ns("sample_runs"), id, &sample)
	if err != nil {
		return runtime.Outcome{}, err
	}
	var manifest EvaluationSample
	if _, err = tx.Get(ctx, ns("sample_manifest"), id, &manifest); err != nil {
		return runtime.Outcome{}, err
	}
	if !api.Equal(manifest.TruthRef, in.Observation.TruthRef) {
		return runtime.Outcome{}, api.E("forbidden", "truth_binding_mismatch")
	}
	sample.Outcome = in.Observation.Outcome
	sample.Usage = in.Observation.Usage
	sample.UsageFinal = in.Observation.UsageFinal
	sample.CostUpperBound = in.Observation.CostUpperBound
	sample.LatencyMillis = in.Observation.LatencyMillis
	sample.MayApplyLater = in.Observation.MayApplyLater
	sample.ModelClaimedComplete = in.Observation.ModelClaimedComplete
	sample.SystemAccepted = in.Observation.SystemAccepted
	sample.TruthRef = &in.Observation.TruthRef
	sample.ObservedAt = in.Observation.ObservedAt
	sample.Revision = rev + 1
	if err = tx.Put(ctx, ns("sample_runs"), id, rev, sample); err != nil {
		return runtime.Outcome{}, err
	}
	var run EvaluationRun
	if _, err = tx.Get(ctx, ns("runs"), sample.RunID, &run); err != nil {
		return runtime.Outcome{}, err
	}
	if run.ReportRef != nil {
		var report EvaluationReport
		if _, err = tx.Get(ctx, ns("reports"), run.ReportRef.ObjectID, &report); err != nil {
			return runtime.Outcome{}, err
		}
		var plan EvaluationPlan
		if _, err = tx.Get(ctx, ns("plans"), run.PlanID, &plan); err != nil {
			return runtime.Outcome{}, err
		}
		samples, err := s.allSamples(ctx, tx, run.RunID)
		if err != nil {
			return runtime.Outcome{}, err
		}
		recomputed, err := summarize(plan, run, samples, report.SealedAt)
		if err != nil {
			return runtime.Outcome{}, err
		}
		contradiction := report.TargetAttainment == "pass" && recomputed.TargetAttainment != "pass" || report.StatisticalGate == "pass" && recomputed.StatisticalGate != "pass" || report.ImprovementGate == "pass" && recomputed.ImprovementGate != "pass"
		if contradiction {
			var qualification ReportQualification
			qrev, err := tx.Get(ctx, ns("report_qualification"), report.ReportID, &qualification)
			if err != nil {
				return runtime.Outcome{}, err
			}
			qualification.Eligible = false
			qualification.Reason = "late_fact_invalidated_original_conclusion"
			qualification.Revision = qrev + 1
			now, err := tx.Now(ctx)
			if err != nil {
				return runtime.Outcome{}, err
			}
			qualification.UpdatedAt = api.Time(now)
			if err = tx.Put(ctx, ns("report_qualification"), qualification.ReportID, qrev, qualification); err != nil {
				return runtime.Outcome{}, err
			}
			noticeID := digestID("evaluation_notice", []any{report.ReportID, id, in.SourceRevision})
			notice := EvaluationNotice{ID: noticeID, ReportRef: tx.Scope().Ref(report.ReportID, 1), SourceRef: tx.Scope().Ref(id, sample.Revision), Reason: qualification.Reason, RecordedAt: api.Time(now)}
			if err = tx.Create(ctx, ns("evaluation_notices"), noticeID, report.ReportID, notice); err != nil {
				return runtime.Outcome{}, err
			}
			if err = s.invalidateReportApprovalsTx(ctx, tx, qualification.ReportID); err != nil {
				return runtime.Outcome{}, err
			}
		}
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(id, sample.Revision), State: "merged"}), nil
}
