package governance_test

import (
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestUnavailableOrUnregisteredInstallationCannotCreatePrepareResponsibility(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	target := api.NewID("target")
	_, receipt := command(t, f, "extensions.target.register", target, governance.TargetRegister{TargetID: target, DataFormat: "v1"}, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("target: %+v", receipt)
	}
	install := governance.Installation{InstallLockRef: component("installation"), ConfigRef: component("config"), PlatformRef: component("platform"), Artifacts: []api.ContentRef{putContent(t, f, content, "artifact", []byte("accurate configured artifact"))}, DependencyRefs: []api.ComponentRef{}, ABI: "go-static-v1", Profile: api.Profile, ReadFormats: []string{"v1"}, WriteFormats: []string{"v1"}, TrustedBuiltin: true, IsolationRefs: []api.ContentRef{}}
	request := governance.PrepareRequest{Installation: install, TargetRef: f.scope.Ref(target, 1)}
	_, receipt = command(t, f, "extensions.prepare", target, request, nil)
	if receipt.Stage != "rejected" || receipt.Error.Code != "unsupported" {
		t.Fatalf("unconfigured lifecycle admitted new responsibility: %+v", receipt)
	}
	host, e := adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: t.TempDir(), Scope: f.scope, Content: referenceContent{content, f.scope}, Clock: time.Now, Installations: []governance.Installation{install}, ReadinessTTL: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	f.svc.Ports.Lifecycle = host
	request.Installation.ConfigRef.Digest = api.Hash([]byte("not the configured installation"))
	_, receipt = command(t, f, "extensions.prepare", target, request, nil)
	if receipt.Stage != "rejected" || receipt.Error.Code != "unsupported" {
		t.Fatalf("unregistered exact installation admitted new responsibility: %+v", receipt)
	}
	_, e = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		rows, e := tx.List(f.ctx, "governance/installations", "", "", 10)
		if e == nil && len(rows) != 0 {
			t.Fatalf("unsupported installations persisted: %+v", rows)
		}
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	jobs, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.prepare"}, 10, time.Minute)
	if e != nil || status != runtime.Committed || len(jobs) != 0 {
		t.Fatalf("unsupported preparation created work: %+v %s %v", jobs, status, e)
	}
}

func TestUnavailableEvaluationRunnerCannotCreateRunResponsibility(t *testing.T) {
	content := contentFiles{root: t.TempDir()}
	f := environment(t, governance.Options{Content: content})
	plan := evaluationPlan(t, f, content, 1, "conformance", time.Now().Add(time.Minute), nil)
	freezePlan(t, f, plan) // 纯计划管理仍可保存，运行必须具备实际端口。
	runID := api.NewID("run")
	_, receipt := command(t, f, "evaluation.run", runID, governance.RunRequest{PlanID: plan.PlanID, RunID: runID}, nil)
	if receipt.Stage != "rejected" || receipt.Error.Code != "unsupported" || receipt.Error.Reason != "evaluation_runner_unavailable" {
		t.Fatalf("unconfigured runner admitted new responsibility: %+v", receipt)
	}
	jobs, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.evaluation"}, 10, time.Minute)
	if e != nil || status != runtime.Committed || len(jobs) != 0 {
		t.Fatalf("unsupported run created work: %+v %s %v", jobs, status, e)
	}
}
