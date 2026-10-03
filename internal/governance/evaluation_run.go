package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func sampleKey(plan, sample, arm string) string {
	return digestID("sample", []string{plan, sample, arm})
}
func environmentKey(run, sample, arm string) string { return run + "/" + sample + "/" + arm }
func (s *Service) createRun(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in RunRequest) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	var plan EvaluationPlan
	if _, err := tx.Get(ctx, ns("plans"), in.PlanID, &plan); err != nil {
		return runtime.Outcome{}, err
	}
	if !plan.Frozen {
		return runtime.Outcome{}, api.E("invalid_state", "plan_not_frozen")
	}
	if !api.ValidID(in.RunID) {
		return runtime.Outcome{}, api.E("invalid_request", "invalid_run_id")
	}
	if plan.Purpose == "formal" {
		gate, err := s.exposureGate(ctx, tx, plan.SourceGroup)
		if err != nil {
			return runtime.Outcome{}, err
		}
		if gate.KnownExposure || !plan.FormalEligible {
			return runtime.Outcome{}, api.E("forbidden", "exposure_invalidated")
		}
	}
	key, err := tx.LookupKey(ctx, ns("runs"), plan.PlanID)
	if err == nil {
		var old EvaluationRun
		if _, err = tx.Get(ctx, ns("runs"), key.ObjectID, &old); err != nil {
			return runtime.Outcome{}, err
		}
		if key.ObjectID != in.RunID {
			return runtime.Outcome{}, api.E("idempotency_conflict", "plan_has_one_logical_run")
		}
		return runtime.Applied(old), nil
	}
	if !errMissing(err) {
		return runtime.Outcome{}, err
	}
	if s.Ports.Runner == nil {
		return runtime.Outcome{}, api.E("unsupported", "evaluation_runner_unavailable")
	}
	if admission, ok := s.Ports.Runner.(EvaluationPlanAdmission); ok {
		if err := admission.CheckEvaluationPlan(plan); err != nil {
			return runtime.Outcome{}, err
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	run := EvaluationRun{RunID: in.RunID, PlanID: plan.PlanID, Revision: 1, State: "prepared", GateOpen: true, FormalEligible: plan.FormalEligible, CreatedAt: api.Time(now), Samples: api.CollectionSummary{CollectionRevision: 1, TotalCount: 2 * plan.FullDenominator, UnresolvedCount: 2 * plan.FullDenominator}, Environments: api.CollectionSummary{CollectionRevision: 1}}
	if err = tx.Create(ctx, ns("runs"), run.RunID, plan.PlanID, run); err != nil {
		return runtime.Outcome{}, err
	}
	digest, err := api.Digest(in)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Bind(ctx, ns("runs"), plan.PlanID, run.RunID, digest); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.evaluation", run.RunID, tx.Scope().Ref(run.RunID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(run), nil
}

func (s *Service) buildSampleIndex(ctx context.Context, tx runtime.Tx, run *EvaluationRun, plan EvaluationPlan) (bool, error) {
	if run.Samples.Complete {
		return true, nil
	}
	rows, err := tx.List(ctx, ns("manifest_samples"), plan.PlanID, run.Cursor, 100)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		var sample EvaluationSample
		if err = row.Decode(&sample); err != nil {
			return false, err
		}
		for _, arm := range []string{"candidate", "baseline"} {
			id := sampleKey(plan.PlanID, sample.SampleID, arm)
			sr := SampleRun{SampleRunID: id, Revision: 1, PlanID: plan.PlanID, RunID: run.RunID, SampleID: sample.SampleID, Arm: arm, EnvironmentKey: environmentKey(run.RunID, sample.SampleID, arm), Outcome: "not_run", Usage: []api.Amount{}, CostUpperBound: []api.Amount{}, OperationRefs: []api.ObjectRef{}, AttemptRefs: []api.ObjectRef{}}
			if err = tx.Create(ctx, ns("sample_runs"), id, run.RunID, sr); err != nil {
				return false, err
			}
			if err = tx.Create(ctx, ns("sample_manifest"), id, run.RunID, sample); err != nil {
				return false, err
			}
		}
		run.Cursor = row.ID
	}
	if len(rows) < 100 {
		run.Samples.Complete = true
		run.Cursor = ""
		return true, nil
	}
	return false, nil
}
func (s *Service) startGate(ctx context.Context, tx runtime.Tx, runID string, plan EvaluationPlan, claim api.Claim) error {
	if err := tx.Guard(ctx, claim); err != nil {
		return err
	}
	var current EvaluationRun
	if _, err := tx.Get(ctx, ns("runs"), runID, &current); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if !current.GateOpen || before(now, plan.ObservationCutoff) != nil {
		return api.E("invalid_state", "evaluation_start_closed")
	}
	if plan.Purpose == "formal" {
		gate, err := s.exposureGate(ctx, tx, plan.SourceGroup)
		if err != nil {
			return err
		}
		if gate.KnownExposure {
			return api.E("forbidden", "exposure_invalidated")
		}
	}
	return nil
}

func (s *Service) continueEvaluation(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var run EvaluationRun
	if _, err := store.Read(ctx, scope, ns("runs"), work.Job.ResponsibilityKey, 0, &run); err != nil {
		return err
	}
	var plan EvaluationPlan
	if _, err := store.Read(ctx, scope, ns("plans"), run.PlanID, 0, &plan); err != nil {
		return err
	}
	if !run.Samples.Complete {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			var current EvaluationRun
			rev, err := tx.Get(ctx, ns("runs"), run.RunID, &current)
			if err != nil {
				return err
			}
			_, err = s.buildSampleIndex(ctx, tx, &current, plan)
			if err != nil {
				return err
			}
			current.Revision = rev + 1
			if current.Samples.Complete && current.State == "prepared" {
				current.State = "running"
			}
			if err = tx.Put(ctx, ns("runs"), run.RunID, rev, current); err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		})
	}
	if !run.GateOpen || run.State == "cancelling" || run.State == "sealed" {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
	}
	if s.Ports.Runner == nil {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			var current EvaluationRun
			rev, err := tx.Get(ctx, ns("runs"), run.RunID, &current)
			if err != nil {
				return err
			}
			current.BlockedReason = "independent_isolated_runner_unavailable"
			current.Revision = rev + 1
			return tx.Put(ctx, ns("runs"), run.RunID, rev, current)
		})
	}
	rows, err := store.List(ctx, scope, ns("sample_runs"), run.RunID, run.Cursor, 100)
	if err != nil {
		return err
	}
	var selected *SampleRun
	for _, row := range rows {
		var sample SampleRun
		if err = row.Decode(&sample); err != nil {
			return err
		}
		if sample.Outcome == "not_run" || sample.Outcome == "fail" && sample.SafeRetry && sample.Attempts < plan.MaximumAttempts && !sample.MayApplyLater {
			selected = &sample
			break
		}
	}
	if selected == nil {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			var current EvaluationRun
			rev, err := tx.Get(ctx, ns("runs"), run.RunID, &current)
			if err != nil {
				return err
			}
			if len(rows) == 100 {
				current.Cursor = rows[len(rows)-1].ID
				current.Revision = rev + 1
				if err = tx.Put(ctx, ns("runs"), run.RunID, rev, current); err != nil {
					return err
				}
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				return tx.Hint(ctx, work.Job.JobID, now)
			}
			return nil
		})
	}
	var sample EvaluationSample
	if _, err = store.Read(ctx, scope, ns("sample_manifest"), selected.SampleRunID, 0, &sample); err != nil {
		return err
	}
	pair := RunnerPair{RunID: run.RunID, Plan: plan, Sample: sample, CandidateEnvironmentKey: environmentKey(run.RunID, sample.SampleID, "candidate"), BaselineEnvironmentKey: environmentKey(run.RunID, sample.SampleID, "baseline"), StartBefore: plan.ObservationCutoff}
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := s.startGate(ctx, tx, run.RunID, plan, work.Claim); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if s.Ports.Proof != nil {
			digest, err := api.Digest(pair)
			if err != nil {
				return err
			}
			pair.Permit, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: scope.TenantID, IssuerID: scope.OwnerID, AudienceID: scope.OwnerID, Purpose: "evaluation_prepare", ObjectRef: scope.Ref(selected.SampleRunID, 1), Digest: digest, IssuedAt: api.Time(now), StartBefore: plan.ObservationCutoff})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	prepared, err := s.Ports.Runner.PreparePair(ctx, pair)
	if err != nil {
		return err
	}
	if !prepared.CandidatePrepared || !prepared.BaselinePrepared {
		return api.E("dependency_unavailable", "both_arms_must_be_prepared")
	}
	var attempt SampleAttempt
	fresh := false
	status, err = store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := s.startGate(ctx, tx, run.RunID, plan, work.Claim); err != nil {
			return err
		}
		var current SampleRun
		rev, err := tx.Get(ctx, ns("sample_runs"), selected.SampleRunID, &current)
		if err != nil {
			return err
		}
		if len(current.AttemptRefs) > 0 {
			last := current.AttemptRefs[len(current.AttemptRefs)-1]
			var old SampleAttempt
			if _, err = tx.Get(ctx, ns("sample_attempts"), last.ObjectID, &old); err != nil {
				return err
			}
			if old.State == "send_started" {
				attempt = old
				return nil
			}
			if old.Observation == nil || !old.Observation.SafeRetry || old.Observation.MayApplyLater {
				return api.E("invalid_state", "unsafe_retry")
			}
		}
		index := current.Attempts + 1
		if index > plan.MaximumAttempts {
			return api.E("invalid_state", "attempt_limit")
		}
		id := digestID("attempt", []any{selected.SampleRunID, index})
		implementation := plan.CandidateRef
		if selected.Arm == "baseline" {
			implementation = plan.BaselineRef
		}
		request := RunnerAttempt{RunID: run.RunID, PlanID: plan.PlanID, SampleID: sample.SampleID, Arm: selected.Arm, AttemptID: id, EnvironmentKey: selected.EnvironmentKey, ImplementationRef: implementation, InputRef: sample.InputRef, TruthRef: sample.TruthRef, StartBefore: plan.ObservationCutoff, Seed: plan.Seed, Budget: plan.Budget}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if s.Ports.Proof != nil {
			digest, err := api.Digest(request)
			if err != nil {
				return err
			}
			request.Permit, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: scope.TenantID, IssuerID: scope.OwnerID, AudienceID: scope.OwnerID, Purpose: "evaluation_start", ObjectRef: scope.Ref(id, 1), Digest: digest, IssuedAt: api.Time(now), StartBefore: plan.ObservationCutoff})
			if err != nil {
				return err
			}
		}
		attempt = SampleAttempt{AttemptID: id, Revision: 1, SampleRunID: selected.SampleRunID, Index: index, State: "send_started", Request: request}
		if err = tx.Create(ctx, ns("sample_attempts"), id, selected.SampleRunID, attempt); err != nil {
			return err
		}
		current.Attempts = index
		current.AttemptRefs = append(current.AttemptRefs, scope.Ref(id, 1))
		current.Revision = rev + 1
		fresh = true
		return tx.Put(ctx, ns("sample_runs"), current.SampleRunID, rev, current)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	observation, known, err := s.Ports.Runner.Lookup(ctx, attempt.Request)
	if err != nil {
		return err
	}
	if !known {
		if !fresh {
			return api.E("effect_unknown", "evaluation_attempt_lookup_unknown")
		}
		observation, err = s.Ports.Runner.Run(ctx, attempt.Request)
		if err != nil {
			return err
		}
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var currentAttempt SampleAttempt
		arev, err := tx.Get(ctx, ns("sample_attempts"), attempt.AttemptID, &currentAttempt)
		if err != nil {
			return err
		}
		if currentAttempt.State == "observed" {
			return nil
		}
		if err = validateObservation(scope, observation); err != nil {
			return err
		}
		if !api.Equal(observation.TruthRef, sample.TruthRef) {
			return api.E("forbidden", "truth_binding_mismatch")
		}
		currentAttempt.State = "observed"
		currentAttempt.Observation = &observation
		currentAttempt.Revision = arev + 1
		if err = tx.Put(ctx, ns("sample_attempts"), attempt.AttemptID, arev, currentAttempt); err != nil {
			return err
		}
		var current SampleRun
		rev, err := tx.Get(ctx, ns("sample_runs"), selected.SampleRunID, &current)
		if err != nil {
			return err
		}
		if err = applyObservation(&current, observation); err != nil {
			return err
		}
		current.Revision = rev + 1
		if err = tx.Put(ctx, ns("sample_runs"), current.SampleRunID, rev, current); err != nil {
			return err
		}
		var latest EvaluationRun
		rrev, err := tx.Get(ctx, ns("runs"), run.RunID, &latest)
		if err != nil {
			return err
		}
		if selected.Outcome == "not_run" && latest.Samples.UnresolvedCount > 0 {
			latest.Samples.UnresolvedCount--
		}
		latest.Samples.CollectionRevision++
		latest.Revision = rrev + 1
		if err = tx.Put(ctx, ns("runs"), run.RunID, rrev, latest); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		return tx.Hint(ctx, work.Job.JobID, now)
	})
}
func validateObservation(scope runtime.Scope, o AttemptObservation) error {
	if _, err := api.ParseTime(o.ObservedAt); err != nil {
		return api.E("invalid_request", "observation_time_required")
	}
	if !contains([]string{"pass", "fail", "timeout", "unknown"}, o.Outcome) || o.TruthRef.TenantID != scope.TenantID || o.ProofRef.TenantID != scope.TenantID || o.OperationRef.TenantID != scope.TenantID {
		return api.E("forbidden", "independent_observation_binding_invalid")
	}
	if err := api.ValidateAmounts(o.Usage); err != nil {
		return err
	}
	if err := api.ValidateAmounts(o.CostUpperBound); err != nil {
		return err
	}
	if len(o.CostUpperBound) > 0 && !bounded(o.Usage, o.CostUpperBound) {
		return api.E("invalid_request", "cost_upper_bound_below_actual")
	}
	return nil
}
func applyObservation(s *SampleRun, o AttemptObservation) error {
	usage, err := amountsAdd(s.Usage, o.Usage)
	if err != nil {
		return err
	}
	s.Outcome = o.Outcome
	s.Usage = usage
	s.UsageFinal = o.UsageFinal
	s.CostUpperBound = o.CostUpperBound
	s.LatencyMillis += o.LatencyMillis
	s.ModelClaimedComplete = o.ModelClaimedComplete
	s.SystemAccepted = o.SystemAccepted
	s.MayApplyLater = o.MayApplyLater
	s.SafeRetry = o.SafeRetry
	s.ObservedAt = o.ObservedAt
	s.TruthRef = &o.TruthRef
	s.OperationRefs = append(s.OperationRefs, o.OperationRef)
	return nil
}

func (s *Service) cancelRun(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in IDInput) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	var run EvaluationRun
	rev, err := tx.Get(ctx, ns("runs"), in.ID, &run)
	if err != nil {
		return runtime.Outcome{}, err
	}
	run.GateOpen = false
	if run.State != "sealed" {
		run.State = "cancelling"
	}
	run.Revision = rev + 1
	if err = tx.Put(ctx, ns("runs"), in.ID, rev, run); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.eval_cancel", in.ID, tx.Scope().Ref(in.ID, run.Revision), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(in.ID, run.Revision), State: "cancelling"}), nil
}
func (s *Service) continueEvaluationCancel(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var run EvaluationRun
	if _, err := store.Read(ctx, scope, ns("runs"), work.Job.ResponsibilityKey, 0, &run); err != nil {
		return err
	}
	var plan EvaluationPlan
	if _, err := store.Read(ctx, scope, ns("plans"), run.PlanID, 0, &plan); err != nil {
		return err
	}
	if !run.Samples.Complete {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			var current EvaluationRun
			rev, err := tx.Get(ctx, ns("runs"), run.RunID, &current)
			if err != nil {
				return err
			}
			if _, err = s.buildSampleIndex(ctx, tx, &current, plan); err != nil {
				return err
			}
			current.Revision = rev + 1
			if err = tx.Put(ctx, ns("runs"), run.RunID, rev, current); err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		})
	}
	rows, err := store.List(ctx, scope, ns("sample_runs"), run.RunID, run.CancellationCursor, 100)
	if err != nil {
		return err
	}
	stops := map[string]PairStopEvidence{}
	for _, row := range rows {
		var sr SampleRun
		if err = row.Decode(&sr); err != nil {
			return err
		}
		if s.Ports.Runner != nil {
			var sample EvaluationSample
			if _, err = store.Read(ctx, scope, ns("sample_manifest"), sr.SampleRunID, 0, &sample); err != nil {
				return err
			}
			pair := RunnerPair{RunID: run.RunID, Plan: plan, Sample: sample, CandidateEnvironmentKey: environmentKey(run.RunID, sample.SampleID, "candidate"), BaselineEnvironmentKey: environmentKey(run.RunID, sample.SampleID, "baseline"), StartBefore: plan.ObservationCutoff}
			stop, err := s.Ports.Runner.Seal(ctx, pair)
			if err != nil {
				return err
			}
			stops[sr.SampleRunID] = stop
		}
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var current EvaluationRun
		rev, err := tx.Get(ctx, ns("runs"), run.RunID, &current)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var sample SampleRun
			srev, err := tx.Get(ctx, ns("sample_runs"), row.ID, &sample)
			if err != nil {
				return err
			}
			if stop, ok := stops[sample.SampleRunID]; ok {
				sample.StopConfirmed = stop.CandidateStopped && stop.BaselineStopped && !stop.MayApplyLater
				sample.EnvironmentDestroyed = stop.CandidateDestroyed && stop.BaselineDestroyed
				sample.MayApplyLater = stop.MayApplyLater
			} else if sample.Attempts == 0 {
				sample.StopConfirmed = true
				sample.EnvironmentDestroyed = true
			}
			sample.Revision = srev + 1
			if err = tx.Put(ctx, ns("sample_runs"), sample.SampleRunID, srev, sample); err != nil {
				return err
			}
			current.CancellationCursor = row.ID
		}
		current.Revision = rev + 1
		current.Samples.CollectionRevision++
		if len(rows) < 100 {
			current.BlockedReason = "cancel_scanned; per-sample stopping/usage/residual facts remain independent"
		}
		if err = tx.Put(ctx, ns("runs"), run.RunID, rev, current); err != nil {
			return err
		}
		if len(rows) == 100 {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		}
		return nil
	})
}
func (s *Service) readEvaluation(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in IDInput) (EvaluationRead, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return EvaluationRead{}, err
	}
	var out EvaluationRead
	if _, err := store.Read(ctx, scope, ns("runs"), in.ID, 0, &out.Run); err != nil {
		return out, err
	}
	if _, err := store.Read(ctx, scope, ns("plans"), out.Run.PlanID, 0, &out.Plan); err != nil {
		return out, err
	}
	if out.Run.ReportRef != nil {
		var report EvaluationReport
		if _, err := store.Read(ctx, scope, ns("reports"), out.Run.ReportRef.ObjectID, 0, &report); err != nil {
			return out, err
		}
		out.Report = &report
		var qualification ReportQualification
		if _, err := store.Read(ctx, scope, ns("report_qualification"), report.ReportID, 0, &qualification); err != nil {
			return out, err
		}
		out.Qualification = &qualification
		gate, err := s.exposeRead(ctx, store, scope, a, out.Plan, report.ReportID, "feedback", out.Run.ReportRef)
		if err != nil {
			return EvaluationRead{}, err
		}
		if gateInvalidatesReport(gate, report) {
			out.Qualification.Eligible = false
			out.Qualification.Reason = "exposure_invalidated"
		}
	}
	return out, nil
}
func (s *Service) readSamples(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in SamplePageRequest) (api.Page[SampleRun], error) {
	out := api.Page[SampleRun]{Items: []SampleRun{}, Gaps: []string{}}
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return out, err
	}
	if in.Limit < 1 || in.Limit > 100 {
		return out, api.E("invalid_request", "invalid_page_limit")
	}
	var run EvaluationRun
	if _, err := store.Read(ctx, scope, ns("runs"), in.RunID, 0, &run); err != nil {
		return out, err
	}
	var plan EvaluationPlan
	if _, err := store.Read(ctx, scope, ns("plans"), run.PlanID, 0, &plan); err != nil {
		return out, err
	}
	if _, err := s.exposeRead(ctx, store, scope, a, plan, run.RunID+"/samples", "debug", run.ReportRef); err != nil {
		return out, err
	}
	rows, err := store.List(ctx, scope, ns("sample_runs"), in.RunID, in.Cursor, int(in.Limit)+1)
	if err != nil {
		return out, err
	}
	out.CollectionRevision = run.Samples.CollectionRevision
	out.Exhausted = len(rows) <= int(in.Limit)
	if !out.Exhausted {
		rows = rows[:in.Limit]
	}
	for _, row := range rows {
		var sr SampleRun
		if err = row.Decode(&sr); err != nil {
			return out, err
		}
		out.Items = append(out.Items, sr)
		out.NextCursor = row.ID
	}
	if out.Exhausted {
		out.NextCursor = ""
	}
	out.Partial = !run.Samples.Complete
	if out.Partial {
		out.Gaps = []string{"sample_index_preparing"}
	}
	return out, nil
}
