package governance_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestReferencePairExecutesSeparateFilesAndLooksUpOriginalAttempt(t *testing.T) {
	ctx := context.Background()
	scope, c, install := fixture(t)
	keys, e := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"evaluation_prepare", "evaluation_start"})
	if e != nil {
		t.Fatal(e)
	}
	input, e := c.Publish(ctx, adapter.Publication{ID: api.NewID("input"), MediaType: "application/json"}, []byte(`{"kind":"report","title":"真实报告","body":"原冻结正文","save_path":"report.md"}`))
	if e != nil {
		t.Fatal(e)
	}
	truth, e := c.Publish(ctx, adapter.Publication{ID: api.NewID("truth"), MediaType: "text/markdown"}, []byte("# 真实报告\n\n原冻结正文\n"))
	if e != nil {
		t.Fatal(e)
	}
	base := install.InstallLockRef
	base.ComponentID = api.NewID("baseline")
	base.Digest = api.Hash([]byte("body-v0"))
	root := t.TempDir()
	cfg := adapter.ReferenceRunnerConfig{Root: root, Scope: scope, Content: c, Clock: time.Now, Keys: keys, Implementations: []adapter.ReferenceImplementation{{Ref: install.InstallLockRef, Strategy: adapter.ReferenceReportV1}, {Ref: base, Strategy: adapter.ReferenceReportBodyV0}}}
	runner, e := adapter.NewReferenceRunner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer runner.Close()
	plan := domain.EvaluationPlan{PlanID: api.NewID("plan"), CandidateRef: install.InstallLockRef, BaselineRef: base, Seed: 42, Budget: []api.Amount{{Unit: "USD", Value: "0"}}, Purpose: "conformance", Frozen: true, ObservationCutoff: api.Time(time.Now().Add(time.Minute))}
	sample := domain.EvaluationSample{SampleID: api.NewID("sample"), InputRef: input, TruthRef: truth, Class: adapter.ReferenceClass}
	pair := domain.RunnerPair{RunID: api.NewID("run"), Plan: plan, Sample: sample, StartBefore: plan.ObservationCutoff}
	pair.CandidateEnvironmentKey = pair.RunID + "/" + sample.SampleID + "/candidate"
	pair.BaselineEnvironmentKey = pair.RunID + "/" + sample.SampleID + "/baseline"
	digest, e := api.Digest(pair)
	if e != nil {
		t.Fatal(e)
	}
	pair.Permit, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: scope.OwnerID, Purpose: "evaluation_prepare", ObjectRef: domain.EvaluationSampleRunRef(scope, plan.PlanID, sample.SampleID, "candidate"), WindowID: domain.EvaluationSampleRunRef(scope, plan.PlanID, sample.SampleID, "candidate").ObjectID, Digest: digest, IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: pair.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := runner.PreparePair(ctx, pair)
	if e != nil || !prepared.CandidatePrepared || !prepared.BaselinePrepared || api.Equal(prepared.CandidateEnvironmentRef, prepared.BaselineEnvironmentRef) {
		t.Fatalf("separate actual environments: %+v %v", prepared, e)
	}
	var observations []domain.AttemptObservation
	for _, arm := range []string{"candidate", "baseline"} {
		implementation := plan.CandidateRef
		if arm == "baseline" {
			implementation = base
		}
		attempt := domain.RunnerAttempt{RunID: pair.RunID, PlanID: plan.PlanID, SampleID: sample.SampleID, Arm: arm, AttemptID: api.NewID("attempt"), EnvironmentKey: pair.RunID + "/" + sample.SampleID + "/" + arm, ImplementationRef: implementation, InputRef: input, TruthRef: truth, StartBefore: pair.StartBefore, Seed: 42, Budget: plan.Budget}
		digest, e := api.Digest(attempt)
		if e != nil {
			t.Fatal(e)
		}
		attempt.Permit, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: scope.OwnerID, Purpose: "evaluation_start", ObjectRef: scope.Ref(attempt.AttemptID, 1), WindowID: attempt.AttemptID, Digest: digest, IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: attempt.StartBefore})
		if e != nil {
			t.Fatal(e)
		}
		observation, e := runner.Run(ctx, attempt)
		if e != nil {
			t.Fatal(e)
		}
		observations = append(observations, observation)
		saved, e := runner.Run(ctx, attempt)
		if e != nil || !api.Equal(saved, observation) {
			t.Fatalf("duplicate physically reran original: %v", e)
		}
		original, known, e := runner.Lookup(ctx, attempt)
		if e != nil || !known || !api.Equal(original, observation) {
			t.Fatalf("lookup lost original: %v", e)
		}
		modified := attempt
		modified.Seed++
		if _, _, e = runner.Lookup(ctx, modified); e == nil {
			t.Fatal("altered original input became a new attempt")
		}
	}
	if observations[0].Outcome != "pass" || observations[1].Outcome != "fail" || !observations[0].SystemAccepted || observations[1].SystemAccepted {
		t.Fatalf("independent target truth: %+v", observations)
	}
	// 平台验收直接读真实目标；expected 为冻结的独立字面值。
	var reports []string
	e = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == "report.md" {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			reports = append(reports, string(b))
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(reports) != 2 {
		t.Fatalf("actual independent files missing: %v", reports)
	}
	joined := strings.Join(reports, "|")
	if !strings.Contains(joined, "# 真实报告\n\n原冻结正文\n") || !strings.Contains(joined, "原冻结正文") {
		t.Fatalf("physical reports differ: %q", reports)
	}
	stopped, e := runner.Seal(ctx, pair)
	if e != nil || !stopped.CandidateStopped || !stopped.BaselineStopped || !stopped.CandidateDestroyed || !stopped.BaselineDestroyed || stopped.MayApplyLater {
		t.Fatalf("actual sealing and destruction: %+v %v", stopped, e)
	}
	if _, e = runner.Run(ctx, domain.RunnerAttempt{}); e == nil {
		t.Fatal("closed environment accepted new run")
	}
	restarted, e := adapter.NewReferenceRunner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if _, e = restarted.PreparePair(ctx, pair); e == nil {
		t.Fatal("seal resurrected exact original environments")
	}
}

func referenceSetup(t *testing.T, after func(context.Context, domain.RunnerAttempt) error) (*adapter.ReferenceRunner, adapter.ReferenceRunnerConfig, domain.RunnerPair, domain.RunnerAttempt) {
	t.Helper()
	ctx := context.Background()
	scope, c, install := fixture(t)
	keys, e := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"evaluation_prepare", "evaluation_start"})
	if e != nil {
		t.Fatal(e)
	}
	input, e := c.Publish(ctx, adapter.Publication{ID: api.NewID("input"), MediaType: "application/json"}, []byte(`{"kind":"report","title":"Frozen","body":"source bytes","save_path":"report.md"}`))
	if e != nil {
		t.Fatal(e)
	}
	truth, e := c.Publish(ctx, adapter.Publication{ID: api.NewID("truth"), MediaType: "text/markdown"}, []byte("# Frozen\n\nsource bytes\n"))
	if e != nil {
		t.Fatal(e)
	}
	cfg := adapter.ReferenceRunnerConfig{Root: t.TempDir(), Scope: scope, Content: c, Clock: time.Now, Keys: keys, Implementations: []adapter.ReferenceImplementation{{Ref: install.InstallLockRef, Strategy: adapter.ReferenceReportV1}}, AfterTargetWrite: after}
	runner, e := adapter.NewReferenceRunner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := runner.Close(); e != nil {
			t.Error(e)
		}
	})
	plan := domain.EvaluationPlan{PlanID: api.NewID("plan"), CandidateRef: install.InstallLockRef, BaselineRef: install.InstallLockRef, Seed: 8, Budget: []api.Amount{{Unit: "USD", Value: "0"}}, Purpose: "conformance", Frozen: true, FullDenominator: 1, ObservationCutoff: api.Time(time.Now().Add(time.Minute))}
	sample := domain.EvaluationSample{SampleID: api.NewID("sample"), InputRef: input, TruthRef: truth, Class: adapter.ReferenceClass}
	pair := domain.RunnerPair{RunID: api.NewID("run"), Plan: plan, Sample: sample, StartBefore: plan.ObservationCutoff}
	pair.CandidateEnvironmentKey = pair.RunID + "/" + sample.SampleID + "/candidate"
	pair.BaselineEnvironmentKey = pair.RunID + "/" + sample.SampleID + "/baseline"
	digest, e := api.Digest(pair)
	if e != nil {
		t.Fatal(e)
	}
	pair.Permit, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: scope.OwnerID, Purpose: "evaluation_prepare", ObjectRef: domain.EvaluationSampleRunRef(scope, plan.PlanID, sample.SampleID, "candidate"), WindowID: domain.EvaluationSampleRunRef(scope, plan.PlanID, sample.SampleID, "candidate").ObjectID, Digest: digest, IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: pair.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = runner.PreparePair(ctx, pair); e != nil {
		t.Fatal(e)
	}
	attempt := domain.RunnerAttempt{RunID: pair.RunID, PlanID: plan.PlanID, SampleID: sample.SampleID, Arm: "candidate", AttemptID: api.NewID("attempt"), EnvironmentKey: pair.CandidateEnvironmentKey, ImplementationRef: plan.CandidateRef, InputRef: input, TruthRef: truth, StartBefore: pair.StartBefore, Seed: 8, Budget: plan.Budget}
	digest, e = api.Digest(attempt)
	if e != nil {
		t.Fatal(e)
	}
	attempt.Permit, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: scope.OwnerID, Purpose: "evaluation_start", ObjectRef: scope.Ref(attempt.AttemptID, 1), WindowID: attempt.AttemptID, Digest: digest, IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: attempt.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	return runner, cfg, pair, attempt
}

func TestReferenceSealCancelsAndWaitsOriginalWriteWithoutInventingOutcome(t *testing.T) {
	entered := make(chan struct{})
	runner, cfg, pair, attempt := referenceSetup(t, func(ctx context.Context, _ domain.RunnerAttempt) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, e := runner.Run(ctx, attempt); completed <- e }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual target write did not enter")
	}
	stop, e := runner.Seal(ctx, pair)
	if e != nil || !stop.CandidateStopped || !stop.CandidateDestroyed || stop.MayApplyLater {
		t.Fatalf("original in-flight write not actually cancelled and joined: %+v %v", stop, e)
	}
	if e = <-completed; !errors.Is(e, context.Canceled) {
		t.Fatalf("original writer was not cancelled: %v", e)
	}
	if _, known, e := runner.Lookup(context.Background(), attempt); e != nil || known {
		t.Fatalf("stop invented a successful original observation: %v %v", known, e)
	}
	cfg.AfterTargetWrite = nil
	recovered, e := adapter.NewReferenceRunner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	if _, e = recovered.Run(context.Background(), attempt); e == nil {
		t.Fatal("unknown original attempt was physically repeated after restart")
	}
	if _, known, e := recovered.Lookup(context.Background(), attempt); e != nil || known {
		t.Fatalf("unknown original journal lost: %v %v", known, e)
	}
}

func TestReferencePreparedCopyCannotBypassCurrentContentSource(t *testing.T) {
	runner, cfg, _, attempt := referenceSetup(t, nil)
	content := cfg.Content.(*fileContent)
	if e := os.Remove(filepath.Join(content.root, attempt.TruthRef.ContentID)); e != nil {
		t.Fatal(e)
	}
	if _, e := runner.Run(context.Background(), attempt); e == nil {
		t.Fatal("prepared private truth copy bypassed current Content authority")
	}
	var files []string
	if e := filepath.WalkDir(cfg.Root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.Name() == "report.md" {
			files = append(files, path)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if len(files) != 0 {
		t.Fatal("target started after current source failed")
	}
}

func TestReferenceEntryRejectsChangedInputExpiredWindowAndWrongRegisteredKey(t *testing.T) {
	runner, cfg, pair, attempt := referenceSetup(t, nil)
	ctx := context.Background()
	missing := pair
	missing.Permit = ""
	if _, e := runner.PreparePair(ctx, missing); e == nil {
		t.Fatal("prepare without original signed admission")
	}
	changed := attempt
	changed.Seed++
	if _, e := runner.Run(ctx, changed); e == nil {
		t.Fatal("changed input with original permit entered target")
	}
	wrong, e := platform.NewDevelopmentKey(cfg.Scope.TenantID, cfg.Scope.OwnerID, []string{"evaluation_prepare", "evaluation_start"})
	if e != nil {
		t.Fatal(e)
	}
	wrongCfg := cfg
	wrongCfg.Keys = wrong
	untrusted, e := adapter.NewReferenceRunner(wrongCfg)
	if e != nil {
		t.Fatal(e)
	}
	defer untrusted.Close()
	if _, e = untrusted.Run(ctx, attempt); e == nil {
		t.Fatal("unregistered signing key entered target")
	}
	futureCfg := cfg
	futureCfg.Clock = func() time.Time { return time.Now().Add(2 * time.Minute) }
	expired, e := adapter.NewReferenceRunner(futureCfg)
	if e != nil {
		t.Fatal(e)
	}
	defer expired.Close()
	if _, e = expired.Run(ctx, attempt); e == nil {
		t.Fatal("expired original start window entered target")
	}
	original, e := runner.Run(ctx, attempt)
	if e != nil {
		t.Fatal(e)
	}
	if saved, known, e := expired.Lookup(ctx, attempt); e != nil || !known || !api.Equal(saved, original) {
		t.Fatalf("expiry lost original result query: %v %v", known, e)
	}
}

func TestReferenceProcessRecoveryHelper(t *testing.T) {
	if os.Getenv("HARNESS_REFERENCE_RECOVERY_HELPER") != "1" {
		return
	}
	var scope runtime.Scope
	var pair domain.RunnerPair
	var attempt domain.RunnerAttempt
	var implementations []adapter.ReferenceImplementation
	for _, part := range []struct {
		key   string
		value any
	}{{"HARNESS_REFERENCE_SCOPE", &scope}, {"HARNESS_REFERENCE_PAIR", &pair}, {"HARNESS_REFERENCE_ATTEMPT", &attempt}, {"HARNESS_REFERENCE_IMPLEMENTATIONS", &implementations}} {
		if e := api.Decode([]byte(os.Getenv(part.key)), part.value); e != nil {
			t.Fatal(e)
		}
	}
	var jwk struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if e := api.Decode([]byte(os.Getenv("HARNESS_REFERENCE_PUBLIC_KEY")), &jwk); e != nil {
		t.Fatal(e)
	}
	public, e := platform.ParsePublicKey(jwk.X, jwk.Y)
	if e != nil {
		t.Fatal(e)
	}
	keys := &platform.Keyring{Keys: map[string]platform.RegisteredKey{"development-es256": {TenantID: scope.TenantID, Issuer: scope.OwnerID, Purposes: []string{"evaluation_prepare", "evaluation_start"}, Public: public}}}
	content := &fileContent{root: os.Getenv("HARNESS_REFERENCE_CONTENT"), scope: scope}
	runner, e := adapter.NewReferenceRunner(adapter.ReferenceRunnerConfig{Root: os.Getenv("HARNESS_REFERENCE_ROOT"), Scope: scope, Content: content, Clock: time.Now, Keys: keys, Implementations: implementations, AfterTargetWrite: func(context.Context, domain.RunnerAttempt) error { fmt.Println("TARGET_WRITTEN"); select {} }})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = runner.PreparePair(context.Background(), pair); e != nil {
		t.Fatal(e)
	}
	if _, e = runner.Run(context.Background(), attempt); e != nil {
		t.Fatal(e)
	}
}

func TestReferenceKilledWriterRetainsOriginalUnknownAndNeverWritesTwice(t *testing.T) {
	_, cfg, pair, attempt := referenceSetup(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReferenceProcessRecoveryHelper$")
	cmd.Env = append(os.Environ(), "HARNESS_REFERENCE_RECOVERY_HELPER=1", "HARNESS_REFERENCE_SCOPE="+string(api.Raw(cfg.Scope)), "HARNESS_REFERENCE_PAIR="+string(api.Raw(pair)), "HARNESS_REFERENCE_ATTEMPT="+string(api.Raw(attempt)), "HARNESS_REFERENCE_IMPLEMENTATIONS="+string(api.Raw(cfg.Implementations)), "HARNESS_REFERENCE_ROOT="+cfg.Root, "HARNESS_REFERENCE_CONTENT="+cfg.Content.(*fileContent).root, "HARNESS_REFERENCE_PUBLIC_KEY="+string(platform.PublicJWK(cfg.Keys.Keys["development-es256"].Public)))
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	line, e := bufio.NewReader(pipe).ReadString('\n')
	if e != nil || line != "TARGET_WRITTEN\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("physical file write did not enter: %q %v", line, e)
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Wait(); e == nil {
		t.Fatal("writer did not actually crash")
	}
	recovered, e := adapter.NewReferenceRunner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	if _, known, e := recovered.Lookup(ctx, attempt); e != nil || known {
		t.Fatalf("crash invented target outcome: %v %v", known, e)
	}
	var target string
	if e := filepath.WalkDir(cfg.Root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.Name() == "report.md" {
			target = path
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	actual, e := os.ReadFile(target)
	if e != nil || string(actual) != "# Frozen\n\nsource bytes\n" {
		t.Fatalf("actual original effect absent: %q %v", actual, e)
	}
	before, e := os.Stat(target)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = recovered.Run(ctx, attempt); e == nil {
		t.Fatal("unknown original writer physically retried")
	}
	after, e := os.Stat(target)
	if e != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("duplicate lookup changed original target: %v", e)
	}
	stop, e := recovered.Seal(ctx, pair)
	if e != nil || !stop.CandidateStopped || !stop.CandidateDestroyed || stop.MayApplyLater {
		t.Fatalf("crash scope not actually destroyed: %+v %v", stop, e)
	}
	if _, known, e := recovered.Lookup(ctx, attempt); e != nil || known {
		t.Fatalf("cleanup overwrote original unknown: %v %v", known, e)
	}
}
