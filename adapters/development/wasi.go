package development

import (
	"path/filepath"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
)

type WASIConfig struct {
	WorkerPath    string `json:"worker_path"`
	WorkerHash    string `json:"worker_hash"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type WASIAssembly struct {
	Driver         execution.Driver
	Admission      execution.EnvironmentAdmission
	ConfigRef      api.ComponentRef
	InstallLockRef api.ComponentRef
	Close          func() error
}

func RequiredWASIContentPurposes() []string {
	return []string{"environment_code", "environment_input", "environment_namespace", "environment_checkpoint", "execution_wasi_receipt"}
}

// configureWASI 只装配选定运行时。业务 Grant、当前来源与命名空间提交仍由原领域负责。
func configureWASI(a *App, cfg *WASIConfig) (WASIAssembly, error) {
	assembly := WASIAssembly{Close: func() error { return nil }}
	if cfg == nil {
		return assembly, nil
	}
	if a == nil || a.Store == nil || a.Memory == nil || !filepath.IsAbs(a.Config.DataRoot) || a.ServiceAuth.TenantID != a.Scope.TenantID || a.ServiceAuth.SubjectID != a.Scope.OwnerID {
		return WASIAssembly{}, api.E("invalid_request", "bound_wasi_worker_services_required")
	}
	c := wasi.Config{Root: filepath.Join(a.Config.DataRoot, "wasi"), WorkerPath: cfg.WorkerPath, WorkerHash: cfg.WorkerHash, MaxConcurrent: cfg.MaxConcurrent, Store: a.Store, Scope: a.Scope, Content: executionContent{a}, Location: "cloud"}
	if !a.OwnsTargets {
		profile, err := wasi.LoadProfile(c)
		if err != nil {
			return WASIAssembly{}, err
		}
		assembly.Driver = contractOnlyDriver{capability: execution.WASIRunCellCapability()}
		assembly.Admission = profile
		assembly.ConfigRef, assembly.InstallLockRef = profile.EnvironmentConfigRef(), profile.InstallLockRef()
		return assembly, nil
	}
	host, err := wasi.New(c)
	if err != nil {
		return WASIAssembly{}, err
	}
	assembly.Driver, assembly.Admission, assembly.Close = host, host.Admission(), host.Close
	assembly.ConfigRef, assembly.InstallLockRef = host.EnvironmentConfigRef(), host.InstallLockRef()
	return assembly, nil
}
