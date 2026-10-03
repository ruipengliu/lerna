package development

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

func TestConfiguredWASIEnvironmentUsesActualPinnedRuntimeAndOriginalPreparation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			worker := filepath.Join(root, "runtime-worker")
			build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", worker, "../../cmd/wasi-worker")
			build.Env = append(os.Environ(), "CGO_ENABLED=0")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build the actual static WASI worker: %s %v", output, err)
			}
			artifact, err := os.ReadFile(worker)
			if err != nil {
				t.Fatal(err)
			}
			// 已有13端口只用来取得真实平台资格；业务断言经过宿主公开方法。
			discovery, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer discovery.Close()
			wasiConfig := WASIConfig{WorkerPath: worker, WorkerHash: api.Hash(artifact), MaxConcurrent: 1}
			qualified, err := configureWASI(discovery, &wasiConfig)
			if err != nil {
				t.Fatalf("actual platform qualification prerequisite: %v", err)
			}
			configRef, installLock := qualified.ConfigRef, qualified.InstallLockRef
			if err = qualified.Close(); err != nil {
				t.Fatal(err)
			}
			if err = discovery.Close(); err != nil {
				t.Fatal(err)
			}
			var configured map[string]any
			if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
				t.Fatal(err)
			}
			configured["wasi"] = wasiConfig
			if err = api.Decode(api.Raw(configured), &cfg); err != nil {
				t.Fatalf("qualified runtime has no explicit host configuration: %v", err)
			}
			a, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			id := api.NewID("environment")
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "environment.create", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(execution.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: configRef, InstallLockRef: installLock, Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}})}
			receipt, err := a.Dispatcher.Command(ctx, a.ServiceAuth, api.Raw(command))
			if err != nil || receipt.Error != nil || receipt.Stage != "accepted" {
				t.Fatalf("public environment admission did not accept the actual pinned runtime: %v %+v", err, receipt)
			}
			if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 100); err != nil {
				t.Fatal(err)
			}
			raw, err := a.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var original execution.Environment
			if err != nil || api.Decode(raw, &original) != nil || original.Phase != "active" || original.RuntimeKind != "restricted_wasi_preview1" || !original.ReadyForCell || !original.ActuallyExited || original.NamespaceRef == nil || original.NamespaceRevision != 1 || !api.Equal(original.InstallLockRef, installLock) || original.PreparationCommandID != command.CommandID {
				t.Fatalf("actual original preparation/readiness was not preserved: %v %+v", err, original)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			raw, err = reopened.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var after execution.Environment
			if err != nil || api.Decode(raw, &after) != nil || !api.Equal(original, after) {
				t.Fatalf("reopen replaced the original environment/generation/namespace: %v %+v", err, after)
			}
			final, err := reopened.Dispatcher.Command(ctx, reopened.ServiceAuth, api.Raw(command))
			if err != nil || final.Stage != "applied" || final.CommandID != command.CommandID {
				t.Fatalf("reopen replaced the original preparation receipt: %v %+v", err, final)
			}
		})
	}
}
