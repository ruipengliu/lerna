package wasi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// Profile 只把原 worker 实测登记的合同交给非执行进程，不宣称当前环境已 ready。
type Profile struct {
	config   api.ComponentRef
	install  api.ComponentRef
	manifest Manifest
}

func LoadProfile(cfg Config) (*Profile, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, api.E("unsupported", "wasi_platform_not_probed")
	}
	if !filepath.IsAbs(cfg.Root) || !filepath.IsAbs(cfg.WorkerPath) || !api.ValidID(cfg.Scope.TenantID) || !api.ValidID(cfg.Scope.OwnerID) || cfg.Scope.DatabaseID == "" || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 4 {
		return nil, api.E("invalid_request", "invalid_wasi_configuration")
	}
	if err := verifyPrivateRoot(cfg.Root); err != nil {
		return nil, err
	}
	hash, err := trustedFileHash(cfg.WorkerPath)
	if err != nil {
		return nil, err
	}
	if hash != cfg.WorkerHash {
		return nil, api.E("unsupported", "wasi_worker_artifact_changed")
	}
	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	r := Runtime{cfg: cfg, root: root}
	err = r.readJSON("manifest.json", &r.manifest)
	closeErr := root.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	m := r.manifest
	if m.RuntimeVersion != RuntimeVersion || m.WorkerHash != cfg.WorkerHash || m.Platform != "linux/amd64" || m.Kernel == "" || m.AddressSpace != AddressSpaceBytes || m.MaxConcurrent != uint64(cfg.MaxConcurrent) || m.NamespaceFormat != execution.PassiveEnvironmentFormat || m.BubblewrapHash == "" || m.PrlimitHash == "" || !qualifiedProbe(m.Probe) {
		return nil, api.E("unsupported", "wasi_profile_not_qualified")
	}
	return &Profile{config: r.EnvironmentConfigRef(), install: r.InstallLockRef(), manifest: cloneManifest(m)}, nil
}

func (p *Profile) EnvironmentConfigRef() api.ComponentRef { return p.config }
func (p *Profile) InstallLockRef() api.ComponentRef       { return p.install }
func (p *Profile) Manifest() Manifest                     { return cloneManifest(p.manifest) }
func (p *Profile) Check(c, i api.ComponentRef, amounts []api.Amount) (execution.EnvironmentIsolation, error) {
	if !api.Equal(c, p.config) || !api.Equal(i, p.install) {
		return execution.EnvironmentIsolation{}, api.E("unsupported", "wasi_runtime_lock_changed")
	}
	if _, e := parseLimits(amounts); e != nil {
		return execution.EnvironmentIsolation{}, e
	}
	return execution.EnvironmentIsolation{RuntimeKind: "restricted_wasi_preview1", IsolationDigest: p.config.Digest}, nil
}
func (p *Profile) Prepare(context.Context, rt.Scope, rt.Auth, execution.Environment) error {
	return api.E("unsupported", "environment_preparation_requires_worker")
}
