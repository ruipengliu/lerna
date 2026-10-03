package governance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type referenceContent struct {
	files contentFiles
	scope runtime.Scope
}

func (c referenceContent) Read(ctx context.Context, r api.ContentRef, purpose string) ([]byte, error) {
	return c.files.Read(ctx, c.scope, runtime.Auth{}, r, purpose)
}
func (c referenceContent) Publish(_ context.Context, p adapter.Publication, b []byte) (api.ContentRef, error) {
	path := filepath.Join(c.files.root, p.ID)
	if old, e := os.ReadFile(path); e == nil {
		if api.Hash(old) != api.Hash(b) {
			return api.ContentRef{}, api.E("idempotency_conflict", "immutable_fixture_content")
		}
	} else if e := os.WriteFile(path, b, 0600); e != nil {
		return api.ContentRef{}, e
	}
	return api.ContentRef{TenantID: c.scope.TenantID, OwnerID: c.scope.OwnerID, ContentID: p.ID, Version: 1, Hash: api.Hash(b), ByteLength: uint64(len(b)), MediaType: p.MediaType}, nil
}

func TestActualReferenceAdapterRunsThroughFrozenPublicDomainAndSealsHonestReport(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	keys, e := platform.NewDevelopmentKey(f.scope.TenantID, f.scope.OwnerID, []string{"evaluation_prepare", "evaluation_start"})
	if e != nil {
		t.Fatal(e)
	}
	candidate, baseline := component("reference_v1"), component("reference_v0")
	runner, e := adapter.NewReferenceRunner(adapter.ReferenceRunnerConfig{Root: t.TempDir(), Scope: f.scope, Content: referenceContent{content, f.scope}, Clock: time.Now, Keys: keys, Implementations: []adapter.ReferenceImplementation{{Ref: candidate, Strategy: adapter.ReferenceReportV1}, {Ref: baseline, Strategy: adapter.ReferenceReportBodyV0}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := runner.Close(); e != nil {
			t.Error(e)
		}
	})
	f.svc.Ports.Runner = runner
	f.svc.Ports.Proof = localProof{keys}
	input := putContent(t, f, content, "input", []byte(`{"kind":"report","title":"Exact","body":"actual target bytes","save_path":"report.md"}`))
	truth := putContent(t, f, content, "truth", []byte("# Exact\n\nactual target bytes\n"))
	sample := governance.EvaluationSample{SampleID: api.NewID("sample"), InputRef: input, TruthRef: truth, Class: adapter.ReferenceClass}
	manifest := putContent(t, f, content, "manifest", api.Raw(struct {
		Samples []governance.EvaluationSample `json:"samples"`
	}{[]governance.EvaluationSample{sample}}))
	cutoff := time.Now().Add(8 * time.Second)
	plan := governance.EvaluationPlan{PlanID: api.NewID("plan"), Revision: 1, ManifestRef: manifest, CandidateRef: candidate, BaselineRef: baseline, PartitionRef: f.scope.Ref(api.NewID("partition"), 1), SourceGroup: api.NewID("source"), Seed: 3, Budget: []api.Amount{{Unit: "USD", Value: "0"}}, Thresholds: governance.EvaluationThresholds{MinimumTargetRate: "0.5", MinimumImprovement: "0.1", MaximumCostPerSample: "0", MaximumLatencyMillis: 10000, ConfidenceLevel: "0.95", StatisticalMethod: "paired_exact_binomial", MinimumSamples: 1, CostUnit: "USD"}, MaximumAttempts: 1, ObservationCutoff: api.Time(cutoff), UnknownOutcomePolicy: "fail_at_cutoff", CostUnknownPolicy: "bounded_worst_case", Purpose: "conformance"}
	freezePlan(t, f, plan)
	runID := api.NewID("run")
	_, receipt := command(t, f, "evaluation.run", runID, governance.RunRequest{PlanID: plan.PlanID, RunID: runID}, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("run: %+v", receipt)
	}
	drain(t, f, "governance.evaluation")
	rows := query[api.Page[governance.SampleRun]](t, f, "evaluation.samples", governance.SamplePageRequest{RunID: runID, Limit: 10})
	if len(rows.Items) != 2 {
		t.Fatalf("original denominator not complete: %+v", rows)
	}
	for _, row := range rows.Items {
		expected := "pass"
		if row.Arm == "baseline" {
			expected = "fail"
		}
		if row.Outcome != expected || row.Attempts != 1 || !row.UsageFinal || row.ModelClaimedComplete || len(row.OperationRefs) != 1 {
			t.Fatalf("actual original arm not recorded: %+v", row)
		}
	}
	if left := time.Until(cutoff) + 20*time.Millisecond; left > 0 {
		timer := time.NewTimer(left)
		defer timer.Stop()
		<-timer.C
	}
	_, receipt = command(t, f, "evaluation.seal", runID, governance.IDInput{ID: runID}, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("seal: %+v", receipt)
	}
	var report governance.EvaluationReport
	if e := api.Decode(receipt.Output, &report); e != nil {
		t.Fatal(e)
	}
	if report.FullDenominator != 1 || report.CandidateSuccess != 1 || report.BaselineSuccess != 0 || report.CandidateModelComplete != 0 || report.CandidateSystemAccepted != 1 || report.TimeoutUnknownNotRun != 0 || report.StatisticalGate != "fail" || report.ImprovementGate != "pass" || report.FormalEligible || len(report.CandidateCost) != 1 || report.CandidateCost[0].Unit != "USD" || report.CandidateCost[0].Value != "0" {
		t.Fatalf("small reference run claimed unsupported quality or fictional cost: %+v", report)
	}
}
