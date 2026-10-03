package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type PlanPending struct {
	ID                   string         `json:"id"`
	Revision             uint64         `json:"revision"`
	Plan                 EvaluationPlan `json:"plan"`
	CommandID            string         `json:"command_id"`
	SubjectID            string         `json:"subject_id"`
	CredentialGeneration uint64         `json:"credential_generation"`
	Roles                []string       `json:"roles"`
	IndexedCount         uint64         `json:"indexed_count"`
	State                string         `json:"state"`
}

func (s *Service) registerEvaluation(r *runtime.Registry) error {
	for _, fn := range []func() error{
		func() error {
			return registerCommand[ImprovementPolicy, StateOutput](s, r, "evaluation.improvement_policy.create", false, false, s.createImprovementPolicy)
		},
		func() error {
			return registerCommand[PlanCreate, StateOutput](s, r, "evaluation.plan_create", false, true, s.createPlan)
		},
		func() error {
			return registerCommand[RunRequest, EvaluationRun](s, r, "evaluation.run", false, false, s.createRun)
		},
		func() error {
			return registerCommand[IDInput, StateOutput](s, r, "evaluation.cancel", false, false, s.cancelRun)
		},
		func() error {
			return registerCommand[IDInput, EvaluationReport](s, r, "evaluation.seal", false, false, s.sealRun)
		},
		func() error { return registerQuery[IDInput, EvaluationRead](r, "evaluation.read", s.readEvaluation) },
		func() error {
			return registerQuery[IDInput, EvaluationPlan](r, "evaluation.plan.read", queryByID[EvaluationPlan]("plans", func(a runtime.Auth, p EvaluationPlan) error { return requireRole(a, "evaluation_authority") }))
		},
		func() error {
			return registerQuery[SamplePageRequest, api.Page[SampleRun]](r, "evaluation.samples", s.readSamples)
		},
		func() error {
			return registerCommand[ExposureRegister, StateOutput](s, r, "evaluation.exposure.register", false, false, s.registerExposure)
		},
		func() error {
			return registerCommand[FeedbackOpen, EvaluationReport](s, r, "evaluation.feedback_open", false, false, s.feedbackOpen)
		},
		func() error {
			return registerCommand[LateObservation, StateOutput](s, r, "evaluation.observation.merge", false, false, s.mergeLateObservation)
		},
	} {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) createImprovementPolicy(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ImprovementPolicy) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Revision != 1 || in.FormalAttemptsUsed != 0 || in.FormalAttemptLimit == 0 || in.FormalAttemptLimit > 100 || in.Stopped || in.ID != c.TargetID || !api.ValidID(in.LineageID) {
		return runtime.Outcome{}, api.E("invalid_request", "improvement_policy_invalid")
	}
	if err := tx.Create(ctx, ns("improvement_policies"), in.ID, in.LineageID, in); err != nil {
		return runtime.Outcome{}, err
	}
	digest, err := api.Digest(in.PolicyRef)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Bind(ctx, ns("improvement_policies"), in.LineageID, in.ID, digest); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(in.ID, 1), State: "frozen"}), nil
}
func (s *Service) createPlan(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in PlanCreate) (runtime.Outcome, error) {
	if err := requireRole(a, "evaluation_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	p := in.Plan
	if p.PlanID != c.TargetID || p.Revision != 1 || p.Frozen || p.FullDenominator != 0 || p.FormalAttemptIndex != 0 || p.FormalEligible || p.ExposureRevision != 0 || p.SourceGroup == "" || p.MaximumAttempts < 1 || p.MaximumAttempts > 10 || p.UnknownOutcomePolicy != "fail_at_cutoff" || p.CostUnknownPolicy != "bounded_worst_case" || p.Thresholds.StatisticalMethod != "paired_exact_binomial" || p.Thresholds.MinimumSamples < 1 || p.Thresholds.CostUnit == "" {
		return runtime.Outcome{}, api.E("invalid_request", "plan_not_frozen")
	}
	if !contains([]string{"formal", "exploratory", "conformance"}, p.Purpose) {
		return runtime.Outcome{}, api.E("invalid_request", "invalid_evaluation_purpose")
	}
	if p.Purpose == "formal" && (p.ImprovementPolicyRef == nil || !api.ValidID(p.ReleaseRequestID)) {
		return runtime.Outcome{}, api.E("invalid_request", "formal_policy_required")
	}
	if p.Purpose == "formal" && s.Ports.FormalPlanGate == nil {
		return runtime.Outcome{}, api.E("unsupported", "registered_formal_lineage_partition_unavailable")
	}
	if admission, ok := s.Ports.Runner.(EvaluationPlanAdmission); ok {
		if err := admission.CheckEvaluationPlan(p); err != nil {
			return runtime.Outcome{}, err
		}
	}
	if err := ownerRef(tx.Scope(), p.PartitionRef); err != nil {
		return runtime.Outcome{}, api.E("unsupported", "cross_owner_partition_not_supported")
	}
	for _, rate := range []string{p.Thresholds.MinimumTargetRate, p.Thresholds.MinimumImprovement, p.Thresholds.ConfidenceLevel} {
		cmp, err := api.CompareDecimal(rate, "1")
		if err != nil || cmp > 0 {
			return runtime.Outcome{}, api.E("invalid_request", "invalid_evaluation_threshold")
		}
	}
	if !contains([]string{"0.90", "0.95", "0.99"}, p.Thresholds.ConfidenceLevel) {
		return runtime.Outcome{}, api.E("unsupported", "confidence_level_not_supported")
	}
	if _, err := api.CompareDecimal(p.Thresholds.MaximumCostPerSample, "0"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := api.ValidateAmounts(p.Budget); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = before(now, p.ObservationCutoff); err != nil {
		return runtime.Outcome{}, err
	}
	if s.Ports.Content == nil {
		return runtime.Outcome{}, api.E("unsupported", "evaluation_manifest_content_unavailable")
	}
	pending := PlanPending{ID: p.PlanID, Revision: 1, Plan: p, CommandID: c.CommandID, SubjectID: a.SubjectID, CredentialGeneration: a.CredentialGeneration, Roles: a.Roles, State: "indexing"}
	if err = tx.Create(ctx, ns("plan_pending"), p.PlanID, c.CommandID, pending); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.plan", p.PlanID, tx.Scope().Ref(p.PlanID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(p.PlanID, 1), State: "manifest_preparing"}), nil
}

// 内容 manifest 可大于单个线帧，逐样本采用同版严格 JSON/闭合 Schema。
func decodeManifest(b []byte) ([]EvaluationSample, error) {
	if len(b) > 16<<20 {
		return nil, api.E("invalid_request", "manifest_too_large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, api.E("invalid_request", "manifest_invalid")
	}
	key, err := d.Token()
	if err != nil || key != "samples" {
		return nil, api.E("invalid_request", "manifest_invalid")
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, api.E("invalid_request", "manifest_invalid")
	}
	out := []EvaluationSample{}
	seen := map[string]bool{}
	validator, err := api.NewValidator(api.SchemaFor[EvaluationSample]())
	if err != nil {
		return nil, err
	}
	for d.More() {
		if len(out) >= 10000 {
			return nil, api.E("invalid_request", "manifest_sample_limit")
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return nil, api.E("invalid_request", "manifest_invalid")
		}
		if err = validator.Validate(raw); err != nil {
			return nil, err
		}
		var sample EvaluationSample
		if err = api.Decode(raw, &sample); err != nil {
			return nil, err
		}
		if !api.ValidID(sample.SampleID) || seen[sample.SampleID] || sample.Class == "" {
			return nil, api.E("invalid_request", "manifest_duplicate_or_invalid_sample")
		}
		seen[sample.SampleID] = true
		out = append(out, sample)
	}
	if token, err = d.Token(); err != nil || token != json.Delim(']') {
		return nil, api.E("invalid_request", "manifest_invalid")
	}
	if d.More() {
		return nil, api.E("invalid_request", "manifest_unknown_or_duplicate_field")
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, api.E("invalid_request", "manifest_invalid")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, api.E("invalid_request", "manifest_trailing_data")
	}
	if len(out) == 0 {
		return nil, api.E("invalid_request", "manifest_empty")
	}
	return out, nil
}
func (s *Service) exposureGate(ctx context.Context, tx runtime.Tx, group string) (ExposureGate, error) {
	id := digestID("exposure_gate", group)
	var gate ExposureGate
	_, err := tx.Get(ctx, ns("exposure_gates"), id, &gate)
	if errMissing(err) {
		gate = ExposureGate{ID: id, Revision: 1, SourceGroup: group}
		err = tx.Create(ctx, ns("exposure_gates"), id, "", gate)
	}
	return gate, err
}

func (s *Service) continuePlan(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending PlanPending
	if _, err := store.Read(ctx, scope, ns("plan_pending"), work.Job.ResponsibilityKey, 0, &pending); err != nil {
		return err
	}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: pending.SubjectID, CredentialGeneration: pending.CredentialGeneration, Roles: pending.Roles}
	b, readErr := s.Ports.Content.Read(ctx, scope, auth, pending.Plan.ManifestRef, "evaluation.manifest")
	var samples []EvaluationSample
	var decodeErr error
	if readErr == nil {
		if api.Hash(b) != pending.Plan.ManifestRef.Hash || uint64(len(b)) != pending.Plan.ManifestRef.ByteLength {
			decodeErr = api.E("invalid_request", "manifest_digest_mismatch")
		} else {
			samples, decodeErr = decodeManifest(b)
		}
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var current PlanPending
		rev, err := tx.Get(ctx, ns("plan_pending"), pending.ID, &current)
		if err != nil {
			return err
		}
		if current.State != "indexing" {
			return nil
		}
		if readErr != nil || decodeErr != nil {
			current.State = "blocked"
			current.Revision = rev + 1
			if err = tx.Put(ctx, ns("plan_pending"), current.ID, rev, current); err != nil {
				return err
			}
			return runtime.Decide(ctx, tx, current.CommandID, nil, api.E("invalid_request", "manifest_unavailable_or_invalid"))
		}
		end := current.IndexedCount + 100
		if end > uint64(len(samples)) {
			end = uint64(len(samples))
		}
		for i := current.IndexedCount; i < end; i++ {
			sample := samples[i]
			if sample.InputRef.TenantID != scope.TenantID || sample.TruthRef.TenantID != scope.TenantID {
				return api.E("forbidden", "manifest_scope_mismatch")
			}
			id := fmt.Sprintf("%020d", i)
			if err = tx.Create(ctx, ns("manifest_samples"), current.ID+"/"+id, current.ID, sample); err != nil {
				return err
			}
		}
		current.IndexedCount = end
		current.Revision = rev + 1
		if err = tx.Put(ctx, ns("plan_pending"), current.ID, rev, current); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if end < uint64(len(samples)) {
			return tx.Hint(ctx, work.Job.JobID, now)
		}
		p := current.Plan
		p.Frozen = true
		p.FullDenominator = uint64(len(samples))
		p.ManifestDigest = api.Hash(b)
		err = tx.Savepoint(ctx, func(inner runtime.Tx) error {
			gate, e := s.exposureGate(ctx, inner, p.SourceGroup)
			if e != nil {
				return e
			}
			p.ExposureRevision = gate.Revision
			p.FormalEligible = p.Purpose == "formal" && !gate.KnownExposure
			if p.Purpose == "formal" {
				if !p.FormalEligible {
					return api.E("forbidden", "exposure_invalidated")
				}
				if s.Ports.FormalPlanGate == nil {
					return api.E("unsupported", "registered_formal_lineage_partition_unavailable")
				}
				if e = s.Ports.FormalPlanGate.CheckTx(ctx, inner, p); e != nil {
					return e
				}
				if e = ownerRef(scope, *p.ImprovementPolicyRef); e != nil {
					return e
				}
				var policy ImprovementPolicy
				prev, e := inner.Get(ctx, ns("improvement_policies"), p.ImprovementPolicyRef.ObjectID, &policy)
				if e != nil {
					return e
				}
				if policy.Stopped || policy.FormalAttemptsUsed >= policy.FormalAttemptLimit {
					return api.E("invalid_state", "formal_attempt_exhausted")
				}
				policy.FormalAttemptsUsed++
				policy.Revision = prev + 1
				p.FormalAttemptIndex = policy.FormalAttemptsUsed
				if e = inner.Put(ctx, ns("improvement_policies"), policy.ID, prev, policy); e != nil {
					return e
				}
				if e = inner.Create(ctx, ns("formal_release_requests"), p.ReleaseRequestID, p.PlanID, StateOutput{Ref: scope.Ref(p.PlanID, 1), State: "permanently_occupied"}); e != nil {
					return api.E("idempotency_conflict", "formal_release_request_already_used")
				}
				if e = inner.Create(ctx, ns("formal_holdouts"), refKey(p.PartitionRef), p.PlanID, StateOutput{Ref: scope.Ref(p.PlanID, 1), State: "permanently_occupied"}); e != nil {
					return api.E("idempotency_conflict", "holdout_already_used")
				}
			}
			return inner.Create(ctx, ns("plans"), p.PlanID, p.SourceGroup, p)
		})
		if err != nil {
			var e *api.Error
			if !errors.As(err, &e) {
				return err
			}
			current.State = "rejected"
			current.Revision++
			if err = tx.Put(ctx, ns("plan_pending"), current.ID, rev+1, current); err != nil {
				return err
			}
			return runtime.Decide(ctx, tx, current.CommandID, nil, e)
		}
		current.State = "frozen"
		current.Revision++
		if err = tx.Put(ctx, ns("plan_pending"), current.ID, rev+1, current); err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, current.CommandID, StateOutput{Ref: scope.Ref(p.PlanID, 1), State: "frozen"}, nil)
	})
}
