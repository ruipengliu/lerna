package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func multiplyDecimal(value string, n uint64) (string, error) {
	out := "0"
	for n > 0 {
		if n&1 == 1 {
			var err error
			out, err = api.AddDecimal(out, value)
			if err != nil {
				return "", err
			}
		}
		n >>= 1
		if n > 0 {
			var err error
			value, err = api.AddDecimal(value, value)
			if err != nil {
				return "", err
			}
		}
	}
	return out, nil
}
func (s *Service) allSamples(ctx context.Context, tx runtime.Tx, runID string) ([]SampleRun, error) {
	all := []SampleRun{}
	cursor := ""
	for {
		rows, err := tx.List(ctx, ns("sample_runs"), runID, cursor, 100)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var sr SampleRun
			if err = row.Decode(&sr); err != nil {
				return nil, err
			}
			all = append(all, sr)
			cursor = row.ID
		}
		if len(all) > 20000 {
			return nil, api.E("invalid_state", "sample_index_limit")
		}
		if len(rows) < 100 {
			break
		}
	}
	return all, nil
}
func (s *Service) sealRun(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in IDInput) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	report, err := s.SealReportTx(ctx, tx, in.ID)
	return runtime.Applied(report), err
}
func (s *Service) SealReportTx(ctx context.Context, tx runtime.Tx, runID string) (EvaluationReport, error) {
	var run EvaluationRun
	rev, err := tx.Get(ctx, ns("runs"), runID, &run)
	if err != nil {
		return EvaluationReport{}, err
	}
	if run.ReportRef != nil {
		var report EvaluationReport
		_, err = tx.Get(ctx, ns("reports"), run.ReportRef.ObjectID, &report)
		return report, err
	}
	var plan EvaluationPlan
	if _, err = tx.Get(ctx, ns("plans"), run.PlanID, &plan); err != nil {
		return EvaluationReport{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return EvaluationReport{}, err
	}
	cutoff, err := api.ParseTime(plan.ObservationCutoff)
	if err != nil {
		return EvaluationReport{}, err
	}
	if now.Before(cutoff) {
		return EvaluationReport{}, api.E("invalid_state", "observation_window_open")
	}
	if !run.Samples.Complete {
		return EvaluationReport{}, api.E("invalid_state", "sample_index_incomplete")
	}
	gate, err := s.exposureGate(ctx, tx, plan.SourceGroup)
	if err != nil {
		return EvaluationReport{}, err
	}
	all, err := s.allSamples(ctx, tx, runID)
	if err != nil {
		return EvaluationReport{}, err
	}
	if uint64(len(all)) != 2*plan.FullDenominator {
		return EvaluationReport{}, api.E("invalid_state", "sample_index_incomplete")
	}
	report, err := summarize(plan, run, all, api.Time(now))
	if err != nil {
		return report, err
	}
	report.FormalEligible = plan.FormalEligible && !gate.KnownExposure
	report.ReportID = digestID("report", runID)
	report.Revision = 1
	if err = tx.Create(ctx, ns("reports"), report.ReportID, plan.SourceGroup, report); err != nil {
		return report, err
	}
	qualification := ReportQualification{ReportID: report.ReportID, Revision: 1, Eligible: report.FormalEligible, GateRevision: gate.Revision, UpdatedAt: api.Time(now)}
	if !qualification.Eligible {
		qualification.Reason = "exploratory_or_exposure_invalidated"
	}
	if err = tx.Create(ctx, ns("report_qualification"), report.ReportID, plan.SourceGroup, qualification); err != nil {
		return report, err
	}
	run.State = "sealed"
	run.GateOpen = false
	run.Revision = rev + 1
	ref := tx.Scope().Ref(report.ReportID, 1)
	run.ReportRef = &ref
	if err = tx.Put(ctx, ns("runs"), runID, rev, run); err != nil {
		return report, err
	}
	if _, err = tx.Raise(ctx, "governance.eval_cancel", runID, tx.Scope().Ref(runID, run.Revision), now); err != nil {
		return report, err
	}
	return report, nil
}

func summarize(plan EvaluationPlan, run EvaluationRun, samples []SampleRun, sealed string) (EvaluationReport, error) {
	out := EvaluationReport{PlanID: plan.PlanID, RunID: run.RunID, Purpose: plan.Purpose, FullDenominator: plan.FullDenominator, SealedAt: sealed, TargetAttainment: "fail", StatisticalGate: "fail", ImprovementGate: "fail", CostGate: "pass", CandidateCost: []api.Amount{}, BaselineCost: []api.Amount{}, CandidateWorstCaseCost: []api.Amount{}, Qualifications: []string{"paired exact binomial; Wilson target interval"}}
	if plan.FullDenominator == 0 {
		return out, api.E("invalid_state", "manifest_empty")
	}
	bySample := map[string]map[string]SampleRun{}
	hash := sha256.New()
	cutoff, err := api.ParseTime(plan.ObservationCutoff)
	if err != nil {
		return out, err
	}
	latencyRegression := false
	for _, original := range samples {
		b, err := api.Canonical(api.Raw(original))
		if err != nil {
			return out, err
		}
		hash.Write(b)
		hash.Write([]byte{10})
		sample := original
		if sample.ObservedAt != "" {
			observed, err := api.ParseTime(sample.ObservedAt)
			if err != nil || observed.After(cutoff) {
				sample.Outcome = "unknown"
			}
		}
		if bySample[sample.SampleID] == nil {
			bySample[sample.SampleID] = map[string]SampleRun{}
		}
		bySample[sample.SampleID][sample.Arm] = sample
		if sample.Arm == "candidate" {
			out.CandidateCost, err = amountsAdd(out.CandidateCost, sample.Usage)
			if err != nil {
				return out, err
			}
			if sample.Outcome == "pass" {
				out.CandidateSuccess++
			}
			if sample.ModelClaimedComplete {
				out.CandidateModelComplete++
			}
			if sample.SystemAccepted {
				out.CandidateSystemAccepted++
			}
			if sample.SystemAccepted && sample.Outcome != "pass" {
				out.FalseAccept++
			}
			if !sample.SystemAccepted && sample.Outcome == "pass" {
				out.FalseReject++
			}
			if contains([]string{"unknown", "timeout", "not_run"}, sample.Outcome) {
				out.TimeoutUnknownNotRun++
				out.MissingCount++
			}
			worst := sample.Usage
			if !sample.UsageFinal {
				if len(sample.CostUpperBound) == 0 {
					out.CostGate = "unknown"
				} else {
					worst = sample.CostUpperBound
				}
			}
			out.CandidateWorstCaseCost, err = amountsAdd(out.CandidateWorstCaseCost, worst)
			if err != nil {
				return out, err
			}
			if sample.LatencyMillis > plan.Thresholds.MaximumLatencyMillis {
				latencyRegression = true
			}
		} else {
			out.BaselineCost, err = amountsAdd(out.BaselineCost, sample.Usage)
			if err != nil {
				return out, err
			}
			if sample.Outcome == "pass" {
				out.BaselineSuccess++
			}
		}
	}
	for _, arms := range bySample {
		candidate, baseline := arms["candidate"], arms["baseline"]
		if candidate.Outcome == "pass" && baseline.Outcome != "pass" {
			out.PairWins++
		}
		if candidate.Outcome != "pass" && baseline.Outcome == "pass" {
			out.PairLosses++
		}
	}
	n := float64(plan.FullDenominator)
	rate := float64(out.CandidateSuccess) / n
	minimum, _ := strconv.ParseFloat(plan.Thresholds.MinimumTargetRate, 64)
	improvement, _ := strconv.ParseFloat(plan.Thresholds.MinimumImprovement, 64)
	confidence, _ := strconv.ParseFloat(plan.Thresholds.ConfidenceLevel, 64)
	z := 1.959963984540054
	if confidence == 0.90 {
		z = 1.6448536269514722
	} else if confidence == 0.99 {
		z = 2.5758293035489004
	}
	den := 1 + z*z/n
	center := (rate + z*z/(2*n)) / den
	half := z * math.Sqrt(rate*(1-rate)/n+z*z/(4*n*n)) / den
	lower, upper := math.Max(0, center-half), math.Min(1, center+half)
	out.ConfidenceLower = strconv.FormatFloat(lower, 'f', 9, 64)
	out.ConfidenceUpper = strconv.FormatFloat(upper, 'f', 9, 64)
	if rate >= minimum {
		out.TargetAttainment = "pass"
	}
	discrepant := out.PairWins + out.PairLosses
	pvalue := 1.0
	if discrepant > 0 {
		pvalue = 0
		for k := out.PairWins; k <= discrepant; k++ {
			logC, _ := math.Lgamma(float64(discrepant + 1))
			logK, _ := math.Lgamma(float64(k + 1))
			logR, _ := math.Lgamma(float64(discrepant - k + 1))
			pvalue += math.Exp(logC - logK - logR - float64(discrepant)*math.Ln2)
		}
	}
	if lower >= minimum && pvalue <= 1-confidence && plan.FullDenominator >= plan.Thresholds.MinimumSamples {
		out.StatisticalGate = "pass"
	}
	difference := (float64(out.CandidateSuccess) - float64(out.BaselineSuccess)) / n
	limit, err := multiplyDecimal(plan.Thresholds.MaximumCostPerSample, plan.FullDenominator)
	if err != nil {
		return out, err
	}
	if !bounded(out.CandidateWorstCaseCost, []api.Amount{{Unit: plan.Thresholds.CostUnit, Value: limit}}) {
		out.CostGate = "fail"
	}
	if difference > 0 && difference >= improvement && out.CostGate == "pass" && !latencyRegression {
		out.ImprovementGate = "pass"
	} else if out.CostGate == "unknown" {
		out.ImprovementGate = "unknown"
	}
	if latencyRegression {
		out.Qualifications = append(out.Qualifications, "latency_regression")
	}
	out.InputsDigest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	return out, nil
}
