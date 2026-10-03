package development

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func configuredInstallation(artifact api.ContentRef) governance.Installation {
	return governance.Installation{InstallLockRef: component("reference-host-installation"), ConfigRef: component("reference-host-config"), PlatformRef: component("reference-host-platform"), Artifacts: []api.ContentRef{artifact}, DependencyRefs: []api.ComponentRef{}, ABI: "go-static-v1", Profile: api.Profile, ReadFormats: []string{"v1"}, WriteFormats: []string{"v1"}, TrustedBuiltin: true, IsolationRefs: []api.ContentRef{}}
}

func TestBuiltinGovernanceNonWorkerOnlyValidatesSameAllowlist(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner")}
	keys, e := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"evaluation_prepare", "evaluation_start"})
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	a := &App{Scope: scope, Config: Config{DataRoot: root}, Keys: keys}
	artifact := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte("declared artifact")), ByteLength: 17, MediaType: "application/octet-stream"}
	install := configuredInstallation(artifact)
	cfg := &BuiltinGovernanceConfig{Installations: []governance.Installation{install}, Implementations: []adapter.ReferenceImplementation{{Ref: install.InstallLockRef, Strategy: adapter.ReferenceReportV1}}, ReadinessTTLSeconds: 30}
	lifecycle, runner, closeAll, e := configureBuiltinGovernance(a, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer closeAll()
	admission := lifecycle.(governance.InstallationAdmission)
	if e = admission.CheckInstallation(install); e != nil {
		t.Fatal(e)
	}
	changed := install
	changed.ConfigRef.Digest = api.Hash([]byte("changed"))
	if e = admission.CheckInstallation(changed); !api.IsCode(e, "unsupported") {
		t.Fatalf("nonworker accepted a different installation: %v", e)
	}
	if _, e = lifecycle.Prepare(context.Background(), install); !api.IsCode(e, "unsupported") {
		t.Fatalf("nonworker acquired lifecycle IO: %v", e)
	}
	if _, e = runner.PreparePair(context.Background(), governance.RunnerPair{}); !api.IsCode(e, "unsupported") {
		t.Fatalf("nonworker acquired evaluation IO: %v", e)
	}
	if _, e = os.Stat(filepath.Join(root, "governance")); !os.IsNotExist(e) {
		t.Fatalf("nonworker created physical governance storage: %v", e)
	}
	cfg.Implementations[0].Strategy = "unregistered-native"
	if _, _, _, e = configureBuiltinGovernance(a, cfg); !api.IsCode(e, "unsupported") {
		t.Fatalf("unknown implementation strategy passed configuration: %v", e)
	}
	lifecycle, runner, closeAll, e = configureBuiltinGovernance(a, nil)
	if e != nil || lifecycle != nil || runner != nil || closeAll() != nil {
		t.Fatalf("unconfigured capability was opened: %v", e)
	}
}

func TestBuiltinGovernanceWorkerBindsCurrentMemoryAndExclusiveOwnership(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" {
				if os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
					t.Skip("requires actual HARNESS_TEST_POSTGRES_DSN")
				}
				t.Setenv("HARNESS_DATABASE_DSN", os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
			}
			runBuiltinGovernanceWorker(t, driver)
		})
	}
}

func runBuiltinGovernanceWorker(t *testing.T, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	c, e := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if e != nil {
		t.Fatal(e)
	}
	c.TenantID, c.OwnerID, c.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	a, e := OpenApp(ctx, c, true)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	// 明确预置本宿主的精确用途，不借旧政策扩大范围。
	for _, purpose := range RequiredContentPurposes() {
		if !slices.Contains(a.ContentPolicy.Values.Purposes, purpose) {
			a.ContentPolicy.Values.Purposes = append(a.ContentPolicy.Values.Purposes, purpose)
		}
	}
	a.ContentPolicy.PolicyRef.ComponentID = api.NewID("policy")
	a.ContentPolicy.PolicyRef.Digest, e = api.Digest(a.ContentPolicy.Values)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Memory.InstallPolicy(ctx, a.Scope, a.ServiceAuth, a.ContentPolicy); e != nil {
		t.Fatal(e)
	}
	// 准确 manifest 字节只是显式测试制品，不声称等于 Go 二进制构建摘要。
	artifact, e := a.Publish(ctx, a.Scope, a.ServiceAuth, api.NewID("content"), "application/json", []byte(`{"builtin":"reference-private-file-health","version":1}`), []api.ContentRef{}, []api.ContentRef{})
	if e != nil {
		t.Fatal(e)
	}
	install := configuredInstallation(artifact)
	cfg := &BuiltinGovernanceConfig{Installations: []governance.Installation{install}, ReadinessTTLSeconds: 30}
	lifecycle, _, closeAll, e := configureBuiltinGovernance(a, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer closeAll()
	if _, _, _, e = configureBuiltinGovernance(a, cfg); !api.IsCode(e, "capacity_exhausted") {
		t.Fatalf("second worker shared the same physical ownership: %v", e)
	}
	prepared, e := lifecycle.Prepare(ctx, install)
	if e != nil || !prepared.IsolationVerified || !prepared.Compatible {
		t.Fatalf("actual trusted preparation: %+v %v", prepared, e)
	}
	if _, e = a.Memory.Read(ctx, a.Scope, a.ServiceAuth, prepared.SelfTestRef, "read"); e != nil {
		t.Fatalf("selftest was not published into actual Content: %v", e)
	}
	req := governance.InstanceRequest{TargetID: api.NewID("target"), ActivationID: api.NewID("activation"), InstanceID: api.NewID("instance"), Generation: 1, Installation: install, ConfigRef: install.ConfigRef, Deadline: api.Time(time.Now().Add(time.Minute))}
	ready, e := lifecycle.Initialize(ctx, req)
	if e != nil || !ready.Ready {
		t.Fatalf("actual instance: %+v %v", ready, e)
	}
	if e = closeAll(); e != nil {
		t.Fatal(e)
	}
	again, _, closeAgain, e := configureBuiltinGovernance(a, cfg)
	if e != nil {
		t.Fatalf("actual ownership not released after exit: %v", e)
	}
	fence, e := again.Fence(ctx, req)
	if e != nil || !fence.Exited || fence.MayApplyLater {
		t.Fatalf("original worker was not actually joined: %+v %v", fence, e)
	}
	if e = closeAgain(); e != nil {
		t.Fatal(e)
	}
	bound := builtinGovernanceContent{a}
	p := adapter.Publication{ID: api.NewID("proof"), MediaType: "application/json", ProcessedSources: []api.ContentRef{artifact}, DisclosedSources: []api.ContentRef{}}
	if _, e = bound.Publish(ctx, p, []byte(`{"kind":"trace"}`)); e != nil {
		t.Fatal(e)
	}
	if e = a.Identity.Revoke(ctx, a.ServiceAuth); e != nil {
		t.Fatal(e)
	}
	if _, e = bound.Publish(ctx, p, []byte(`{"kind":"trace"}`)); !api.IsCode(e, "forbidden") {
		t.Fatalf("cached proof publication bypassed current revoked credential: %v", e)
	}
	if _, e = bound.Read(ctx, artifact, "extension.prepare.artifact"); !api.IsCode(e, "forbidden") {
		t.Fatalf("current bound read accepted revoked worker: %v", e)
	}
}
