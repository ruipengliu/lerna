package wasi

import (
	"context"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

const AddressSpaceBytes = 1 << 30

// JournalPhase 只供宿主故障验证准确原日志的持久边界，不替换执行结果。
type JournalPhase string

const AfterJournalRename JournalPhase = "after_rename_before_directory_sync"

type Config struct {
	Root          string
	WorkerPath    string
	WorkerHash    string
	Store         rt.Store
	Content       execution.ContentPort
	Scope         rt.Scope
	Location      string
	MaxConcurrent int
	JournalFault  func(JournalPhase, string) error
}

type Manifest struct {
	RuntimeVersion  string      `json:"runtime_version"`
	WorkerHash      string      `json:"worker_hash"`
	BubblewrapHash  string      `json:"bubblewrap_hash"`
	PrlimitHash     string      `json:"prlimit_hash"`
	Platform        string      `json:"platform"`
	Kernel          string      `json:"kernel"`
	AddressSpace    uint64      `json:"address_space"`
	Probe           WorkerProbe `json:"probe"`
	MaxConcurrent   uint64      `json:"max_concurrent"`
	NamespaceFormat string      `json:"namespace_format"`
}

type Limits struct {
	NamespaceBytes uint64 `json:"namespace_bytes"`
	MemoryPages    uint32 `json:"memory_pages"`
	CPUSeconds     uint64 `json:"cpu_seconds"`
	WallMillis     uint64 `json:"wall_millis"`
	OutputBytes    uint64 `json:"output_bytes"`
}

func DefaultLimits() []api.Amount {
	return []api.Amount{{Unit: "namespace_bytes", Value: "65536"}, {Unit: "memory_pages", Value: "256"}, {Unit: "cpu_seconds", Value: "2"}, {Unit: "wall_millis", Value: "2000"}, {Unit: "output_bytes", Value: "65536"}}
}

func parseLimits(amounts []api.Amount) (Limits, error) {
	if len(amounts) != 5 || api.ValidateAmounts(amounts) != nil {
		return Limits{}, api.E("invalid_request", "invalid_wasi_limits")
	}
	values := map[string]uint64{}
	for _, a := range amounts {
		n, err := strconv.ParseUint(a.Value, 10, 64)
		if err != nil || n == 0 || values[a.Unit] != 0 {
			return Limits{}, api.E("invalid_request", "invalid_wasi_limits")
		}
		values[a.Unit] = n
	}
	n := values["namespace_bytes"]
	p := values["memory_pages"]
	c := values["cpu_seconds"]
	w := values["wall_millis"]
	o := values["output_bytes"]
	if n == 0 || n > 65536 || p == 0 || p > 256 || c == 0 || c > 5 || w == 0 || w > 10000 || o == 0 || o > n {
		return Limits{}, api.E("invalid_request", "invalid_wasi_limits")
	}
	return Limits{n, uint32(p), c, w, o}, nil
}

type Runtime struct {
	cfg          Config
	manifest     Manifest
	bwrap        string
	prlimit      string
	root         *os.Root
	lock         *os.File
	mu           sync.Mutex
	closing      bool
	closed       bool
	active       map[string]*process
	journalMu    sync.Mutex
	journalCount int
}

func New(cfg Config) (*Runtime, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, api.E("unsupported", "wasi_platform_not_probed")
	}
	if cfg.Store == nil || cfg.Content == nil || !api.ValidID(cfg.Scope.TenantID) || !api.ValidID(cfg.Scope.OwnerID) || cfg.Scope.DatabaseID != cfg.Store.ID() || cfg.Location == "" || !filepath.IsAbs(cfg.Root) || !filepath.IsAbs(cfg.WorkerPath) || cfg.WorkerHash == "" || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 4 {
		return nil, api.E("invalid_request", "invalid_wasi_configuration")
	}
	worker, err := elf.Open(cfg.WorkerPath)
	if err != nil {
		return nil, err
	}
	for _, p := range worker.Progs {
		if p.Type == elf.PT_INTERP {
			worker.Close()
			return nil, api.E("unsupported", "wasi_worker_must_be_static")
		}
	}
	if err = worker.Close(); err != nil {
		return nil, err
	}
	r := &Runtime{cfg: cfg, active: map[string]*process{}}
	if r.bwrap, err = exec.LookPath("bwrap"); err != nil {
		return nil, api.E("unsupported", "bubblewrap_not_installed")
	}
	if r.prlimit, err = exec.LookPath("prlimit"); err != nil {
		return nil, api.E("unsupported", "prlimit_not_installed")
	}
	if err = r.verifyExecutables(); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(cfg.Root, 0700); err != nil {
		return nil, err
	}
	if err = verifyPrivateRoot(cfg.Root); err != nil {
		return nil, err
	}
	if r.root, err = os.OpenRoot(cfg.Root); err != nil {
		return nil, err
	}
	if r.lock, err = acquireRuntimeLock(r.root); err != nil {
		r.root.Close()
		return nil, err
	}
	if err = r.probe(context.Background()); err != nil {
		r.lock.Close()
		r.root.Close()
		return nil, err
	}
	if err = r.openJournal(); err != nil {
		r.lock.Close()
		r.root.Close()
		return nil, err
	}
	return r, nil
}

func (r *Runtime) probe(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	key := api.NewID("probe")
	p := &process{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	if r.closing || len(r.active) >= r.cfg.MaxConcurrent {
		r.mu.Unlock()
		return api.E("overloaded", "wasi_probe_capacity_unavailable")
	}
	r.active[key] = p
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.active, key); close(p.done); r.mu.Unlock() }()
	cmd := r.command(ctx, 2, "--probe")
	stdout := limitedBuffer{limit: 4096}
	stderr := limitedBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return api.E("unsupported", "wasi_isolation_probe_failed")
	}
	var probe WorkerProbe
	if err := api.Decode(stdout.Bytes(), &probe); err != nil || !qualifiedProbe(probe) {
		return api.E("unsupported", "wasi_runtime_probe_changed")
	}
	manifest := Manifest{RuntimeVersion: RuntimeVersion, WorkerHash: r.cfg.WorkerHash, BubblewrapHash: r.manifest.BubblewrapHash, PrlimitHash: r.manifest.PrlimitHash, Platform: runtime.GOOS + "/" + runtime.GOARCH, Kernel: kernelRelease(), AddressSpace: AddressSpaceBytes, Probe: probe, MaxConcurrent: uint64(r.cfg.MaxConcurrent), NamespaceFormat: execution.PassiveEnvironmentFormat}
	if r.manifest.Platform != "" {
		if !api.Equal(manifest, r.manifest) {
			return api.E("unsupported", "wasi_runtime_probe_changed")
		}
	} else {
		r.manifest = manifest
	}
	return nil
}

func qualifiedProbe(p WorkerProbe) bool {
	return p.Protocol == WorkerProtocol && p.RuntimeVersion == RuntimeVersion && p.ModuleVersion == "v1.10.1" && p.ModuleSum == "h1:2DugeJf6VVk58KTPszlNfeeN8AhhpwcZqkJj2wwFuH8=" && p.GoVersion != "" && p.Answer == 42 && api.Equal(p.RootEntries, []string{"worker"}) && api.Equal(p.EnvironmentKeys, []string{"GOMAXPROCS", "GOMEMLIMIT", "PWD"}) && api.Equal(p.NetworkInterfaces, []string{"lo"}) && p.AddressSpaceBytes == AddressSpaceBytes && p.CPULimitSeconds == 2 && p.OpenFileLimit == 64
}

func (r *Runtime) command(ctx context.Context, cpu uint64, args ...string) *exec.Cmd {
	argv := []string{"--as=1073741824:1073741824", "--cpu=" + strconv.FormatUint(cpu, 10) + ":" + strconv.FormatUint(cpu, 10), "--core=0:0", "--nofile=64:64", "--", r.bwrap, "--unshare-all", "--die-with-parent", "--clearenv", "--cap-drop", "ALL", "--chdir", "/", "--setenv", "GOMAXPROCS", "1", "--setenv", "GOMEMLIMIT", "67108864", "--ro-bind", r.cfg.WorkerPath, "/worker", "--", "/worker"}
	cmd := exec.CommandContext(ctx, r.prlimit, append(argv, args...)...)
	configureProcess(cmd)
	return cmd
}

func (r *Runtime) EnvironmentConfigRef() api.ComponentRef {
	digest, _ := api.Digest([]any{"environment.wasi", "1", r.manifest, r.cfg.Scope, r.cfg.Root, "stdio-only;no-fs;no-network;no-credentials;no-subprocess"})
	return api.ComponentRef{ComponentID: execution.BuiltinComponentID("environment.wasi"), Version: "1", Digest: digest}
}
func (r *Runtime) InstallLockRef() api.ComponentRef {
	digest, _ := api.Digest(r.manifest)
	return api.ComponentRef{ComponentID: execution.BuiltinComponentID("environment.wasi.install"), Version: "1", Digest: digest}
}
func cloneManifest(m Manifest) Manifest {
	m.Probe.RootEntries = append([]string{}, m.Probe.RootEntries...)
	m.Probe.EnvironmentKeys = append([]string{}, m.Probe.EnvironmentKeys...)
	m.Probe.NetworkInterfaces = append([]string{}, m.Probe.NetworkInterfaces...)
	return m
}
func (r *Runtime) Manifest() Manifest { return cloneManifest(r.manifest) }
func (r *Runtime) Check(config, install api.ComponentRef, amounts []api.Amount) (execution.EnvironmentIsolation, error) {
	r.mu.Lock()
	closing := r.closing
	r.mu.Unlock()
	if closing {
		return execution.EnvironmentIsolation{}, api.E("unsupported", "wasi_worker_closed")
	}
	if !api.Equal(config, r.EnvironmentConfigRef()) || !api.Equal(install, r.InstallLockRef()) {
		return execution.EnvironmentIsolation{}, api.E("unsupported", "wasi_runtime_lock_changed")
	}
	if _, err := parseLimits(amounts); err != nil {
		return execution.EnvironmentIsolation{}, err
	}
	return execution.EnvironmentIsolation{RuntimeKind: "restricted_wasi_preview1", IsolationDigest: r.EnvironmentConfigRef().Digest}, nil
}
func (r *Runtime) PrepareEnvironment(ctx context.Context, sc rt.Scope, auth rt.Auth, env execution.Environment) error {
	if sc != r.cfg.Scope || env.Principal.TenantID != sc.TenantID || auth.TenantID != sc.TenantID {
		return api.E("forbidden", "wasi_scope_changed")
	}
	if _, err := r.Check(env.ConfigRef, env.InstallLockRef, env.Limits); err != nil {
		return err
	}
	if err := r.verifyExecutables(); err != nil {
		return err
	}
	return r.probe(ctx)
}

// admission 适配只提供环境准备入口；Driver.Prepare 仍只冻结输入而不运行代码。
type admission struct{ r *Runtime }

func (a admission) Check(c, i api.ComponentRef, amounts []api.Amount) (execution.EnvironmentIsolation, error) {
	return a.r.Check(c, i, amounts)
}
func (a admission) Prepare(ctx context.Context, sc rt.Scope, auth rt.Auth, env execution.Environment) error {
	return a.r.PrepareEnvironment(ctx, sc, auth, env)
}
func (r *Runtime) Admission() execution.EnvironmentAdmission { return admission{r} }
func (r *Runtime) Capability() execution.Capability          { return execution.WASIRunCellCapability() }
