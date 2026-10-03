package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type BuiltinGovernanceConfig struct {
	Installations       []governance.Installation         `json:"installations"`
	Implementations     []adapter.ReferenceImplementation `json:"implementations"`
	ReadinessTTLSeconds uint64                            `json:"readiness_ttl_seconds"`
}

// RequiredContentPurposes 返回本参考宿主的准确用途；配置它不放宽现有政策。
func RequiredContentPurposes() []string {
	return []string{"extension.prepare.artifact", "evaluation.runner.input", "evaluation.runner.truth", "evaluation.runner.prepared_current", "evaluation.runner.start_current", "evaluation.manifest", "content.write"}
}

func configureBuiltinGovernance(a *App, cfg *BuiltinGovernanceConfig) (governance.LifecyclePort, governance.EvaluationRunner, func() error, error) {
	noop := func() error { return nil }
	if cfg == nil {
		return nil, nil, noop, nil
	}
	if a == nil || !api.ValidID(a.Scope.TenantID) || !api.ValidID(a.Scope.OwnerID) || len(cfg.Installations)+len(cfg.Implementations) == 0 {
		return nil, nil, nil, api.E("invalid_request", "configured_governance_scope_and_allowlist_required")
	}
	var installs *adapter.BuiltinRegistry
	var references *adapter.ReferenceRegistry
	var e error
	if len(cfg.Installations) != 0 {
		if cfg.ReadinessTTLSeconds < 1 || cfg.ReadinessTTLSeconds > 600 {
			return nil, nil, nil, api.E("invalid_request", "explicit_finite_builtin_readiness_required")
		}
		installs, e = adapter.NewBuiltinRegistry(cfg.Installations)
		if e != nil {
			return nil, nil, nil, e
		}
	}
	if len(cfg.Implementations) != 0 {
		references, e = adapter.NewReferenceRegistry(cfg.Implementations)
		if e != nil {
			return nil, nil, nil, e
		}
		for _, purpose := range []string{"evaluation_prepare", "evaluation_start"} {
			registered := false
			if a.Keys != nil {
				for _, key := range a.Keys.Keys {
					if key.TenantID == a.Scope.TenantID && key.Issuer == a.Scope.OwnerID && key.Public != nil {
						for _, p := range key.Purposes {
							registered = registered || p == purpose
						}
					}
				}
			}
			if !registered {
				return nil, nil, nil, api.E("unsupported", "registered_evaluation_entry_keys_required")
			}
		}
	}
	if !a.OwnsTargets {
		var lifecycle governance.LifecyclePort
		var runner governance.EvaluationRunner
		if installs != nil {
			lifecycle = contractOnlyLifecycle{installs}
		}
		if references != nil {
			runner = contractOnlyEvaluation{references}
		}
		return lifecycle, runner, noop, nil
	}
	if !filepath.IsAbs(a.Config.DataRoot) || a.Store == nil || a.Memory == nil || a.ServiceAuth.TenantID != a.Scope.TenantID || a.ServiceAuth.SubjectID != a.Scope.OwnerID {
		return nil, nil, nil, api.E("invalid_request", "bound_governance_worker_services_required")
	}
	root := filepath.Join(a.Config.DataRoot, "governance")
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, nil, nil, e
	}
	info, e := os.Lstat(root)
	if e != nil {
		return nil, nil, nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return nil, nil, nil, api.E("forbidden", "governance_worker_root_must_be_private")
	}
	lock, e := os.OpenFile(filepath.Join(root, "worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, nil, nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return nil, nil, nil, errors.Join(api.E("capacity_exhausted", "governance_worker_root_in_use"), lock.Close())
	}
	var host *adapter.BuiltinHost
	var runner *adapter.ReferenceRunner
	var mu sync.Mutex
	closed := false
	closeAll := func() error {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return nil
		}
		var e error
		if runner != nil {
			e = runner.Close()
			if e == nil {
				runner = nil
			}
		}
		if host != nil {
			err := host.Close()
			if err == nil {
				host = nil
			}
			e = errors.Join(e, err)
		}
		if e != nil {
			return e // 未观察实际退出，不释放宿主所有权。
		}
		closed = true
		return lock.Close()
	}
	content := builtinGovernanceContent{a}
	if installs != nil {
		host, e = adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: filepath.Join(root, "builtin"), Scope: a.Scope, Content: content, Clock: time.Now, Installations: cfg.Installations, ReadinessTTL: time.Duration(cfg.ReadinessTTLSeconds) * time.Second})
		if e != nil {
			return nil, nil, nil, errors.Join(e, closeAll())
		}
	}
	if references != nil {
		runner, e = adapter.NewReferenceRunner(adapter.ReferenceRunnerConfig{Root: filepath.Join(root, "reference"), Scope: a.Scope, Content: content, Clock: time.Now, Keys: a.Keys, Implementations: cfg.Implementations})
		if e != nil {
			return nil, nil, nil, errors.Join(e, closeAll())
		}
	}
	var lifecycle governance.LifecyclePort
	var evaluation governance.EvaluationRunner
	if host != nil {
		lifecycle = host
	}
	if runner != nil {
		evaluation = runner
	}
	return lifecycle, evaluation, closeAll, nil
}

type builtinGovernanceContent struct{ a *App }

func (c builtinGovernanceContent) Read(ctx context.Context, ref api.ContentRef, purpose string) ([]byte, error) {
	return c.a.Memory.Read(ctx, c.a.Scope, c.a.ServiceAuth, ref, purpose)
}
func (c builtinGovernanceContent) Publish(ctx context.Context, p adapter.Publication, b []byte) (api.ContentRef, error) {
	status, e := c.a.Store.Within(ctx, c.a.Scope, []string{"platform", "content", "memory", "governance"}, func(tx runtime.Tx) error {
		if e := currentCredentialTx(ctx, tx, c.a.ServiceAuth); e != nil {
			return e
		}
		for _, ref := range uniqueSources(append(append([]api.ContentRef{}, p.ProcessedSources...), p.DisclosedSources...)) {
			if _, e := c.a.Memory.CheckContentTx(ctx, tx, c.a.ServiceAuth, ref, "content.write", "cloud", true); e != nil {
				return e
			}
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return api.ContentRef{}, runtime.ErrCommitUnknown
	}
	if e != nil {
		return api.ContentRef{}, e
	}
	return c.a.Publish(ctx, c.a.Scope, c.a.ServiceAuth, p.ID, p.MediaType, b, p.ProcessedSources, p.DisclosedSources)
}

type contractOnlyLifecycle struct{ *adapter.BuiltinRegistry }

func (contractOnlyLifecycle) Prepare(context.Context, governance.Installation) (governance.PreparationEvidence, error) {
	return governance.PreparationEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyLifecycle) Initialize(context.Context, governance.InstanceRequest) (governance.InstanceEvidence, error) {
	return governance.InstanceEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyLifecycle) Fence(context.Context, governance.InstanceRequest) (governance.FenceEvidence, error) {
	return governance.FenceEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyLifecycle) Dispose(context.Context, governance.Installation) (governance.DisposalEvidence, error) {
	return governance.DisposalEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}

type contractOnlyEvaluation struct{ *adapter.ReferenceRegistry }

func (contractOnlyEvaluation) PreparePair(context.Context, governance.RunnerPair) (governance.PairEvidence, error) {
	return governance.PairEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyEvaluation) Run(context.Context, governance.RunnerAttempt) (governance.AttemptObservation, error) {
	return governance.AttemptObservation{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyEvaluation) Lookup(context.Context, governance.RunnerAttempt) (governance.AttemptObservation, bool, error) {
	return governance.AttemptObservation{}, false, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyEvaluation) Seal(context.Context, governance.RunnerPair) (governance.PairStopEvidence, error) {
	return governance.PairStopEvidence{}, api.E("unsupported", "worker_target_ownership_required")
}
