package governance_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 不可变内容夹具处于 Content 系统边界；真值字节与 candidate 输入分别存放。
type contentFiles struct{ root string }

func (c contentFiles) Read(_ context.Context, scope runtime.Scope, _ runtime.Auth, ref api.ContentRef, _ string) ([]byte, error) {
	if ref.TenantID != scope.TenantID {
		return nil, api.E("forbidden", "tenant_mismatch")
	}
	b, err := os.ReadFile(filepath.Join(c.root, ref.ContentID))
	if err != nil {
		return nil, err
	}
	if api.Hash(b) != ref.Hash {
		return nil, api.E("invalid_request", "content_hash_mismatch")
	}
	return b, nil
}
func putContent(t *testing.T, f *fixture, c contentFiles, name string, b []byte) api.ContentRef {
	t.Helper()
	r := ref(t, f, name)
	r.Hash = api.Hash(b)
	r.ByteLength = uint64(len(b))
	r.MediaType = "application/json"
	if err := os.WriteFile(filepath.Join(c.root, r.ContentID), b, 0600); err != nil {
		t.Fatal(err)
	}
	return r
}

// 文件保存每个物理 attempt 的原事实；重建 runner 后 Lookup 原键恢复。
// candidate 只读取 input；只读 judge 路径在该受信边界持有 truth。
type independentRunner struct {
	root    string
	content contentFiles
	scope   runtime.Scope
	proof   api.ContentRef
	mu      sync.Mutex
}

func (r *independentRunner) PreparePair(_ context.Context, p governance.RunnerPair) (governance.PairEvidence, error) {
	for _, key := range []string{p.CandidateEnvironmentKey, p.BaselineEnvironmentKey} {
		if err := os.MkdirAll(filepath.Join(r.root, api.Hash([]byte(key))[7:]), 0700); err != nil {
			return governance.PairEvidence{}, err
		}
	}
	return governance.PairEvidence{CandidatePrepared: true, BaselinePrepared: true, CandidateEnvironmentRef: r.scope.Ref(api.NewID("environment"), 1), BaselineEnvironmentRef: r.scope.Ref(api.NewID("environment"), 1), ProofRef: r.proof}, nil
}
func (r *independentRunner) Lookup(_ context.Context, a governance.RunnerAttempt) (governance.AttemptObservation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(r.root, a.AttemptID))
	if os.IsNotExist(err) {
		return governance.AttemptObservation{}, false, nil
	}
	if err != nil {
		return governance.AttemptObservation{}, false, err
	}
	var out governance.AttemptObservation
	err = api.Decode(b, &out)
	return out, err == nil, err
}
func (r *independentRunner) Run(ctx context.Context, a governance.RunnerAttempt) (governance.AttemptObservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	input, err := r.content.Read(ctx, r.scope, runtime.Auth{}, a.InputRef, "evaluation_input")
	if err != nil {
		return governance.AttemptObservation{}, err
	}
	truth, err := r.content.Read(ctx, r.scope, runtime.Auth{}, a.TruthRef, "independent_truth")
	if err != nil {
		return governance.AttemptObservation{}, err
	}
	output := input
	if a.Arm == "baseline" {
		output = []byte("incorrect")
	}
	outcome := "fail"
	if string(output) == string(truth) {
		outcome = "pass"
	}
	out := governance.AttemptObservation{Outcome: outcome, TruthRef: a.TruthRef, Usage: []api.Amount{{Unit: "USD", Value: "0.1"}}, UsageFinal: true, CostUpperBound: []api.Amount{}, LatencyMillis: 10, ModelClaimedComplete: true, SystemAccepted: outcome == "pass", OperationRef: r.scope.Ref(api.NewID("operation"), 1), ProofRef: r.proof, ObservedAt: api.Time(time.Now())}
	file, err := os.OpenFile(filepath.Join(r.root, a.AttemptID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, err
	}
	_, writeErr := file.Write(api.Raw(out))
	closeErr := file.Close()
	if writeErr != nil {
		return out, writeErr
	}
	return out, closeErr
}
func (r *independentRunner) Seal(_ context.Context, p governance.RunnerPair) (governance.PairStopEvidence, error) {
	for _, key := range []string{p.CandidateEnvironmentKey, p.BaselineEnvironmentKey} {
		if err := os.RemoveAll(filepath.Join(r.root, api.Hash([]byte(key))[7:])); err != nil {
			return governance.PairStopEvidence{}, err
		}
	}
	return governance.PairStopEvidence{CandidateStopped: true, BaselineStopped: true, CandidateDestroyed: true, BaselineDestroyed: true, ProofRef: r.proof}, nil
}

func evaluationPlan(t *testing.T, f *fixture, c contentFiles, n int, purpose string, cutoff time.Time, policy *api.ObjectRef) governance.EvaluationPlan {
	t.Helper()
	samples := make([]governance.EvaluationSample, 0, n)
	for i := 0; i < n; i++ {
		input := putContent(t, f, c, "input", []byte("expected"))
		truth := putContent(t, f, c, "truth", []byte("expected"))
		samples = append(samples, governance.EvaluationSample{SampleID: api.NewID("sample"), InputRef: input, TruthRef: truth, Class: "document"})
	}
	b, err := json.Marshal(struct {
		Samples []governance.EvaluationSample `json:"samples"`
	}{samples})
	if err != nil {
		t.Fatal(err)
	}
	manifest := putContent(t, f, c, "manifest", b)
	return governance.EvaluationPlan{PlanID: api.NewID("plan"), Revision: 1, ManifestRef: manifest, CandidateRef: component("candidate"), BaselineRef: component("baseline"), PartitionRef: f.scope.Ref(api.NewID("partition"), 1), SourceGroup: api.NewID("source_group"), Seed: 17, Budget: []api.Amount{{Unit: "USD", Value: "1000"}}, Thresholds: governance.EvaluationThresholds{MinimumTargetRate: "0.5", MinimumImprovement: "0.1", MaximumCostPerSample: "1", MaximumLatencyMillis: 100, ConfidenceLevel: "0.95", StatisticalMethod: "paired_exact_binomial", MinimumSamples: 5, CostUnit: "USD"}, MaximumAttempts: 1, ObservationCutoff: api.Time(cutoff), UnknownOutcomePolicy: "fail_at_cutoff", CostUnknownPolicy: "bounded_worst_case", ReleaseRequestID: api.NewID("release"), ImprovementPolicyRef: policy, Purpose: purpose}
}
func freezePlan(t *testing.T, f *fixture, plan governance.EvaluationPlan) api.Command {
	t.Helper()
	if plan.Purpose == "formal" {
		registerFormalPlanFixture(t, f, plan)
	}
	c, r := command(t, f, "evaluation.plan_create", plan.PlanID, governance.PlanCreate{Plan: plan}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("plan: %+v", r)
	}
	drain(t, f, "governance.plan")
	original, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || original.Stage != "applied" {
		t.Fatalf("freeze: %+v %v", original, err)
	}
	return c
}

func registerFormalPlanFixture(t *testing.T, f *fixture, plan governance.EvaluationPlan) {
	t.Helper()
	// 数据管理边界夹具只由测试预置。公共 evaluation 输入无法登记它。
	f.svc.Ports.FormalPlanGate = registeredFormalPlan{}
	_, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		return tx.Create(f.ctx, "governance/fixture_formal_registry", plan.PartitionRef.ObjectID, "", plan)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type registeredFormalPlan struct{}

func formalBindings(p governance.EvaluationPlan) governance.EvaluationPlan {
	p.PlanID = ""
	p.ReleaseRequestID = ""
	p.ManifestDigest = p.ManifestRef.Hash
	p.Frozen = false
	p.FullDenominator = 0
	p.FormalAttemptIndex = 0
	p.FormalEligible = false
	p.ExposureRevision = 0
	return p
}
func (registeredFormalPlan) CheckTx(ctx context.Context, tx runtime.Tx, p governance.EvaluationPlan) error {
	var registered governance.EvaluationPlan
	if _, err := tx.Get(ctx, "governance/fixture_formal_registry", p.PartitionRef.ObjectID, &registered); err != nil {
		return err
	}
	if !api.Equal(formalBindings(p), formalBindings(registered)) {
		return api.E("forbidden", "formal_registry_binding_mismatch")
	}
	return nil
}

func TestFormalPlanWithoutIndependentRegistryCannotCreateFormalResponsibility(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	policy := f.scope.Ref(api.NewID("policy"), 1)
	plan := evaluationPlan(t, f, content, 1, "formal", time.Now().Add(time.Minute), &policy)
	_, r := command(t, f, "evaluation.plan_create", plan.PlanID, governance.PlanCreate{Plan: plan}, nil)
	if r.Stage != "rejected" || r.Error.Code != "unsupported" || r.Error.Reason != "registered_formal_lineage_partition_unavailable" {
		t.Fatalf("unregistered formal plan: %+v", r)
	}
	jobs, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.plan"}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(jobs) != 0 {
		t.Fatalf("unsupported plan created work: %+v %s %v", jobs, status, err)
	}
}
func TestFormalAttemptAndHoldoutRemainOccupiedAfterCancellation(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	policy := governance.ImprovementPolicy{ID: api.NewID("policy"), Revision: 1, PolicyRef: component("policy"), LineageID: api.NewID("lineage"), FormalAttemptLimit: 1}
	_, r := command(t, f, "evaluation.improvement_policy.create", policy.ID, policy, nil)
	if r.Stage != "applied" {
		t.Fatalf("policy: %+v", r)
	}
	pr := f.scope.Ref(policy.ID, 1)
	plan := evaluationPlan(t, f, content, 1, "formal", time.Now().Add(time.Minute), &pr)
	freezePlan(t, f, plan)
	runID := api.NewID("run")
	_, r = command(t, f, "evaluation.run", runID, governance.RunRequest{PlanID: plan.PlanID, RunID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("run: %+v", r)
	}
	_, r = command(t, f, "evaluation.cancel", runID, governance.IDInput{ID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("cancel: %+v", r)
	}
	drain(t, f, "governance.eval_cancel")
	next := evaluationPlan(t, f, content, 1, "formal", time.Now().Add(time.Minute), &pr)
	registerFormalPlanFixture(t, f, next)
	c, pending := command(t, f, "evaluation.plan_create", next.PlanID, governance.PlanCreate{Plan: next}, nil)
	if pending.Stage != "accepted" {
		t.Fatalf("second plan: %+v", pending)
	}
	drain(t, f, "governance.plan")
	rejected, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || rejected.Stage != "rejected" || rejected.Error.Reason != "formal_attempt_exhausted" {
		t.Fatalf("formal attempt returned after cancellation: %+v %v", rejected, err)
	}
}
func TestThousandSampleCancellationKeepsFullDenominatorAndNotRunOutcomes(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	plan := evaluationPlan(t, f, content, 1001, "conformance", time.Now().Add(time.Minute), nil)
	freezePlan(t, f, plan)
	runID := api.NewID("run")
	_, r := command(t, f, "evaluation.run", runID, governance.RunRequest{PlanID: plan.PlanID, RunID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("run: %+v", r)
	}
	_, r = command(t, f, "evaluation.cancel", runID, governance.IDInput{ID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("cancel: %+v", r)
	}
	drain(t, f, "governance.eval_cancel")
	cursor := ""
	count := 0
	for {
		page := query[api.Page[governance.SampleRun]](t, f, "evaluation.samples", governance.SamplePageRequest{RunID: runID, Cursor: cursor, Limit: 100})
		for _, sample := range page.Items {
			if sample.Outcome != "not_run" || sample.Attempts != 0 || !sample.StopConfirmed {
				t.Fatalf("cancelled unstarted sample: %+v", sample)
			}
		}
		count += len(page.Items)
		if page.Exhausted {
			break
		}
		cursor = page.NextCursor
	}
	if count != 2002 {
		t.Fatalf("denominator lost: %d", count)
	}
	view := query[governance.EvaluationRead](t, f, "evaluation.read", governance.IDInput{ID: runID})
	if view.Plan.FullDenominator != 1001 || view.Run.Samples.TotalCount != 2002 || !view.Run.Samples.Complete || view.Run.GateOpen {
		t.Fatalf("run summary: %+v", view)
	}
}

func TestRealPairedRunSealsOriginalStatisticsAndLateFailureInvalidatesQualification(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	runner := &independentRunner{root: t.TempDir(), content: content}
	f := environment(t, governance.Options{Content: content, Runner: runner})
	runner.scope = f.scope
	runner.proof = ref(t, f, "runner_proof")
	policy := governance.ImprovementPolicy{ID: api.NewID("policy"), Revision: 1, PolicyRef: component("policy"), LineageID: api.NewID("lineage"), FormalAttemptLimit: 2}
	_, r := command(t, f, "evaluation.improvement_policy.create", policy.ID, policy, nil)
	if r.Stage != "applied" {
		t.Fatalf("policy: %+v", r)
	}
	pr := f.scope.Ref(policy.ID, 1)
	cutoff := time.Now().Add(12 * time.Second)
	plan := evaluationPlan(t, f, content, 12, "formal", cutoff, &pr)
	plan.Thresholds.MinimumImprovement = "0.95"
	freezePlan(t, f, plan)
	runID := api.NewID("run")
	_, r = command(t, f, "evaluation.run", runID, governance.RunRequest{PlanID: plan.PlanID, RunID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("run: %+v", r)
	}
	drain(t, f, "governance.evaluation")
	if left := time.Until(cutoff) + 30*time.Millisecond; left > 0 {
		time.Sleep(left)
	}
	_, r = command(t, f, "evaluation.seal", runID, governance.IDInput{ID: runID}, nil)
	if r.Stage != "applied" {
		t.Fatalf("seal: %+v", r)
	}
	var report governance.EvaluationReport
	if err := api.Decode(r.Output, &report); err != nil {
		t.Fatal(err)
	}
	if report.FullDenominator != 12 || report.CandidateSuccess != 12 || report.BaselineSuccess != 0 || report.TargetAttainment != "pass" || report.StatisticalGate != "pass" || report.ImprovementGate != "pass" || report.CandidateCost[0].Value != "1.2" {
		t.Fatalf("independent paired report: %+v", report)
	}
	samples := query[api.Page[governance.SampleRun]](t, f, "evaluation.samples", governance.SamplePageRequest{RunID: runID, Limit: 100})
	if len(samples.Items) != 24 {
		t.Fatalf("sample-arm denominator: %+v", samples)
	}
	var candidate governance.SampleRun
	for _, sample := range samples.Items {
		if sample.Attempts != 1 {
			t.Fatalf("unexecuted sample: %+v", sample)
		}
		if sample.Arm == "candidate" {
			candidate = sample
			if sample.Outcome != "pass" {
				t.Fatalf("candidate: %+v", sample)
			}
		} else if sample.Outcome != "fail" {
			t.Fatalf("baseline: %+v", sample)
		}
	}
	_, opened := command(t, f, "evaluation.feedback_open", report.ReportID, governance.FeedbackOpen{ReportRef: f.scope.Ref(report.ReportID, 1), ExposureID: api.NewID("exposure")}, nil)
	if opened.Stage != "applied" {
		t.Fatalf("feedback: %+v", opened)
	}
	drain(t, f, "governance.exposure")
	view := query[governance.EvaluationRead](t, f, "evaluation.read", governance.IDInput{ID: runID})
	if !view.Qualification.Eligible {
		t.Fatalf("normal sealed feedback invalidated own report: %+v", view)
	}
	f.auth.Roles = append(f.auth.Roles, "evaluation_reporter")
	late := governance.LateObservation{SampleRunRef: f.scope.Ref(candidate.SampleRunID, candidate.Revision), SourceRevision: 1, SourceDigest: api.Hash([]byte("independent corrected target readback")), Observation: governance.AttemptObservation{Outcome: "fail", TruthRef: *candidate.TruthRef, Usage: candidate.Usage, UsageFinal: true, CostUpperBound: []api.Amount{}, LatencyMillis: 10, ModelClaimedComplete: true, SystemAccepted: true, OperationRef: candidate.OperationRefs[0], ProofRef: runner.proof, ObservedAt: candidate.ObservedAt}}
	_, merged := command(t, f, "evaluation.observation.merge", candidate.SampleRunID, late, nil)
	if merged.Stage != "applied" {
		t.Fatalf("late merge: %+v", merged)
	}
	view = query[governance.EvaluationRead](t, f, "evaluation.read", governance.IDInput{ID: runID})
	if view.Qualification.Eligible || view.Qualification.Reason != "late_fact_invalidated_original_conclusion" || !api.Equal(view.Report, &report) {
		t.Fatalf("late fact rewrote report or kept obsolete eligibility: %+v", view)
	}
}
